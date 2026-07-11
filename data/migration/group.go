package migration

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
)

// Set is one migration source: an fs.FS applied under its own goose version
// table. Build it with Source; the zero value is not usable.
type Set struct {
	fsys  fs.FS
	table string
}

// Source pairs an embedded migration FS with the version table it owns. An
// empty table uses DefaultTable ("schema_migrations").
func Source(fsys fs.FS, table string) Set {
	return Set{fsys: fsys, table: table}
}

// GroupMigrator applies several Sets, each under its own version table, in
// declared order. It structurally satisfies postgres.Migrator, so it can be
// passed straight to postgres.WithMigrator. Build it with Group.
type GroupMigrator struct {
	sets []Set
}

// Group builds a composite Migrator over sets. Each Set keeps an independent
// timeline (its own version table), so forge-owned migrations never collide
// with the consumer's app migration numbering.
func Group(sets ...Set) *GroupMigrator {
	return &GroupMigrator{sets: sets}
}

// Up applies each Set's migrations under its own version table, in order. It
// stops at the first error. The db is owned by the caller and never closed.
//
// Before applying anything, it verifies that every Set resolves to a distinct
// version table (empty tables default to DefaultTable); two Sets sharing a
// table would make goose track both under one timeline, silently skipping the
// second source's migrations as "already applied".
func (g *GroupMigrator) Up(ctx context.Context, db *sql.DB) error {
	seen := make(map[string]struct{}, len(g.sets))
	for _, s := range g.sets {
		table := s.table
		if table == "" {
			table = DefaultTable
		}
		if _, dup := seen[table]; dup {
			return fmt.Errorf("%w: %q", ErrDuplicateSource, table)
		}
		seen[table] = struct{}{}
	}

	for _, s := range g.sets {
		if err := New(s.fsys, WithTable(s.table)).Up(ctx, db); err != nil {
			return err
		}
	}
	return nil
}
