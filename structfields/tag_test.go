package structfields_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/structfields"
)

func TestTag_Ignored(t *testing.T) {
	assert.True(t, structfields.Tag{Name: "-"}.Ignored())
	assert.False(t, structfields.Tag{Name: "field"}.Ignored())
	assert.False(t, structfields.Tag{Name: ""}.Ignored())
}

func TestTag_HasOption(t *testing.T) {
	tg := structfields.Tag{Name: "field", Options: []string{"omitempty", "string"}}
	assert.True(t, tg.HasOption("omitempty"))
	assert.True(t, tg.HasOption("string"))
	assert.False(t, tg.HasOption("required"))
	assert.False(t, tg.HasOption(""))

	empty := structfields.Tag{Name: "field"}
	assert.False(t, empty.HasOption("omitempty"), "nil Options never matches")
}
