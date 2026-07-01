package mapx_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/mapx"
)

func TestOrdered_InsertionOrder(t *testing.T) {
	o := mapx.NewOrdered[string, int]()
	o.Set("c", 3)
	o.Set("a", 1)
	o.Set("b", 2)
	assert.Equal(t, []string{"c", "a", "b"}, o.Keys())

	o.Set("a", 11) // update keeps position
	assert.Equal(t, []string{"c", "a", "b"}, o.Keys())
	v, ok := o.Get("a")
	assert.True(t, ok)
	assert.Equal(t, 11, v)

	o.Delete("c")
	assert.Equal(t, []string{"a", "b"}, o.Keys())
	o.Set("c", 30) // re-add appends at end
	assert.Equal(t, []string{"a", "b", "c"}, o.Keys())
	assert.Equal(t, 3, o.Len())
}

func TestOrdered_All(t *testing.T) {
	o := mapx.NewOrdered[string, int]()
	o.Set("x", 1)
	o.Set("y", 2)
	var keys []string
	var vals []int
	for k, v := range o.All() {
		keys = append(keys, k)
		vals = append(vals, v)
	}
	assert.Equal(t, []string{"x", "y"}, keys)
	assert.Equal(t, []int{1, 2}, vals)
}

func TestOrdered_JSONPreservesOrder(t *testing.T) {
	o := mapx.NewOrdered[string, int]()
	o.Set("z", 26)
	o.Set("a", 1)
	o.Set("m", 13)
	b, err := json.Marshal(o)
	require.NoError(t, err)
	assert.Equal(t, `{"z":26,"a":1,"m":13}`, string(b), "marshal preserves insertion order")

	var got mapx.Ordered[string, int]
	require.NoError(t, json.Unmarshal([]byte(`{"z":26,"a":1,"m":13}`), &got))
	assert.Equal(t, []string{"z", "a", "m"}, got.Keys(), "unmarshal preserves source key order")
	v, ok := got.Get("m")
	assert.True(t, ok)
	assert.Equal(t, 13, v)
}

func TestOrdered_UnmarshalNull(t *testing.T) {
	var o mapx.Ordered[string, int]
	require.NoError(t, json.Unmarshal([]byte(`null`), &o))
	assert.Equal(t, 0, o.Len())
}

func TestOrdered_UnmarshalRejectsTrailingData(t *testing.T) {
	var o mapx.Ordered[string, int]
	// A direct UnmarshalJSON call with trailing content after the object must
	// error rather than silently ignore it. (json.Unmarshal already guards this
	// at the top level; this makes the method self-validating for direct and
	// streaming callers.)
	err := o.UnmarshalJSON([]byte(`{"a":1} garbage`))
	assert.Error(t, err)
}
