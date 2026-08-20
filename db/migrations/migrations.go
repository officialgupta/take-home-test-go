package migrations

import (
	"database/sql"
	"embed"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/pressly/goose/v3"
)

//go:embed *.sql
var FS embed.FS

// NewProvider builds a *goose.Provider from a standard postgres:// URL and
// the embedded migration files. Callers are responsible for calling
// provider.Close() once done.
func NewProvider(databaseURL string) (*goose.Provider, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	provider, err := goose.NewProvider(goose.DialectPostgres, db, FS)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("create goose provider: %w", err)
	}

	return provider, nil
}
