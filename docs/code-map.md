# Code Map

How the library fits together, and why it is shaped this way.

This is a map of the code as it stands, not a design proposal.
It is meant to be read on its own.

`(dec. 12)` points at entry 12 of `docs/decisions.md`, where that choice is argued in full against the alternatives it beat.
Follow one only when the short reason here is not enough — the decision log is the record, this is the tour.

## The two halves

FlowCore has a configuration side and a runtime side, and the split runs all the way down — two service types, two sets of tables, two sets of values.

```
  DEFINITIONS (templates)                RUNS (instances)
  edited through Catalog                 executed through Engine

  workflow_definition                    workflow
  workflow_status_definition             step          } a snapshot of the
  step_definition                        action        } definition's graph
  action_definition                      step_visit    <- record of work done
```

A definition is a template.
Starting a run copies its whole graph into the instance tables, and the run never reads the definition again — so editing or deleting a definition cannot reach a run already in flight (dec. 23).

`step_visit` is the one instance table that is not a copy of anything.
One row is written each time a run _enters_ a step, so a step reached twice by a loop has two rows, and a completed one is never rewritten (dec. 23).

## Layers

```
  CLIENT CODE
  Builds WorkflowDefinition and *Params values by hand. Imports no pgx,
  holds no connection, can assemble a whole definition entirely offline.
        |
        |  values in, values out
        v
+------------------------------------------------------------------------+
| SERVICE LAYER              catalog.go, engine.go                       |
|                                                                        |
| Both hold the *pgxpool.Pool, sequence store calls, and                 |
| own the transactions. Each sees a whole tree or a whole                |
| run at once, so rules that span rows live here, and only               |
| here.                                                                  |
+----------------------------------------+-------------------------------+
| Catalog               catalog.go       | Engine        engine.go       |
|                                        |                               |
| Edits the templates: define a          | Runs instances: start         |
| workflow, then edit its parts          | one, advance it, read         |
| one row at a time.                     | where it stands.              |
|                                        |                               |
|   Create                               |   Start                       |
|   Get                                  |   CompleteStep                |
|   UpdateWorkflowDefinition             |   GetState                    |
|   DeleteWorkflowDefinition             |   GetHistory                  |
|   Add / Update / Delete Status         |                               |
|   Add / Update / Delete Step           |                               |
|   Add / Update / Delete Action         |                               |
|                                        |                               |
| unexported, tree-shaped, no SQL:       | unexported, no SQL:           |
|   readDefinition  fillIDs              |   buildSnapshot               |
|   ensureID  stepExists  clone          |   writeSnapshot               |
|                                        |   advance   readState         |
+------------------------------------------------------------------------+
        |                                        ^
        |  (ctx, querier, value)                 |  domain value, or a
        v                                        |  typed domain error
+------------------------------------------------------------------------+
| store                                store.go + store_<table>.go       |
|                                                                        |
| Hand-written SQL, one file per table. Free functions: no receiver, no  |
| state, and no connection of its own -- every call is handed a querier  |
| by the layer above.                                                    |
|                                                                        |
|   DEFINITION SIDE                    INSTANCE SIDE                     |
|   store_workflow_definition.go       store_workflow.go                 |
|     insert get update setInitial       insert getState update complete |
|     delete                             getIDBySubject                  |
|   store_status_definition.go         store_step.go                     |
|     insert list update delete          insert get                      |
|   store_step_definition.go           store_action.go                   |
|     insert get list update delete      insert list getForStep          |
|   store_action_definition.go         store_step_visit.go               |
|     insert list(x2) update delete      insert complete get list        |
|                                                                        |
|   querier   = Exec | Query | QueryRow.   Begin deliberately absent.    |
|   txQuerier = querier + Conn.            See "one statement or many".  |
+------------------------------------------------------------------------+
        |                                        ^
        |  raw *pgconn.PgError                   |  typed error + sentinel
        v                                        |
+------------------------------------------------------------------------+
| error mapping                              errmap.go  ->  errors.go    |
|                                                                        |
|   mapInsertErr           parent (cascade) FK  ->  NotFoundError{parent}|
|   mapWriteErr            reference FK         ->  CrossDefinitionError |
|   mapDeleteErr           reference FK         ->  ReferencedError      |
|   mapWorkflowInsertErr   active-run index     ->  ActiveWorkflowExists |
|                                                                        |
|   shared by all four:                                                  |
|     unique index  ->  DuplicateNameError                               |
|     CHECK (name)  ->  InvalidNameError | InvalidActionError            |
|     CHECK (id)    ->  InvalidIdentifierError{Field}                    |
|     unrecognized  ->  UnmappedConstraintError   (fails loud, no guess) |
+------------------------------------------------------------------------+
        |
        v
  Postgres, schema "flowcore"   <- migrate.go applies it, embedded
```

