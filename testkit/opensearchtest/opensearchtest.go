//go:build integration

package opensearchtest

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/testcontainers/testcontainers-go/modules/opensearch"
)

var (
	sharedOnce sync.Once
	sharedAddr string
)

// Addr returns an "http://host:port" address of an OpenSearch to test against.
//
// If FORGE_TEST_OPENSEARCH_ADDR is set it is returned verbatim, pointing the
// suite at an existing server. Otherwise a throwaway opensearch:2.11.1 container
// (security plugin disabled) is started once per test process, shared across
// every test in the package, and removed by the testcontainers Ryuk reaper when
// the process exits.
func Addr(tb testing.TB) string {
	tb.Helper()
	if addr := os.Getenv("FORGE_TEST_OPENSEARCH_ADDR"); addr != "" {
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
	c, err := opensearch.Run(ctx, "opensearchproject/opensearch:2.11.1")
	if err != nil {
		panic(fmt.Sprintf("opensearchtest: start container: %v", err))
	}
	addr, err := c.Address(ctx)
	if err != nil {
		panic(fmt.Sprintf("opensearchtest: address: %v", err))
	}
	sharedAddr = addr
}
