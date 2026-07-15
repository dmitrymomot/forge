package pgqueue_test

import (
	"fmt"
	"io"
	"os"
	"testing"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
)

// testDSN is the Postgres the suite runs against, set by TestMain.
var testDSN string

// TestMain runs the driver against a real Postgres with no Docker required: by
// default it boots an embedded PostgreSQL 18 (downloaded once, then cached, and
// run natively — so no container-VM clock skew), tearing it down afterwards.
// Set FORGE_TEST_POSTGRES_DSN to point the same suite at an existing server
// (e.g. CI's Postgres service) instead.
func TestMain(m *testing.M) {
	if dsn := os.Getenv("FORGE_TEST_POSTGRES_DSN"); dsn != "" {
		testDSN = dsn
		os.Exit(m.Run())
	}

	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Port(59432).
		Version(embeddedpostgres.V18).
		Logger(io.Discard))
	if err := pg.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "pgqueue: start embedded postgres: %v\n", err)
		os.Exit(1)
	}
	testDSN = "postgres://postgres:postgres@localhost:59432/postgres?sslmode=disable"

	code := m.Run()
	if err := pg.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "pgqueue: stop embedded postgres: %v\n", err)
	}
	os.Exit(code)
}
