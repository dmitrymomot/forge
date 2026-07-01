package slicex_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/slicex"
)

func TestMap(t *testing.T) {
	got := slicex.Map([]int{1, 2, 3}, func(v int) string { return string(rune('a' + v)) })
	assert.Equal(t, []string{"b", "c", "d"}, got)

	assert.Nil(t, slicex.Map(nil, func(v int) int { return v }), "nil in -> nil out")
	assert.Equal(t, []int{}, slicex.Map([]int{}, func(v int) int { return v }), "empty non-nil -> empty non-nil")
}

func TestFilter(t *testing.T) {
	got := slicex.Filter([]int{1, 2, 3, 4}, func(v int) bool { return v%2 == 0 })
	assert.Equal(t, []int{2, 4}, got)
	assert.Nil(t, slicex.Filter(nil, func(int) bool { return true }))
}

func TestReduce(t *testing.T) {
	sum := slicex.Reduce([]int{1, 2, 3}, 0, func(acc, v int) int { return acc + v })
	assert.Equal(t, 6, sum)
	assert.Equal(t, 10, slicex.Reduce(nil, 10, func(acc, v int) int { return acc + v }), "empty -> init")
}

func TestGroupBy(t *testing.T) {
	got := slicex.GroupBy([]int{1, 2, 3, 4}, func(v int) int { return v % 2 })
	assert.Equal(t, map[int][]int{0: {2, 4}, 1: {1, 3}}, got)
}

func TestKeyBy(t *testing.T) {
	type u struct {
		id   int
		name string
	}
	got := slicex.KeyBy([]u{{1, "a"}, {2, "b"}, {1, "c"}}, func(x u) int { return x.id })
	assert.Equal(t, u{1, "c"}, got[1], "last value wins on duplicate key")
	assert.Equal(t, u{2, "b"}, got[2])
}

func TestUnique(t *testing.T) {
	assert.Equal(t, []int{3, 1, 2}, slicex.Unique([]int{3, 1, 3, 2, 1}), "first-seen order preserved")
	assert.Nil(t, slicex.Unique[int](nil))
}

func TestFlatten(t *testing.T) {
	assert.Equal(t, []int{1, 2, 3, 4}, slicex.Flatten([][]int{{1, 2}, {}, {3, 4}}))
	assert.Nil(t, slicex.Flatten[int](nil))
}

func TestChunk(t *testing.T) {
	assert.Equal(t, [][]int{{1, 2}, {3, 4}, {5}}, slicex.Chunk([]int{1, 2, 3, 4, 5}, 2))
	assert.Nil(t, slicex.Chunk[int](nil, 2))
}

func TestChunk_PanicsOnNonPositiveN(t *testing.T) {
	require.Panics(t, func() { slicex.Chunk([]int{1}, 0) })
	require.Panics(t, func() { slicex.Chunk([]int{1}, -1) })
}

func TestChunk_NoAliasingAppend(t *testing.T) {
	src := []int{1, 2, 3, 4}
	chunks := slicex.Chunk(src, 2)
	chunks[0] = append(chunks[0], 99) // must not overwrite src[2]
	assert.Equal(t, 3, src[2], "chunk append must not spill into the source slice")
}
