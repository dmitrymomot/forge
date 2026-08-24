package opensearch_test

import (
	"errors"
	"fmt"
	"testing"

	osgo "github.com/opensearch-project/opensearch-go/v4"
	"github.com/stretchr/testify/assert"

	forgeos "github.com/dmitrymomot/forge/data/opensearch"
)

func TestIsNotFound(t *testing.T) {
	// opensearch-go v4 parses api errors into *StructError / *StringError, each
	// carrying Status. A 404 in either shape must classify as not-found, including
	// when wrapped by fmt.Errorf.
	structErr := &osgo.StructError{Status: 404}
	stringErr := &osgo.StringError{Status: 404, Err: "no such index"}

	assert.True(t, forgeos.IsNotFound(structErr))
	assert.True(t, forgeos.IsNotFound(stringErr))

	// Wrap through an error variable: opensearch-go declares Error() on the value type
	// but returns the pointer, so vet's %w check flags the pointer even though the
	// errors.As lookup IsNotFound performs resolves it.
	var wrappedStruct, wrappedString error = structErr, stringErr
	assert.True(t, forgeos.IsNotFound(fmt.Errorf("setup: %w", wrappedStruct)))
	assert.True(t, forgeos.IsNotFound(fmt.Errorf("setup: %w", wrappedString)))

	// Non-404 statuses and unrelated errors are not not-found.
	assert.False(t, forgeos.IsNotFound(&osgo.StructError{Status: 500}))
	assert.False(t, forgeos.IsNotFound(&osgo.StringError{Status: 403, Err: "forbidden"}))
	assert.False(t, forgeos.IsNotFound(errors.New("connection refused")))
	assert.False(t, forgeos.IsNotFound(nil))
}
