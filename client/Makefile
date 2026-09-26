# Console developer tasks.
#
# The front end builds to web/dist, which the Go binary embeds, so a release is
# one binary. Development runs Vite separately for hot reload, proxying /api to
# this process.

.PHONY: up down build run dev

# Start the console's Postgres and wait for it to accept connections.
up:
	docker compose up -d --wait

down:
	docker compose down

# Build the front end, then the binary that embeds it.
build:
	cd web && npm install && npm run build
	go build -o bin/console .

# Serve the API and the built application from one process.
# Run `make build` first, or after any front-end change.
run:
	go run .

# Development: this serves the API, and `cd web && npm run dev` serves the UI on
# 5173 with hot reload, proxying /api here.
dev: run
