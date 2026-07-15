package migration_test

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/data/migration"
)

// oneMigration is a minimal goose SQL migration creating a table. It is shared
// with the integration tier (migration_integration_test.go).
var oneMigration = fstest.MapFS{
	"00001_create_widgets.sql": &fstest.MapFile{Data: []byte(`-- +goose Up
CREATE TABLE widgets (id bigserial PRIMARY KEY, name text NOT NULL);
-- +goose Down
DROP TABLE widgets;
`)},
}

func TestNew_ReturnsMigrator(t *testing.T) {
	// New never returns an error and never touches a database — it only stores
	// config. The default version table is applied lazily inside Up.
	m := migration.New(oneMigration)
	require.NotNil(t, m)

	m = migration.New(oneMigration, migration.WithTable("custom_versions"))
	require.NotNil(t, m)
}
