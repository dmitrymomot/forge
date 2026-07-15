//go:build integration

package redistest

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/testcontainers/testcontainers-go/modules/redis"
)

var (
	sharedOnce sync.Once
	sharedAddr string
)

// Addr returns a "host:port" address of a Redis to test against.
//
// If FORGE_TEST_REDIS_URL is set it points the suite at an existing server (e.g.
// a CI service); it may be given as "host:port" or as a "redis://host:port" URL,
// and either way Addr returns the bare host:port. Otherwise a throwaway
// redis:7-alpine container is started once per test process, shared across every
// test in the package, and removed by the testcontainers Ryuk reaper when the
// process exits.
func Addr(tb testing.TB) string {
	tb.Helper()
	if addr := os.Getenv("FORGE_TEST_REDIS_URL"); addr != "" {
		return hostPort(addr)
	}
	sharedOnce.Do(startShared)
	return sharedAddr
}

// hostPort normalizes a Redis address to bare "host:port", accepting either that
// form directly or a "redis://host:port" URL.
func hostPort(addr string) string {
	if strings.Contains(addr, "://") {
		if u, err := url.Parse(addr); err == nil && u.Host != "" {
			return u.Host
		}
	}
	return addr
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
	sharedAddr = hostPort(conn)
}
