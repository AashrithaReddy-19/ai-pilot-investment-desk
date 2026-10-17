package store

import (
	"context"
	"database/sql"
	"fmt"
	"io"

	"github.com/pressly/goose/v3"

	// registers the "pgx" database/sql driver used by goose
	_ "github.com/jackc/pgx/v5/stdlib"

	"c23desk/internal/migrations"
)

// Migrate runs goose against the embedded SQL files. action: up | down (one step) | status.
func Migrate(ctx context.Context, dsn, action string, out io.Writer) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}
	switch action {
	case "up":
		results, err := provider.Up(ctx)
		if err != nil {
			return fmt.Errorf("migrate up: %w", err)
		}
		if len(results) == 0 {
			fmt.Fprintln(out, "database is up to date")
		}
		for _, r := range results {
			fmt.Fprintf(out, "applied %s (%s)\n", r.Source.Path, r.Duration)
		}
	case "down":
		r, err := provider.Down(ctx)
		if err != nil {
			return fmt.Errorf("migrate down: %w", err)
		}
		fmt.Fprintf(out, "rolled back %s\n", r.Source.Path)
	case "status":
		statuses, err := provider.Status(ctx)
		if err != nil {
			return fmt.Errorf("migrate status: %w", err)
		}
		for _, s := range statuses {
			fmt.Fprintf(out, "%-10s %s\n", s.State, s.Source.Path)
		}
	default:
		return fmt.Errorf("unknown migrate action %q (use up, down or status)", action)
	}
	return nil
}
