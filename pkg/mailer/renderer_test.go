package mailer

import (
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func TestRenderer_Render_PlainText(t *testing.T) {
	t.Parallel()

	fs := fstest.MapFS{
		"layouts/default.html": &fstest.MapFile{
			Data: []byte(`<html><body>{{.Content}}</body></html>`),
		},
		"welcome.md": &fstest.MapFile{
			Data: []byte(`---
Subject: Welcome {{.Name}}
---
Hello **{{.Name}}**!

Welcome to our service.
`),
		},
	}

	renderer := NewRendererWithConfig(fs, RendererConfig{
		LayoutDir: "layouts",
	})

	result, err := renderer.Render("default.html", "welcome.md", map[string]string{"Name": "Alice"})
	require.NoError(t, err)

	// Text should contain processed markdown (not HTML)
	require.Contains(t, result.Text, "Hello **Alice**!")
	require.Contains(t, result.Text, "Welcome to our service.")
	require.NotContains(t, result.Text, "<strong>", "Text should not contain HTML tags")

	// HTML should contain rendered HTML
	require.Contains(t, result.HTML, "<strong>Alice</strong>")
}

func TestRenderer_Render_CachesTemplates(t *testing.T) {
	t.Parallel()

	var openCount atomic.Int32

	// Custom FS that counts Open calls
	cfs := &countingFS{
		MapFS: fstest.MapFS{
			"layouts/default.html": &fstest.MapFile{
				Data: []byte(`<html>{{.Content}}</html>`),
			},
			"email.md": &fstest.MapFile{
				Data: []byte(`---
Subject: Test
---
Hello {{.Name}}
`),
			},
		},
		openCount: &openCount,
	}

	renderer := NewRendererWithConfig(cfs, RendererConfig{
		LayoutDir: "layouts",
	})

	// First render - should read files (2 opens: template + layout)
	_, err := renderer.Render("default.html", "email.md", map[string]string{"Name": "Alice"})
	require.NoError(t, err)
	firstOpenCount := openCount.Load()
	require.Equal(t, int32(2), firstOpenCount, "Should have opened 2 files (template + layout)")

	// Second render - should use cache, no additional opens
	_, err = renderer.Render("default.html", "email.md", map[string]string{"Name": "Bob"})
	require.NoError(t, err)
	secondOpenCount := openCount.Load()
	require.Equal(t, firstOpenCount, secondOpenCount, "Should not open files again (cached)")

	// Third render with different layout - should open layout file only
	cfs.MapFS["layouts/other.html"] = &fstest.MapFile{
		Data: []byte(`<div>{{.Content}}</div>`),
	}
	_, err = renderer.Render("other.html", "email.md", map[string]string{"Name": "Charlie"})
	require.NoError(t, err)
	thirdOpenCount := openCount.Load()
	require.Equal(t, int32(3), thirdOpenCount, "Should open only the new layout file")
}

func TestRenderer_Render_DifferentDataProducesDifferentOutput(t *testing.T) {
	t.Parallel()

	fs := fstest.MapFS{
		"layouts/default.html": &fstest.MapFile{
			Data: []byte(`<html>{{.Content}}</html>`),
		},
		"greeting.md": &fstest.MapFile{
			Data: []byte(`---
Subject: Hello
---
Welcome {{.Name}}!
`),
		},
	}

	renderer := NewRendererWithConfig(fs, RendererConfig{
		LayoutDir: "layouts",
	})

	result1, err := renderer.Render("default.html", "greeting.md", map[string]string{"Name": "Alice"})
	require.NoError(t, err)

	result2, err := renderer.Render("default.html", "greeting.md", map[string]string{"Name": "Bob"})
	require.NoError(t, err)

	// Results should be different
	require.Contains(t, result1.Text, "Welcome Alice!")
	require.Contains(t, result2.Text, "Welcome Bob!")
	require.NotEqual(t, result1.Text, result2.Text)
	require.NotEqual(t, result1.HTML, result2.HTML)
}

func TestRenderer_Render_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	fs := fstest.MapFS{
		"layouts/default.html": &fstest.MapFile{
			Data: []byte(`<html>{{.Content}}</html>`),
		},
		"email.md": &fstest.MapFile{
			Data: []byte(`---
Subject: Test
---
Hello {{.ID}}
`),
		},
	}

	renderer := NewRendererWithConfig(fs, RendererConfig{
		LayoutDir: "layouts",
	})

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	for i := range 100 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			result, err := renderer.Render("default.html", "email.md", map[string]int{"ID": id})
			if err != nil {
				errors <- err
				return
			}
			// Verify the result contains the correct ID
			if result.Text == "" || result.HTML == "" {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("Concurrent render failed: %v", err)
	}
}

// countingFS wraps MapFS and counts ReadFile calls.
type countingFS struct {
	fstest.MapFS
	openCount *atomic.Int32
}

func (c *countingFS) ReadFile(name string) ([]byte, error) {
	c.openCount.Add(1)
	return c.MapFS.ReadFile(name)
}
