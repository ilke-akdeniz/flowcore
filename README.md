# FlowCore

A subject-agnostic workflow library for Go, with human-in-the-loop AI review steps.

FlowCore is a code library plus Postgres schema and migrations — not a service, API, or application.
Clients import it and call it directly.
The library records opaque references — subject, assignee, completer — and never interprets them; authorization and identity stay in the client.

## Status

Early development.
Iteration 1: configure workflow, start workflow, get current step, complete step.

## Stack

Go, Postgres, pgx v5 used natively, hand-written SQL in a repository layer, plain SQL migrations.

## Design

The authoritative design lives in [`docs/system-design.md`](docs/system-design.md): boundary, flows, state, responsibilities, invariants, and trade-offs.
[`docs/decisions.md`](docs/decisions.md) is a running log of the design decisions behind it — each with the options weighed and the reasoning that settled it.

## How this is built

Design decisions are worked out and recorded before code is written.
Each is stress-tested through its alternatives, settled with a human in the loop, and landed in the design doc first; implementation follows the doc.
`docs/decisions.md` is the trail.

One deliberate idiom deviation: identifiers spell out full domain words rather than Go's typical short local names, with a narrow receiver-like exception for a function's single dominant parameter. Reasons recorded in decision 18.

## License

MIT — see [LICENSE](LICENSE).
</file_text>