## The values that flow through every layer

These are carried by value from the client all the way to the SQL parameters.
They are not a layer; nothing in them reaches downward.

```
+----------------------------------+  +----------------------------------+
| definition_types.go              |  | instance_types.go                |
| what the CATALOG returns         |  | what the ENGINE returns          |
|                                  |  |                                  |
|  WorkflowDefinition  (root)      |  |  WorkflowState                   |
|   +- Statuses []                 |  |   +- CurrentStep *               |
|   +- Steps    []                 |  |       +- Actions []              |
|       +- Actions []              |  |  StepVisit                       |
|                                  |  |   +- Completion *                |
|  ONE TYPE PER TABLE: a           |  |                                  |
|  definition's read shape IS      |  |  NOT one type per table:         |
|  its row shape.                  |  |  PROJECTIONS assembled from      |
|                                  |  |  several tables at once.         |
+----------------------------------+  +----------------------------------+

+------------------------------------------------------------------------+
| params.go -- what the caller SUPPLIES                                  |
|                                                                        |
|   UpdateWorkflowDefinitionParams    AddActionParams                    |
|   AddStatusParams                   UpdateActionParams                 |
|   UpdateStatusParams                StartParams                        |
|   AddStepParams                     CompleteParams                     |
|   UpdateStepParams                                                     |
|                                                                        |
|   Nullable[T] + SetTo / Clear   immutable columns are OMITTED,         |
|   validate() rejects a field    so they cannot be expressed            |
|   left undecided                                                       |
+------------------------------------------------------------------------+
```

All of it is inert: no pool, no ctx, no pgx, no `Save`, no `Load`, no `Delete` (dec. 11).
The only methods are `ToUpdate()`, which turns a read value into the params for updating it, and `clone()`, a deep copy.

## Why the instance side has two kinds of type

The definition side needs only one type per table because a definition's read shape _is_ its row shape — `Catalog.Get` returns a nested tree of rows.

A run is different.
"Where does this stand" spans the workflow, its open visit, that visit's step, and the step's actions; a history entry needs its step's name, which is on another table (dec. 33).
So the instance side has two families:

```
  workflowRow  stepRow  actionRow  stepVisitRow     <- unexported, write path
       mirror table rows exactly                       nobody receives one

  WorkflowState  CurrentStep  Action                <- exported, read path
  StepVisit      Completion                            assembled from joins
```

The `Row` suffix is the reminder of which is which.
Row structs exist so an insert with eight columns — several of them adjacent uuids — is a keyed literal rather than positional arguments, where a transposition compiles and silently writes the wrong value (dec. 33).

`Completion` is a nested pointer rather than four loose fields because the schema makes completion all-or-nothing.
One pointer holds the same rule in Go: a half-completed visit is unrepresentable in both places, and `Completion.By` cannot be read without acknowledging the visit might still be open (dec. 30, 33).

## One statement or many: querier vs txQuerier

The single most useful thing to know when reading a helper.

