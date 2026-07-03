package structfields_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/core/structfields"
)

func TestSentinels_Messages(t *testing.T) {
	assert.EqualError(t, structfields.ErrNotStruct, "structfields: not a struct")
	assert.EqualError(t, structfields.ErrNotSettable, "structfields: field not settable")
}
