package mapx_test

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/mapx"
)

func TestMerge(t *testing.T) {
	got := mapx.Merge(map[string]int{"a": 1, "b": 2}, map[string]int{"b": 20, "c": 3})
	assert.Equal(t, map[string]int{"a": 1, "b": 20, "c": 3}, got, "later maps win")
}

func TestMapValues(t *testing.T) {
	got := mapx.MapValues(map[string]int{"a": 1, "b": 2}, func(v int) int { return v * 10 })
	assert.Equal(t, map[string]int{"a": 10, "b": 20}, got)
}

func TestInvert(t *testing.T) {
	got := mapx.Invert(map[string]int{"a": 1, "b": 2})
	assert.Equal(t, map[int]string{1: "a", 2: "b"}, got)
}

func TestFilter(t *testing.T) {
	got := mapx.Filter(map[string]int{"a": 1, "b": 2, "c": 3}, func(_ string, v int) bool { return v%2 == 1 })
	assert.Equal(t, map[string]int{"a": 1, "c": 3}, got)
}

func TestEntriesRoundTrip(t *testing.T) {
	m := map[string]int{"a": 1, "b": 2}
	es := mapx.Entries(m)
	sort.Slice(es, func(i, j int) bool { return es[i].Key < es[j].Key })
	assert.Equal(t, []mapx.Entry[string, int]{{Key: "a", Value: 1}, {Key: "b", Value: 2}}, es)
	assert.Equal(t, m, mapx.FromEntries(es))
}
