DATABASE_URL ?= postgres://formstore:formstore@localhost:5433/formstore?sslmode=disable
SQLC_VERSION ?= v1.31.1

.PHONY: up down migrate-up migrate-down run test sqlc fmt vet

up:
	docker compose up -d

down:
	docker compose down

migrate-up:
	DATABASE_URL=$(DATABASE_URL) go run ./cmd/migrate up

migrate-down:
	DATABASE_URL=$(DATABASE_URL) go run ./cmd/migrate down

run:
	DATABASE_URL=$(DATABASE_URL) go run ./cmd/server

# Runs unit tests and the real-Postgres integration tests together — the
# latter spin up their own throwaway container via testcontainers-go, so
# nothing needs to be running first (no `make up` required).
test:
	go test ./...

# Regenerates internal/store from db/queries/*.sql against the schema in
# db/migrations. sqlc itself is a dev-time codegen tool, not a module
# dependency (see go.mod) — this runs the pinned version directly via
# `go run`, no separate install step.
sqlc:
	go run github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION) generate

fmt:
	go fmt ./...

vet:
	go vet ./...
