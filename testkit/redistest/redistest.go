//go:build integration

package redistest

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"

	"github.com/testcontainers/testcontainers-go/modules/redis"

	"github.com/dmitrymomot/forge/core/id"
)

// RunID is unique per test process. Embed it in key prefixes so re-runs against
// a persistent server (keys leak by design) never collide with prior state.
var RunID = id.NewULID().String()

var (
	sharedOnce sync.Once
	sharedAddr string
)

// Addr returns a "host:port" address of a Redis to test against.
//
// If FORGE_TEST_REDIS_URL is set it is returned verbatim, pointing the suite at
// an existing server (e.g. a CI service). Otherwise a throwaway redis:7-alpine
// container is started once per test process, shared across every test in the
// package, and removed by the testcontainers Ryuk reaper when the process
// exits.
func Addr(tb testing.TB) string {
	tb.Helper()
	if addr := os.Getenv("FORGE_TEST_REDIS_URL"); addr != "" {
		return addr
	}
	sharedOnce.Do(startShared)
	return sharedAddr
}

// startShared boots the shared container. It panics rather than failing a
// single test: a Goexit inside sync.Once still marks it done, which would leave
// sharedAddr empty and make every later caller dial "".
func startShared() {
	ctx := context.Background()
	c, err := redis.Run(ctx, "redis:7-alpine")
	if err != nil {
		panic(fmt.Sprintf("redistest: start container: %v", err))
	}
	conn, err := c.ConnectionString(ctx)
	if err != nil {
		panic(fmt.Sprintf("redistest: connection string: %v", err))
	}
	u, err := url.Parse(conn)
	if err != nil {
		panic(fmt.Sprintf("redistest: parse %q: %v", conn, err))
	}
	sharedAddr = u.Host
}
