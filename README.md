# FlowCore

A subject-agnostic workflow library for Go, with human-in-the-loop AI review steps.

FlowCore is a code library plus Postgres schema and migrations — not a service,
API, or application. Clients import it and call it directly.

## Status

Early development. Iteration 1: configure workflow, start workflow, get current
step, complete step.

## Stack

Go, Postgres, pgx v5, hand-written SQL in a repository layer, plain SQL migrations.

## License

MIT — see [LICENSE](LICENSE).
