//go:build integration

package clickhousetest

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/testcontainers/testcontainers-go/modules/clickhouse"
)

var (
	sharedOnce sync.Once
	sharedDSN  string
)

// DSN returns a ClickHouse connection string to test against.
//
// If FORGE_TEST_CLICKHOUSE_DSN is set it is returned verbatim, pointing the
// suite at an existing server. Otherwise a throwaway clickhouse-server container
// is started once per test process, shared across every test in the package,
// and removed by the testcontainers Ryuk reaper when the process exits.
func DSN(tb testing.TB) string {
	tb.Helper()
	if dsn := os.Getenv("FORGE_TEST_CLICKHOUSE_DSN"); dsn != "" {
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
	c, err := clickhouse.Run(ctx, "clickhouse/clickhouse-server:24.3-alpine")
	if err != nil {
		panic(fmt.Sprintf("clickhousetest: start container: %v", err))
	}
	dsn, err := c.ConnectionString(ctx)
	if err != nil {
		panic(fmt.Sprintf("clickhousetest: connection string: %v", err))
	}
	sharedDSN = dsn
}
