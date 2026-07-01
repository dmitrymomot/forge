package slug_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/slug"
)

func TestUnique(t *testing.T) {
	t.Run("base free", func(t *testing.T) {
		got := slug.Unique("Hello World", func(string) bool { return false })
		assert.Equal(t, "hello-world", got)
	})

	t.Run("base taken, -2 free", func(t *testing.T) {
		taken := map[string]bool{"hello-world": true}
		got := slug.Unique("Hello World", func(c string) bool { return taken[c] })
		assert.Equal(t, "hello-world-2", got)
	})

	t.Run("increments until free", func(t *testing.T) {
		taken := map[string]bool{
			"post": true, "post-2": true, "post-3": true,
		}
		got := slug.Unique("post", func(c string) bool { return taken[c] })
		assert.Equal(t, "post-4", got)
	})

	t.Run("honors options (custom separator)", func(t *testing.T) {
		taken := map[string]bool{"hello_world": true}
		got := slug.Unique("Hello World", func(c string) bool { return taken[c] }, slug.WithSeparator("_"))
		assert.Equal(t, "hello_world_2", got)
	})

	t.Run("empty base still increments", func(t *testing.T) {
		// A base that folds to "" and is 'taken' still produces a stable candidate.
		taken := map[string]bool{"": true}
		got := slug.Unique("你好", func(c string) bool { return taken[c] })
		assert.Equal(t, "-2", got)
	})
}
