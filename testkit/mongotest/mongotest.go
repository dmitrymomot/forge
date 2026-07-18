//go:build integration

package mongotest

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

var (
	sharedOnce sync.Once
	sharedURI  string
)

// URI returns a MongoDB connection string to test against.
//
// If FORGE_TEST_MONGO_URI is set it is returned verbatim, pointing the suite at
// an existing server. Otherwise a throwaway mongo:7 container is started once
// per test process as a single-node replica set — so multi-document
// transactions work — shared across every test in the package and removed by
// the testcontainers Ryuk reaper when the process exits.
//
// Sharding needs a mongos router, which cannot be provisioned this way; the
// sharded suite stays gated on FORGE_TEST_MONGO_SHARDED_URI.
func URI(tb testing.TB) string {
	tb.Helper()
	if uri := os.Getenv("FORGE_TEST_MONGO_URI"); uri != "" {
		return uri
	}
	sharedOnce.Do(startShared)
	return sharedURI
}

// startShared boots the shared container. It panics rather than failing a
// single test: a Goexit inside sync.Once still marks it done, which would leave
// sharedURI empty and make every later caller dial "".
func startShared() {
	ctx := context.Background()
	c, err := mongodb.Run(ctx, "mongo:7", mongodb.WithReplicaSet("rs0"))
	if err != nil {
		panic(fmt.Sprintf("mongotest: start container: %v", err))
	}
	uri, err := c.ConnectionString(ctx)
	if err != nil {
		panic(fmt.Sprintf("mongotest: connection string: %v", err))
	}
	// The module initiates the replica set advertising the container-internal
	// host, which the test process (on the host network) cannot reach — a
	// replicaSet=… URI would hang on topology discovery. directConnection=true
	// dials the single node straight; it is still a primary, so multi-document
	// transactions work. The two options are mutually exclusive, so drop
	// replicaSet.
	u, err := url.Parse(uri)
	if err != nil {
		panic(fmt.Sprintf("mongotest: parse %q: %v", uri, err))
	}
	q := u.Query()
	q.Del("replicaSet")
	q.Set("directConnection", "true")
	u.RawQuery = q.Encode()
	sharedURI = u.String()
	waitForPrimary(ctx, sharedURI)
}

// waitForPrimary blocks until the single-node replica set has a writable primary.
// The module's own readiness check (rs.status().ok) can report true mid-election,
// before any member is writable, so an immediate write can fail with
// "NotWritablePrimary"; retrying Ping(readpref.Primary()) waits out that window.
func waitForPrimary(ctx context.Context, uri string) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	client, err := mongodriver.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		panic(fmt.Sprintf("mongotest: connect: %v", err))
	}
	defer func() {
		dctx, dcancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer dcancel()
		_ = client.Disconnect(dctx)
	}()

	var lastErr error
	for {
		if lastErr = client.Ping(ctx, readpref.Primary()); lastErr == nil {
			return
		}
		select {
		case <-ctx.Done():
			panic(fmt.Sprintf("mongotest: wait for primary: %v (last ping error: %v)", ctx.Err(), lastErr))
		case <-time.After(200 * time.Millisecond):
		}
	}
}
