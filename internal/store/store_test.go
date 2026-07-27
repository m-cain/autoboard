package store_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/m-cain/autoboard/internal/store"
)

func TestOpenAppliesSQLiteOperatingRulesAndMigrations(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "autoboard.db")
	db, err := store.Open(ctx, path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	for _, fixture := range []struct {
		query string
		want  any
	}{
		{query: "PRAGMA journal_mode", want: "wal"},
		{query: "PRAGMA foreign_keys", want: int64(1)},
		{query: "PRAGMA synchronous", want: int64(1)},
		{query: "PRAGMA busy_timeout", want: int64(5000)},
	} {
		var text string
		if expected, ok := fixture.want.(string); ok {
			if err := db.QueryRowContext(ctx, fixture.query).Scan(&text); err != nil {
				t.Fatalf("%s: %v", fixture.query, err)
			}
			if text != expected {
				t.Errorf("%s = %q, want %q", fixture.query, text, expected)
			}
			continue
		}
		var number int64
		if err := db.QueryRowContext(ctx, fixture.query).Scan(&number); err != nil {
			t.Fatalf("%s: %v", fixture.query, err)
		}
		if number != fixture.want {
			t.Errorf("%s = %d, want %v", fixture.query, number, fixture.want)
		}
	}
	version, err := store.MigrationVersion(ctx, db)
	if err != nil {
		t.Fatalf("migration version: %v", err)
	}
	if version != 1 {
		t.Errorf("migration version = %d, want 1", version)
	}
	if got := db.Stats().MaxOpenConnections; got != 4 {
		t.Errorf("maximum open connections = %d, want bounded reader pool", got)
	}
	if _, err := db.ExecContext(
		ctx,
		`INSERT INTO tickets
		 (id, project_id, number, title, inserted_at, updated_at)
		 VALUES ('ticket', 'missing-project', 1, 'Invalid', 'now', 'now')`,
	); err == nil {
		t.Fatal("foreign-key violating insert succeeded")
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	reopened, err := store.Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen WAL store: %v", err)
	}
	defer reopened.Close()
	reopenedVersion, err := store.MigrationVersion(ctx, reopened)
	if err != nil {
		t.Fatalf("reopened migration version: %v", err)
	}
	if reopenedVersion != version {
		t.Errorf(
			"reopened migration version = %d, want %d",
			reopenedVersion,
			version,
		)
	}
}