```
  q querier     "I work either way."      caller may pass a pool OR a tx
  q txQuerier   "I need a transaction."   a pool does not compile

  one statement  -> atomic by itself     -> querier    (34 helpers)
  many statements -> only a tx makes them atomic -> txQuerier (4 helpers)
                     readDefinition  readState  writeSnapshot  advance
```

`txQuerier` is `querier` plus `Conn`, which `pgx.Tx` has and `*pgxpool.Pool` does not.
`Begin` is absent from both: a helper can _require_ a transaction, never start one (dec. 10, 37).

Why it matters concretely — `advance` closes one visit and opens the next.
On a pool those are two independent commits, so a failure between them leaves a run with no open step, permanently stuck (dec. 37).

## Transactions, and why the isolation levels differ

Only five methods open a transaction. Everything else is a single statement on the pool.

```
  Catalog.Create        default          write a whole tree atomically
  Catalog.Get           RepeatableRead + ReadOnly    one consistent snapshot
  Engine.Start          RepeatableRead   reads a definition over 4 queries;
                                         must not freeze a torn one
  Engine.CompleteStep   default          MUST NOT be repeatable read
  Engine.GetState       RepeatableRead + ReadOnly    status and step agree
```

`CompleteStep` is the interesting one, and the choice is load-bearing rather than incidental.
Its conditional update must **block and re-check**: when two callers complete the same visit, the loser waits for the winner to commit, re-evaluates `completed_at is null`, matches nothing, and is told its view is stale — a typed `VisitNotOpenError`.
Under repeatable read that same race raises `40001`, which has no member in the taxonomy and no retry logic behind it (dec. 19, 36).

`Start` is repeatable read for the opposite reason, and can afford it because it only inserts rows whose ids it just generated, so it cannot hit a write conflict (dec. 36).
`Catalog.Get` takes the same read-only snapshot, for the same reason (dec. 12).

## Two calls traced end to end

`Catalog.Create` is the only path that exercises every ownership rule at once.

```
Catalog.Create(ctx, definition)
|
|  [1] PRE-FLIGHT -- no database touched yet          catalog.go
|        len(Steps) == 0                   -> ErrNoSteps      dec. 16
|        definition = definition.clone()      caller's value is never
|                                             mutated
|        fillIDs(&definition)   dec. 2       UUIDv7 into every zero id,
|                                             stamps parent links downward
|        entry step: default to Steps[0], or verify a supplied id is
|        actually in this tree             -> ErrInitialStepNotInTree  dec. 6
|
|        ^ every one of these needs the WHOLE TREE in scope.
|          This is the work no single entity could do for itself.
|
|  [2] tx, err := c.pool.Begin(ctx)                   catalog.go
|        defer tx.Rollback(ctx)
|
|  [3] COMPOSE store calls -- every one handed the SAME tx
|        insertWorkflowDefinition -> insertStatusDefinition (each)
|        -> insertStepDefinition (each) -> insertActionDefinition (each)
|        -> setInitialStepDefinition
|              |
|              v  each helper maps its OWN error, keyed on the intent
|        mapInsertErr / mapWriteErr        dec. 13, 20    errmap.go
|
|  [4] readDefinition(ctx, tx, id)                    catalog.go
|        four queries on the SAME tx, so Create reads its own
|        uncommitted writes back and returns a canonical tree
|
|  [5] tx.Commit(ctx)
|        the reference FKs are DEFERRABLE INITIALLY DEFERRED, so a bad
|        reference surfaces HERE rather than at the offending insert dec. 15
|
v  returns the stored WorkflowDefinition
```

`Engine.CompleteStep` is the intricate one on the runtime side.

