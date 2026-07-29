# Code Map

How the definition side fits together, and why it is shaped this way.

This is a map of the code as it stands, not a design proposal.
The reasoning behind individual choices lives in `docs/decisions.md`; this document is meant to stand on its own without it.

## Layers

```
  CLIENT CODE
  Builds WorkflowDefinition and *Params values by hand. Imports no pgx,
  holds no connection, can assemble a whole definition entirely offline.
        |
        |  values in, values out
        v
+------------------------------------------------------------------------+
| Catalog                                                    catalog.go  |
|                                                                        |
| Holds the *pgxpool.Pool. Sequences store calls. Owns transactions.     |
| Sees the whole tree at once, so tree-wide rules live here, and only    |
| here.                                                                  |
|                                                                        |
|   Create      Get           DeleteWorkflowDefinition                   |
|   AddStatus   UpdateStatus  DeleteStatus                               |
|   AddStep     UpdateStep    DeleteStep                                 |
|   AddAction   UpdateAction  DeleteAction                               |
|                                                                        |
| unexported, tree-shaped, contain no SQL:                               |
|   readDefinition   fillIDs   ensureID   stepExists   clone             |
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
|   store_workflow_definition.go  insert  get  setInitialStep  delete    |
|   store_status_definition.go    insert  list  update  delete           |
|   store_step_definition.go      insert  get  list  update  delete      |
|   store_action_definition.go    insert  list(x2)  update  delete       |
|                                                                        |
|   querier = Exec | Query | QueryRow.   Begin is deliberately absent.   |
|   Satisfied by BOTH *pgxpool.Pool and pgx.Tx -- that is the point.     |
+------------------------------------------------------------------------+
        |                                        ^
        |  raw *pgconn.PgError                   |  typed error + sentinel
        v                                        |
+------------------------------------------------------------------------+
| error mapping                              errmap.go  ->  errors.go    |
|                                                                        |
|   mapInsertErr   parent (cascade) FK  ->  NotFoundError{parent}        |
|   mapWriteErr    reference FK         ->  CrossDefinitionError         |
|   mapDeleteErr   reference FK         ->  ReferencedError{target}      |
|                                                                        |
|   shared by all three:                                                 |
|     unique index  ->  DuplicateNameError                               |
|     CHECK         ->  InvalidNameError | InvalidActionError            |
|     unrecognized  ->  UnmappedConstraintError   (fails loud, no guess) |
+------------------------------------------------------------------------+
        |
        v
  Postgres, schema "flowcore"
```

## The values that flow through every layer

These are carried by value from the client all the way to the SQL parameters.
They are not a layer; nothing in them reaches downward.

```
+---------------------------------+  +---------------------------------+
| definition_types.go             |  | params.go                       |
| what the library RETURNS        |  | what the caller SUPPLIES        |
|                                 |  |                                 |
|  WorkflowDefinition   (root)    |  |  AddStatusParams                |
|   +- Statuses []                |  |  UpdateStatusParams             |
|   +- Steps    []                |  |  AddStepParams                  |
|       +- Actions []             |  |  UpdateStepParams               |
|                                 |  |  AddActionParams                |
|  methods, and ONLY these:       |  |  UpdateActionParams             |
|    ToUpdate()  value -> params  |  |                                 |
|    clone()     deep copy        |  |  Nullable[T] + SetTo / Clear    |
|                                 |  |  validate()  rejects a field    |
|  no pool. no ctx. no pgx.       |  |              left undecided     |
|  no Save. no Load. no Delete.   |  |                                 |
|                                 |  |  immutable columns are OMITTED, |
|  Inert data, start to finish.   |  |  so they cannot be expressed    |
+---------------------------------+  +---------------------------------+
```

## Why the structs stay inert

The single fact that decides it:

```
   Catalog.Create                      Catalog.AddStep
        |                                    |
   tx := pool.Begin(ctx)               (no transaction at all)
        |                                    |
        v                                    v
   insertStepDefinition(               insertStepDefinition(
       ctx, tx, step)                      ctx, pool, step)
         \                                    /
          \______  the same function  _______/
                   called two ways

   A step.Save() method would have to hold either the pool or the
   transaction. Holding one makes the other impossible: an entity
   built to save itself on the pool cannot enlist in Create's
   transaction, and one built around a transaction cannot be used
   for a standalone edit.

   So persistence is a free function that is HANDED a querier, and
   the struct carries no persistence behavior at all.
```

