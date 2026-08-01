# FlowCore developer tasks.
#
# Tests run against a real Postgres. The schema is applied by goose (which is not
# a Go dependency — it is invoked here as an installed CLI), then `go test` runs
# against it. `make test` does both, so daily use is one command.

# Override to point at another database, e.g.
#   make test FLOWCORE_TEST_DSN=postgres://user:pass@host:5432/db?sslmode=disable
FLOWCORE_TEST_DSN ?= postgres://flowcore:flowcore@localhost:5432/flowcore_test?sslmode=disable

# Distinct version table so FlowCore's migration history never collides with a
# client that also uses goose for their own migrations.
GOOSE_TABLE := public.flowcore_goose_db_version
MIGRATIONS  := migrations

.PHONY: test migrate-test migrate-test-down create-test-db

# Run the suite: apply migrations, then test against the migrated schema.
test: migrate-test
	FLOWCORE_TEST_DSN="$(FLOWCORE_TEST_DSN)" go test ./...

# Apply all migrations to the test database.
migrate-test:
	goose -dir $(MIGRATIONS) -table $(GOOSE_TABLE) postgres "$(FLOWCORE_TEST_DSN)" up

# Roll back the last migration on the test database.
migrate-test-down:
	goose -dir $(MIGRATIONS) -table $(GOOSE_TABLE) postgres "$(FLOWCORE_TEST_DSN)" down

# Convenience: create the local test database inside the docker-compose Postgres.
create-test-db:
	docker exec flowcore-postgres psql -U flowcore -d flowcore -c "create database flowcore_test" || true
