package opensearch_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	forgeos "github.com/dmitrymomot/forge/data/opensearch"
)

func TestClose_NilTolerated(t *testing.T) {
	// Close must tolerate a nil client and a nil logger without panicking; it never
	// touches the network (the HTTP client owns no persistent sockets to release).
	// The live-client path lives in the integration tier (lifecycle_integration_test.go).
	assert.NotPanics(t, func() { forgeos.Close(nil, nil) })
}
