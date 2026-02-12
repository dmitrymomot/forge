package forgetest

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

const testHTML = `<html><body>
<h1 class="title">Hello World</h1>
<input type="text" name="email" value="test@example.com">
<div class="item">One</div>
<div class="item">Two</div>
<a href="/home" data-action="navigate">Home</a>
</body></html>`

func TestDocument_RequireExists(t *testing.T) {
	t.Parallel()

	t.Run("succeeds when element exists", func(t *testing.T) {
		t.Parallel()
		doc := newDocument(t, testHTML)
		doc.RequireExists(t, "h1.title")
	})

	t.Run("succeeds with multiple matches", func(t *testing.T) {
		t.Parallel()
		doc := newDocument(t, testHTML)
		doc.RequireExists(t, ".item")
	})

	t.Run("fails when element does not exist", func(t *testing.T) {
		t.Parallel()
		doc := newDocument(t, testHTML)

		mt := expectFailure(t, func(mt *mockT) {
			doc.RequireExists(mt, ".nonexistent")
		})

		require.True(t, mt.failed, "expected test to fail")
		require.Contains(t, mt.message, `expected element matching ".nonexistent" to exist, but none found`)
	})
}

func TestDocument_RequireNotExists(t *testing.T) {
	t.Parallel()

	t.Run("succeeds when no elements match", func(t *testing.T) {
		t.Parallel()
		doc := newDocument(t, testHTML)
		doc.RequireNotExists(t, ".nonexistent")
	})

	t.Run("fails when element exists", func(t *testing.T) {
		t.Parallel()
		doc := newDocument(t, testHTML)

		mt := expectFailure(t, func(mt *mockT) {
			doc.RequireNotExists(mt, "h1.title")
		})

		require.True(t, mt.failed, "expected test to fail")
		require.Contains(t, mt.message, `expected no elements matching "h1.title", but found 1`)
	})

	t.Run("fails with count when multiple matches", func(t *testing.T) {
		t.Parallel()
		doc := newDocument(t, testHTML)

		mt := expectFailure(t, func(mt *mockT) {
			doc.RequireNotExists(mt, ".item")
		})

		require.True(t, mt.failed, "expected test to fail")
		require.Contains(t, mt.message, `expected no elements matching ".item", but found 2`)
	})
}

func TestDocument_RequireText(t *testing.T) {
	t.Parallel()

	t.Run("succeeds when text contains substring", func(t *testing.T) {
		t.Parallel()
		doc := newDocument(t, testHTML)
		doc.RequireText(t, "h1.title", "Hello")
		doc.RequireText(t, "h1.title", "World")
		doc.RequireText(t, "h1.title", "Hello World")
	})

	t.Run("checks first element when multiple match", func(t *testing.T) {
		t.Parallel()
		doc := newDocument(t, testHTML)
		doc.RequireText(t, ".item", "One")
	})

	t.Run("fails when element does not exist", func(t *testing.T) {
		t.Parallel()
		doc := newDocument(t, testHTML)

		mt := expectFailure(t, func(mt *mockT) {
			doc.RequireText(mt, ".nonexistent", "test")
		})

		require.True(t, mt.failed, "expected test to fail")
		require.Contains(t, mt.message, `expected element matching ".nonexistent" to exist, but none found`)
	})

	t.Run("fails when text does not contain substring", func(t *testing.T) {
		t.Parallel()
		doc := newDocument(t, testHTML)

		mt := expectFailure(t, func(mt *mockT) {
			doc.RequireText(mt, "h1.title", "Goodbye")
		})

		require.True(t, mt.failed, "expected test to fail")
		require.Contains(t, mt.message, `expected text of "h1.title" to contain "Goodbye"`)
		require.Contains(t, mt.message, `got "Hello World"`)
	})
}