```
Engine.CompleteStep(ctx, params)
|
|  [1] tx = pool.BeginTx(READ COMMITTED)     dec. 36     engine.go
|        NOT repeatable read -- see above
|
|  [2] completeStepVisit(...)                store_step_visit.go
|        UPDATE ... SET completed_at = now(), completed_by, selected_action
|        WHERE id = $1 AND completed_at IS NULL      <- THE GATE dec. 34
|                                            RETURNING the closed row
|
|        one statement, so there is no check-then-write window.
|        0 rows -> re-read to say which:
|             row absent  -> NotFoundError      (bad id)
|             row closed  -> VisitNotOpenError  (stale view)
|
|  [3] getActionForStep(actionID, closed.StepID)   store_action.go
|        scoped to the step the visit was on.
|        The FK enforces the same pair, but it is DEFERRED: it fires
|        at COMMIT, too late to be useful here.   dec. 35
|        So this read is the mechanism, the constraint a backstop.
|                                       -> ActionNotAvailableError
|
|  [4] advance(...)                                   engine.go
|        terminal action -> completeWorkflow: stamp the terminal
|                           status  dec. 26   set completed_at,
|                           releasing {subject, definition} for a new run
|        routing action  -> getStep(next)             the frozen snapshot,
|                           insertStepVisit  dec. 28    seeded with that
|                           updateWorkflowStatus      step's default assignee
|
|  [5] readState(ctx, tx, workflowID)                 engine.go
|        reads its own uncommitted writes back, so the returned value is
|        what is stored rather than what the code believes it wrote
|
|  [6] tx.Commit(ctx) -> mapWriteErr
|
v  returns the new WorkflowState
```

## Why the boundaries sit where they do

```
+------------------------------------------------------------------------+
| Why the store cannot open its own transaction               dec. 10    |
|                                                                        |
| Begin is left out of the querier interface on purpose. A helper is     |
| therefore incapable of starting a transaction -- not discouraged from  |
| it, incapable.                                                         |
|                                                                        |
| If helpers could, every insert in Create might quietly run in its own, |
| and a failure halfway would leave half a definition in the database.   |
| Keeping Begin above the store means there is exactly one place to look |
| to know what is atomic with what.                                      |
+------------------------------------------------------------------------+

+------------------------------------------------------------------------+
| Why error mapping happens at the store, not the service   dec. 13, 17  |
|                                                                        |
| Postgres reports a referencing INSERT and a referenced DELETE with     |
| byte-identical fields: same SQLSTATE, same constraint name, same       |
| table. Only the human-readable prose differs, and that is locale-      |
| dependent.                                                             |
|                                                                        |
| So the error alone cannot tell you what went wrong. You need to know   |
| which operation produced it -- and that is knowledge only the function |
| that ran the statement has. By the time an error reaches the service   |
| layer it is gone.                                                      |
|                                                                        |
| Hence four wrappers instead of one mapper with an "operation"          |
| argument: intent is implicit in which function you are standing in, so |
| no call site can pass the wrong one.                                   |
+------------------------------------------------------------------------+

+------------------------------------------------------------------------+
| Why instance rows record definition ids, not reference them  dec. 24   |
|                                                                        |
| Every instance row carries the id of the definition row it was copied  |
| from -- as a plain uuid, with no foreign key.                          |
|                                                                        |
| A foreign key would make any step or status ever touched by a run      |
| undeletable forever, and would change what DeleteWorkflowDefinition    |
| means. Storing only the frozen NAME would be worse in a subtler way:   |
| a rename silently splits one concept into two, and every cross-run     |
| query undercounts with no error.                                       |
|                                                                        |
| Recording the id keeps "all steps that were Director Approval"         |
| answerable across a rename and after the definition is gone -- the     |
| same record-but-do-not-interpret rule already used for assignees and   |
| subject references, turned inward onto the library's own ids.          |
+------------------------------------------------------------------------+

+------------------------------------------------------------------------+
| Why one file per table, but one service for all of them     dec. 11    |
|                                                                        |
| SQL changes per table: a migration that alters action_definition       |
| should touch exactly store_action_definition.go.                       |
|                                                                        |
| Orchestration changes per flow, and flows cross tables. Separate       |
| repositories would force the caller to assemble a cross-entity         |
| transaction themselves -- which is precisely the guarantee the library |
| exists to provide.                                                     |
+------------------------------------------------------------------------+
```
