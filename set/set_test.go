package set_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/set"
)

func TestNewAddRemoveContains(t *testing.T) {
	s := set.New(1, 2, 2, 3)
	assert.Equal(t, 3, s.Len(), "duplicates collapse")
	assert.True(t, s.Contains(2))
	s.Add(4)
	assert.True(t, s.Contains(4))
	s.Remove(2)
	assert.False(t, s.Contains(2))
	assert.False(t, s.IsEmpty())
}

func TestZeroValueUsable(t *testing.T) {
	var s set.Set[int]
	assert.True(t, s.IsEmpty())
	assert.False(t, s.Contains(1))
	s.Add(1) // must lazily allocate
	assert.True(t, s.Contains(1))
}

func TestAlgebra(t *testing.T) {
	a := set.New(1, 2, 3)
	b := set.New(2, 3, 4)
	assert.ElementsMatch(t, []int{1, 2, 3, 4}, a.Union(b).Slice())
	assert.ElementsMatch(t, []int{2, 3}, a.Intersect(b).Slice())
	assert.ElementsMatch(t, []int{1}, a.Diff(b).Slice(), "elements in a not in b")
	// operands unmodified
	assert.Equal(t, 3, a.Len())
	assert.Equal(t, 3, b.Len())
}

func TestEqual(t *testing.T) {
	assert.True(t, set.New(1, 2).Equal(set.New(2, 1)))
	assert.False(t, set.New(1, 2).Equal(set.New(1, 2, 3)))
	assert.False(t, set.New(1, 2).Equal(set.New(1, 3)))
}

func TestSortedAndAll(t *testing.T) {
	s := set.New(3, 1, 2)
	assert.Equal(t, []int{1, 2, 3}, s.Sorted(func(a, b int) bool { return a < b }))

	seen := map[int]bool{}
	for v := range s.All() {
		seen[v] = true
	}
	assert.Equal(t, map[int]bool{1: true, 2: true, 3: true}, seen)
}

func TestCopyAliasesBackingStore(t *testing.T) {
	// Documented caveat: copying a non-empty Set shares the backing map.
	a := set.New(1)
	b := a
	b.Add(2)
	assert.True(t, a.Contains(2), "documented: copy shares the backing store")
}
