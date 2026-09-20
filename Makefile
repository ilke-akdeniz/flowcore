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

# Passed through to `go test`, for the inner loop:
#   make test TESTFLAGS="-run TestStart -v"
#   make test TESTFLAGS=-count=1
TESTFLAGS ?=

.PHONY: test migrate-test migrate-test-down create-test-db up

# Run the suite: apply migrations, then test against the migrated schema.
#
# Deliberately does not depend on `up`. FLOWCORE_TEST_DSN may point at a database
# that is not the local container, and starting a container in that case would be
# both useless and confusing. Run `make up` yourself once per boot.
test: migrate-test
	FLOWCORE_TEST_DSN="$(FLOWCORE_TEST_DSN)" go test $(TESTFLAGS) ./...

# Apply all migrations to the test database.
migrate-test:
	goose -dir $(MIGRATIONS) -table $(GOOSE_TABLE) postgres "$(FLOWCORE_TEST_DSN)" up

# Roll back the last migration on the test database.
migrate-test-down:
	goose -dir $(MIGRATIONS) -table $(GOOSE_TABLE) postgres "$(FLOWCORE_TEST_DSN)" down

# Start the local docker-compose Postgres and wait until it accepts connections.
# --wait needs the healthcheck in docker-compose.yml; without one it would return
# as soon as the container was running, which is too early for the first query.
up:
	docker compose up -d --wait

# Convenience: create the local test database inside the docker-compose Postgres.
# Depends on `up` because it reaches into the container directly, so unlike
# `test` it cannot work against any other database anyway.
create-test-db: up
	docker exec flowcore-postgres psql -U flowcore -d flowcore -c "create database flowcore_test" || true
