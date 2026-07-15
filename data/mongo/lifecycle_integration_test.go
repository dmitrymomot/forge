//go:build integration

package mongo_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"

	forgemongo "github.com/dmitrymomot/forge/data/mongo"
	"github.com/dmitrymomot/forge/testkit/mongotest"
)

func mongoURI(t *testing.T) string {
	t.Helper()
	return mongotest.URI(t)
}

func openTestDB(t *testing.T) *mongodriver.Database {
	t.Helper()
	cfg := forgemongo.DefaultConfig()
	cfg.URI = mongoURI(t)
	cfg.Database = "forge_test"
	db, err := forgemongo.Open(t.Context(), forgemongo.WithConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(func() { forgemongo.Close(db, nil) })
	return db
}

func TestHealthcheck_Integration(t *testing.T) {
	db := openTestDB(t)

	hc := forgemongo.Healthcheck(db)
	require.NotNil(t, hc)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	require.NoError(t, hc(ctx), "healthcheck against a live server must succeed")
}

func TestHealthcheck_FailsWrapped(t *testing.T) {
	db := openTestDB(t)
	// Close the client, then the healthcheck must fail with an ErrHealthcheck-wrapped
	// error rather than panicking.
	forgemongo.Close(db, nil)

	hc := forgemongo.Healthcheck(db)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	err := hc(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, forgemongo.ErrHealthcheck)
}
