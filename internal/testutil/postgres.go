// Package testutil provides shared test infrastructure for integration
// tests that need a real Postgres — no manual `docker compose up` required,
// `go test ./...` spins up and tears down its own throwaway container.
package testutil

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"take-home-test-go/db/migrations"
)

// NewPostgresPool starts a throwaway Postgres container, applies the same
// embedded migrations cmd/migrate applies in production, and returns a pool
// connected to it. Both the pool and the container are torn down
// automatically via tb.Cleanup.
//
// Call once per test package (e.g. from TestMain), not once per test
// function — container startup takes a few seconds, and every test in the
// package can safely share one instance since each test is expected to use
// its own application_reference values.
func NewPostgresPool(tb testing.TB) *pgxpool.Pool {
	tb.Helper()
	ctx := context.Background()

	container, err := postgres.Run(ctx, "postgres:17-alpine",
		postgres.WithDatabase("formstore"),
		postgres.WithUsername("formstore"),
		postgres.WithPassword("formstore"),
		// Postgres restarts itself once after initdb on first boot; without
		// this, a connection can land in that restart window and get reset.
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		tb.Fatalf("start postgres container: %v", err)
	}
	tb.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			tb.Logf("terminate postgres container: %v", err)
		}
	})

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		tb.Fatalf("get connection string: %v", err)
	}

	provider, err := migrations.NewProvider(connStr)
	if err != nil {
		tb.Fatalf("create migration provider: %v", err)
	}
	defer provider.Close()
	if _, err := provider.Up(ctx); err != nil {
		tb.Fatalf("apply migrations: %v", err)
	}

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		tb.Fatalf("connect pool: %v", err)
	}
	tb.Cleanup(pool.Close)

	return pool
}