Three consequences follow from that, and together they are the whole argument:

```
  1. The caller can build a definition with no database in sight.
     Create takes a tree the client assembled in memory. That only
     works because StepDefinition is a plain value -- no connection
     to construct, nothing to inject, trivially serializable to a
     client's admin page.

  2. Cross-entity rules have somewhere to live.
     "The entry step must be one of these steps." "Every action gets
     its parent's id stamped on it." These are properties of the
     TREE, not of any row. On an entity that saves itself they would
     have to be bolted onto whichever entity happened to notice.
     Catalog is the only place the whole tree is in scope at once,
     so they live there -- see fillIDs and stepExists.

  3. Atomicity stays the library's job, not the caller's.
     A reader must never see a half-built definition. If each entity
     saved itself, either the transaction gets threaded through every
     entity method, or the caller assembles it. Both hand the client
     a guarantee the library sold them.
```

## One call traced end to end

`Catalog.Create` is the only path that exercises every ownership rule at once.

```
Catalog.Create(ctx, definition)
|
|  [1] PRE-FLIGHT -- no database touched yet          catalog.go
|        len(Steps) == 0                   -> ErrNoSteps
|        definition = definition.clone()      caller's value is never
|                                             mutated
|        fillIDs(&definition)                 UUIDv7 into every zero id,
|                                             stamps parent links downward
|        entry step: default to Steps[0], or verify a supplied id is
|        actually in this tree             -> ErrInitialStepNotInTree
|
|        ^ every one of these needs the WHOLE TREE in scope.
|          This is the work no single entity could do for itself.
|
|  [2] tx, err := c.pool.Begin(ctx)                   catalog.go
|        defer tx.Rollback(ctx)
|        ^ Catalog is the ONLY layer permitted to do this.
|
|  [3] COMPOSE store calls -- every one handed the SAME tx
|        insertWorkflowDefinition (ctx, tx, id, name)
|        insertStatusDefinition   (ctx, tx, status)     for each status
|        insertStepDefinition     (ctx, tx, step)       for each step
|        insertActionDefinition   (ctx, tx, action)       for its actions
|        setInitialStepDefinition (ctx, tx, defID, stepID)
|              |
|              |  each helper maps its OWN error, keyed on the intent
|              |  of the statement it just ran
|              v
|        mapInsertErr(err, name, parentEntity, parentID)     errmap.go
|              23503 on a cascade-driver FK -> NotFoundError{parent}
|              anything else                -> mapWriteErr
|
|  [4] readDefinition(ctx, tx, id)                    catalog.go
|        four queries on the SAME tx, so Create reads its own
|        uncommitted writes back and returns a canonical tree:
|        ordered, every slice non-nil
|
|  [5] tx.Commit(ctx)
|        the reference FKs are DEFERRABLE INITIALLY DEFERRED, so a bad
|        reference surfaces HERE rather than at the offending insert
|        -> mapWriteErr(err, "") -> CrossDefinitionError
|
v
returns the stored WorkflowDefinition
```

## Why the boundaries sit where they do

```
+------------------------------------------------------------------------+
| Why the store cannot open its own transaction                          |
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
| Why error mapping happens at the store, not in Catalog                 |
|                                                                        |
| Postgres reports a referencing INSERT and a referenced DELETE with     |
| byte-identical fields: same SQLSTATE, same constraint name, same       |
| table. Only the human-readable prose differs, and that is locale-      |
| dependent.                                                             |
|                                                                        |
| So the error alone cannot tell you what went wrong. You need to know   |
| which operation produced it -- and that is knowledge only the function |
| that ran the statement has. By the time an error reaches Catalog it is |
| gone.                                                                  |
|                                                                        |
| Hence three wrappers instead of one mapper with an "operation"         |
| argument: an insert helper calls mapInsertErr, a delete helper calls   |
| mapDeleteErr. Intent is implicit in which function you are standing    |
| in, so no call site can pass the wrong one.                            |
+------------------------------------------------------------------------+

+------------------------------------------------------------------------+
| Why one file per table, but one Catalog for all of them                |
|                                                                        |
| SQL changes per table: a migration that alters action_definition       |
| should touch exactly store_action_definition.go.                       |
|                                                                        |
| Orchestration changes per flow, and flows cross tables. Four separate  |
| repositories would force the caller to assemble a cross-entity         |
| transaction themselves -- which is precisely the guarantee the library |
| exists to provide.                                                     |
+------------------------------------------------------------------------+
```
