# FlowCore reference client

A working application built on [FlowCore](../README.md) — run it, read it, lift snippets
from it, or fork it as the starting point for a real one.

It is not an SDK. FlowCore is a library you import, not a service you call, so there is
nothing here that wraps it; this is an example of *being* the client.

> **The UI layer is being rebuilt** on React, TypeScript and Mantine — see
> [the slices](../docs/pending-tasks/client-ui-rebuild.md). What follows describes the HTMX UI it
> replaces; the Go side underneath is unchanged.

## Run it

```
docker compose up -d
go run .
```

Then <http://localhost:8080>. Nothing else to configure — the client applies the library's
schema itself, seeds your session on first request, and runs its agent steps from
pre-written findings when no API key is set.

To have a model make those judgments instead:

```
export ANTHROPIC_API_KEY=...
go run .
```

Everything else on the path is identical either way: the same queue, the same worker, the
same `CompleteStep` call, the same remark stamped on the same visit. Only the source of
the judgment changes, and the interface says which is live.

## Why it exists

A boundary is invisible from one side. FlowCore's central claims — that references are
opaque, that the library never calls a model, that the caller owns dispatch, that subjects
live elsewhere — cannot be read from the API alone. Each becomes legible only when
something is shown doing the other half.

So the point of this application is the **call log** at the bottom of every page. It shows
the work this client did beside the one line where FlowCore was involved:

```
  resolve Priya Raman → [user:priya group:qa]   (this person, plus their groups)
→ engine.ListAssignedSteps([user:priya group:qa])
← 1 open step, across every run in the database
  filter to this session's own definitions → 1
```

That ratio is the argument. Identity, subjects, dispatch, tenancy and policy all live on
this side of the line, which is why one engine carries a software release and an insurance
claim without knowing what either one is.

## What to try

- **Switch who you are** (top right). The worklist changes because this application expands
  a person into their groups before asking — FlowCore does not know what a group is.
- **Open a claim or a release.** The subject is labelled as stored here; beneath it is the
  opaque reference and version token that are all FlowCore holds.
- **Act on a step that is not in your queue.** The buttons still work. FlowCore records who
  acted and never decides whether they were allowed to, which is exactly how a person
  overrides an agent.
- **Build your own workflow** under *workflows*. The assignee field is free text with no
  validation — type `anything:at-all` and the workflow still runs. Anything beginning
  `agent:` gets dispatched automatically, which is this application's convention and not the
  library's.
- **Delete a status a step is using**, or reuse a name. The errors are sentences because the
  library returns typed errors rather than one opaque failure.

## How it is built

Server-rendered Go templates with [HTMX](https://htmx.org) available for the places that
need it, [mermaid](https://mermaid.js.org) for the workflow diagram, and a classless
stylesheet from a CDN. No build step, no `node_modules`, no second toolchain — `go run .`
is the whole setup.

That is a deliberate choice rather than an absent one. A JSON API with a client-side store
would put two layers of indirection between the click and the `flowcore` call this
application exists to make visible. The reasoning, and the alternatives that lost, are in
[docs/decisions.md](docs/decisions.md).

```
main.go                 wiring
internal/app/           the client half of the boundary:
                        identity, subjects, sessions, seeding, the agent dispatcher,
                        error translation, the call log
internal/web/           handlers, routes, templates
```

`internal/app` is where to look first. Everything in it exists because FlowCore
deliberately does not do it.

## What this is not

Authentication is faked and there is no user management, because neither demonstrates
anything about the library and both would be the largest code here. Sessions live in
memory. The subject store is a map.

Fork it and those are the first things you would replace — the shape of what reaches
FlowCore would not change at all.
