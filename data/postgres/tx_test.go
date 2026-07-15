package postgres_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/data/postgres"
)

func TestRetryOption_Defaults(t *testing.T) {
	// The defaults are observable through behavior in the integration test; here we
	// assert the options are constructible and chainable without panic.
	assert.NotPanics(t, func() {
		_ = postgres.WithRetryAttempts(5)
		_ = postgres.WithRetryInterval(10 * time.Millisecond)
	})
}
