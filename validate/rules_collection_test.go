package validate_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/validate"
)

func TestCollections(t *testing.T) {
	assert.True(t, validate.MinItems[string](2)([]string{"a", "b"}).IsZero())
	assert.Equal(t, "validation.min_items", validate.MinItems[string](2)([]string{"a"}).Key)

	assert.True(t, validate.MaxItems[int](2)([]int{1, 2}).IsZero())
	assert.Equal(t, "validation.max_items", validate.MaxItems[int](2)([]int{1, 2, 3}).Key)

	assert.True(t, validate.UniqueItems([]string{"a", "b", "c"}).IsZero())
	assert.Equal(t, "validation.unique_items", validate.UniqueItems([]string{"a", "b", "a"}).Key)
}

func TestIs(t *testing.T) {
	even := validate.Is(func(n int) bool { return n%2 == 0 }, "validation.even")
	assert.True(t, even(4).IsZero())
	assert.Equal(t, "validation.even", even(3).Key)
}