func TestDocument_RequireExactText(t *testing.T) {
	t.Parallel()

	t.Run("succeeds when text matches exactly", func(t *testing.T) {
		t.Parallel()
		doc := newDocument(t, testHTML)
		doc.RequireExactText(t, "h1.title", "Hello World")
	})

	t.Run("trims whitespace from element text", func(t *testing.T) {
		t.Parallel()
		html := `<div class="spaced">
			Trimmed Text
		</div>`
		doc := newDocument(t, html)
		doc.RequireExactText(t, ".spaced", "Trimmed Text")
	})

	t.Run("checks first element when multiple match", func(t *testing.T) {
		t.Parallel()
		doc := newDocument(t, testHTML)
		doc.RequireExactText(t, ".item", "One")
	})

	t.Run("fails when element does not exist", func(t *testing.T) {
		t.Parallel()
		doc := newDocument(t, testHTML)

		mt := expectFailure(t, func(mt *mockT) {
			doc.RequireExactText(mt, ".nonexistent", "test")
		})

		require.True(t, mt.failed, "expected test to fail")
		require.Contains(t, mt.message, `expected element matching ".nonexistent" to exist, but none found`)
	})

	t.Run("fails when text does not match exactly", func(t *testing.T) {
		t.Parallel()
		doc := newDocument(t, testHTML)

		mt := expectFailure(t, func(mt *mockT) {
			doc.RequireExactText(mt, "h1.title", "Hello")
		})

		require.True(t, mt.failed, "expected test to fail")
		require.Contains(t, mt.message, `expected text of "h1.title" to equal "Hello"`)
		require.Contains(t, mt.message, `got "Hello World"`)
	})
}

func TestDocument_RequireValue(t *testing.T) {
	t.Parallel()

	t.Run("succeeds when value attribute matches", func(t *testing.T) {
		t.Parallel()
		doc := newDocument(t, testHTML)
		doc.RequireValue(t, "input[name=email]", "test@example.com")
	})

	t.Run("fails when element does not exist", func(t *testing.T) {
		t.Parallel()
		doc := newDocument(t, testHTML)

		mt := expectFailure(t, func(mt *mockT) {
			doc.RequireValue(mt, "input[name=nonexistent]", "value")
		})

		require.True(t, mt.failed, "expected test to fail")
		require.Contains(t, mt.message, `expected element matching "input[name=nonexistent]" to exist, but none found`)
	})

	t.Run("fails when value attribute does not match", func(t *testing.T) {
		t.Parallel()
		doc := newDocument(t, testHTML)

		mt := expectFailure(t, func(mt *mockT) {
			doc.RequireValue(mt, "input[name=email]", "wrong@example.com")
		})

		require.True(t, mt.failed, "expected test to fail")
		require.Contains(t, mt.message, `expected attribute "value" of "input[name=email]" to equal "wrong@example.com"`)
		require.Contains(t, mt.message, `got "test@example.com"`)
	})

	t.Run("fails when value attribute does not exist", func(t *testing.T) {
		t.Parallel()
		doc := newDocument(t, testHTML)

		mt := expectFailure(t, func(mt *mockT) {
			doc.RequireValue(mt, "h1.title", "something")
		})

		require.True(t, mt.failed, "expected test to fail")
		require.Contains(t, mt.message, `expected element "h1.title" to have attribute "value", but it does not`)
	})
}

func TestDocument_RequireAttr(t *testing.T) {
	t.Parallel()

	t.Run("succeeds when attribute matches", func(t *testing.T) {
		t.Parallel()
		doc := newDocument(t, testHTML)
		doc.RequireAttr(t, "a", "href", "/home")
		doc.RequireAttr(t, "a", "data-action", "navigate")
		doc.RequireAttr(t, "input", "type", "text")
		doc.RequireAttr(t, "input", "name", "email")
	})

	t.Run("fails when element does not exist", func(t *testing.T) {
		t.Parallel()
		doc := newDocument(t, testHTML)

		mt := expectFailure(t, func(mt *mockT) {
			doc.RequireAttr(mt, ".nonexistent", "href", "/home")
		})

		require.True(t, mt.failed, "expected test to fail")
		require.Contains(t, mt.message, `expected element matching ".nonexistent" to exist, but none found`)
	})

	t.Run("fails when attribute does not exist", func(t *testing.T) {
		t.Parallel()
		doc := newDocument(t, testHTML)

		mt := expectFailure(t, func(mt *mockT) {
			doc.RequireAttr(mt, "h1.title", "href", "/home")
		})

		require.True(t, mt.failed, "expected test to fail")
		require.Contains(t, mt.message, `expected element "h1.title" to have attribute "href", but it does not`)
	})

	t.Run("fails when attribute value does not match", func(t *testing.T) {
		t.Parallel()
		doc := newDocument(t, testHTML)

		mt := expectFailure(t, func(mt *mockT) {
			doc.RequireAttr(mt, "a", "href", "/wrong")
		})

		require.True(t, mt.failed, "expected test to fail")
		require.Contains(t, mt.message, `expected attribute "href" of "a" to equal "/wrong"`)
		require.Contains(t, mt.message, `got "/home"`)
	})
}

