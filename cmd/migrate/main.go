// Command migrate applies (or rolls back) the database schema.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/pressly/goose/v3"

	"take-home-test-go/db/migrations"
)

func main() {
	if len(os.Args) != 2 || (os.Args[1] != "up" && os.Args[1] != "down") {
		fmt.Fprintln(os.Stderr, "usage: migrate <up|down>")
		os.Exit(2)
	}
	direction := os.Args[1]

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is not set")
	}

	provider, err := migrations.NewProvider(databaseURL)
	if err != nil {
		log.Fatalf("failed to initialize migrator: %v", err)
	}
	defer provider.Close()

	ctx := context.Background()

	var results []*goose.MigrationResult
	if direction == "up" {
		results, err = provider.Up(ctx)
	} else {
		// DownTo(0) matches golang-migrate's old Down() semantics: roll back
		// every applied migration, not just the most recent one.
		results, err = provider.DownTo(ctx, 0)
	}
	if err != nil {
		log.Fatalf("migrate %s failed: %v", direction, err)
	}

	for _, r := range results {
		log.Println(r.String())
	}
	log.Printf("migrate %s: done", direction)
}
