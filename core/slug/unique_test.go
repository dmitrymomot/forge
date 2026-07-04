package slug_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/core/slug"
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

	t.Run("empty base increments without a leading separator", func(t *testing.T) {
		// A base that folds to "" and is 'taken' produces the bare number — no
		// leading separator, matching the package's no-leading-separator invariant.
		taken := map[string]bool{"": true}
		got := slug.Unique("你好", func(c string) bool { return taken[c] })
		assert.Equal(t, "2", got)
	})

	t.Run("honors max length by shrinking the base", func(t *testing.T) {
		// base "abcdefgh" is exactly maxLength; a collision forces "-2", so the base
		// must be cut to 6 runes to keep the whole candidate within 8.
		base := slug.Make("abcdefgh", slug.WithMaxLength(8))
		taken := map[string]bool{base: true}
		got := slug.Unique("abcdefgh", func(c string) bool { return taken[c] }, slug.WithMaxLength(8))
		assert.Equal(t, "abcdef-2", got)
		assert.LessOrEqual(t, len([]rune(got)), 8)
		assert.NotEqual(t, byte('-'), got[len(got)-1], "no trailing separator")
	})

	t.Run("tiny max length drops base to the bare number", func(t *testing.T) {
		// maxLength too small for base+sep+number: the base is dropped and the bare
		// number is emitted (an inherent limit — the counter cannot be truncated).
		base := slug.Make("a")
		taken := map[string]bool{base: true}
		got := slug.Unique("a", func(c string) bool { return taken[c] }, slug.WithMaxLength(1))
		assert.Equal(t, "2", got)
	})

	t.Run("max length with custom separator", func(t *testing.T) {
		// Same maxLength shrink as above, but with a custom "_" separator, to
		// confirm the budget calculation isn't hardcoded to "-".
		base := slug.Make("abcdefgh", slug.WithMaxLength(8), slug.WithSeparator("_"))
		taken := map[string]bool{base: true}
		got := slug.Unique("abcdefgh", func(c string) bool { return taken[c] },
			slug.WithMaxLength(8), slug.WithSeparator("_"))
		assert.Equal(t, "abcdef_2", got)
		assert.LessOrEqual(t, len([]rune(got)), 8)
	})
}
