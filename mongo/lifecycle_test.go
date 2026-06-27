package mongo_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"

	forgemongo "github.com/dmitrymomot/forge/mongo"
)

func TestClose_NilLoggerTolerated(t *testing.T) {
	// Close must tolerate a nil *mongo.Client and a nil logger without panicking;
	// the log line is simply skipped. This is the pure, server-free contract.
	assert.NotPanics(t, func() {
		forgemongo.Close(nil, nil)
	})
	assert.NotPanics(t, func() {
		forgemongo.Close(nil, slog.New(slog.DiscardHandler))
	})
}

func mongoURI(t *testing.T) string {
	t.Helper()
	uri := os.Getenv("FORGE_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("FORGE_TEST_MONGO_URI not set; skipping integration test")
	}
	return uri
}

func openTestClient(t *testing.T) *mongodriver.Client {
	t.Helper()
	cfg := forgemongo.DefaultConfig()
	cfg.URI = mongoURI(t)
	c, err := forgemongo.Open(t.Context(), forgemongo.WithConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(func() { forgemongo.Close(c, nil) })
	return c
}

func TestHealthcheck_Integration(t *testing.T) {
	c := openTestClient(t)

	hc := forgemongo.Healthcheck(c)
	require.NotNil(t, hc)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	require.NoError(t, hc(ctx), "healthcheck against a live server must succeed")
}

func TestHealthcheck_FailsWrapped(t *testing.T) {
	c := openTestClient(t)
	// Close the client, then the healthcheck must fail with an ErrHealthcheck-wrapped
	// error rather than panicking.
	forgemongo.Close(c, nil)

	hc := forgemongo.Healthcheck(c)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	err := hc(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, forgemongo.ErrHealthcheck)
}