func TestDocument_RequireCount(t *testing.T) {
	t.Parallel()

	t.Run("succeeds when count matches", func(t *testing.T) {
		t.Parallel()
		doc := newDocument(t, testHTML)
		doc.RequireCount(t, ".item", 2)
		doc.RequireCount(t, "h1.title", 1)
		doc.RequireCount(t, ".nonexistent", 0)
	})

	t.Run("fails when count does not match", func(t *testing.T) {
		t.Parallel()
		doc := newDocument(t, testHTML)

		mt := expectFailure(t, func(mt *mockT) {
			doc.RequireCount(mt, ".item", 3)
		})

		require.True(t, mt.failed, "expected test to fail")
		require.Contains(t, mt.message, `expected 3 elements matching ".item", got 2`)
	})

	t.Run("fails when no elements found but count expected", func(t *testing.T) {
		t.Parallel()
		doc := newDocument(t, testHTML)

		mt := expectFailure(t, func(mt *mockT) {
			doc.RequireCount(mt, ".nonexistent", 1)
		})

		require.True(t, mt.failed, "expected test to fail")
		require.Contains(t, mt.message, `expected 1 elements matching ".nonexistent", got 0`)
	})
}

func TestDocument_Find(t *testing.T) {
	t.Parallel()

	t.Run("returns goquery selection for custom assertions", func(t *testing.T) {
		t.Parallel()
		doc := newDocument(t, testHTML)

		sel := doc.Find(".item")
		require.NotNil(t, sel, "expected selection to be returned")
		require.Equal(t, 2, sel.Length(), "expected 2 items")

		// Can perform custom assertions
		firstText := sel.First().Text()
		require.Equal(t, "One", firstText)

		lastText := sel.Last().Text()
		require.Equal(t, "Two", lastText)
	})

	t.Run("returns empty selection when no match", func(t *testing.T) {
		t.Parallel()
		doc := newDocument(t, testHTML)

		sel := doc.Find(".nonexistent")
		require.NotNil(t, sel, "expected selection to be returned")
		require.Equal(t, 0, sel.Length(), "expected empty selection")
	})

	t.Run("can chain goquery methods", func(t *testing.T) {
		t.Parallel()
		doc := newDocument(t, testHTML)

		// Find input and get attribute
		sel := doc.Find("input[name=email]")
		val, exists := sel.Attr("value")
		require.True(t, exists, "expected value attribute to exist")
		require.Equal(t, "test@example.com", val)
	})
}

func TestNewDocument(t *testing.T) {
	t.Parallel()

	t.Run("parses valid HTML", func(t *testing.T) {
		t.Parallel()
		doc := newDocument(t, testHTML)
		require.NotNil(t, doc)
		require.NotNil(t, doc.doc)
	})

	t.Run("parses minimal HTML", func(t *testing.T) {
		t.Parallel()
		doc := newDocument(t, "<div>test</div>")
		require.NotNil(t, doc)
		doc.RequireText(t, "div", "test")
	})

	t.Run("parses empty HTML", func(t *testing.T) {
		t.Parallel()
		doc := newDocument(t, "")
		require.NotNil(t, doc)
	})
}

// mockT wraps testing.T to capture failures
type mockT struct {
	*testing.T
	failed  bool
	message string
}

func newMockT() *mockT {
	return &mockT{
		T: &testing.T{},
	}
}

func (m *mockT) Fatalf(format string, args ...any) {
	m.failed = true
	m.message = fmt.Sprintf(format, args...)
	panic("test failed")
}

func (m *mockT) Fatal(args ...any) {
	m.failed = true
	if len(args) > 0 {
		m.message = fmt.Sprint(args...)
	}
	panic("test failed")
}

// expectFailure runs fn and expects it to call t.Fatal/Fatalf
func expectFailure(t *testing.T, fn func(*mockT)) *mockT {
	t.Helper()
	mt := newMockT()
	func() {
		defer func() { _ = recover() }()
		fn(mt)
	}()
	return mt
}
