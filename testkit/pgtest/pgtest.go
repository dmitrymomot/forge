//go:build integration

package pgtest

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

var (
	sharedOnce sync.Once
	sharedDSN  string
)

// DSN returns a Postgres connection string to test against.
//
// If FORGE_TEST_POSTGRES_DSN is set it is returned verbatim, pointing the suite
// at an existing server (e.g. a CI service). Otherwise a throwaway
// postgres:16-alpine container is started once per test process, shared across
// every test in the package, and removed by the testcontainers Ryuk reaper when
// the process exits.
func DSN(tb testing.TB) string {
	tb.Helper()
	if dsn := os.Getenv("FORGE_TEST_POSTGRES_DSN"); dsn != "" {
		return dsn
	}
	sharedOnce.Do(startShared)
	return sharedDSN
}

// startShared boots the shared container. It panics rather than failing a
// single test: a Goexit inside sync.Once still marks it done, which would leave
// sharedDSN empty and make every later caller dial "".
func startShared() {
	ctx := context.Background()
	c, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("forge"),
		postgres.WithUsername("forge"),
		postgres.WithPassword("forge"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		panic(fmt.Sprintf("pgtest: start container: %v", err))
	}
	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		panic(fmt.Sprintf("pgtest: connection string: %v", err))
	}
	sharedDSN = dsn
}
