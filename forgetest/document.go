package forgetest

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// Document wraps a goquery.Document for HTML assertion helpers.
type Document struct {
	doc *goquery.Document
}

// newDocument parses HTML and returns a Document.
func newDocument(t testing.TB, html string) *Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("forgetest: parse HTML: %v", err)
	}
	return &Document{doc: doc}
}

// RequireExists asserts that at least one element matches the CSS selector.
func (d *Document) RequireExists(t testing.TB, selector string) {
	t.Helper()
	if d.doc.Find(selector).Length() == 0 {
		t.Fatalf("expected element matching %q to exist, but none found", selector)
	}
}

// RequireNotExists asserts that no elements match the CSS selector.
func (d *Document) RequireNotExists(t testing.TB, selector string) {
	t.Helper()
	n := d.doc.Find(selector).Length()
	if n > 0 {
		t.Fatalf("expected no elements matching %q, but found %d", selector, n)
	}
}

// RequireText asserts that the first element matching the selector contains substr in its text.
func (d *Document) RequireText(t testing.TB, selector, substr string) {
	t.Helper()
	sel := d.doc.Find(selector)
	if sel.Length() == 0 {
		t.Fatalf("expected element matching %q to exist, but none found", selector)
	}
	text := sel.First().Text()
	if !strings.Contains(text, substr) {
		t.Fatalf("expected text of %q to contain %q, got %q", selector, substr, text)
	}
}

// RequireExactText asserts that the first element matching the selector has
// exactly the given text (trimmed of leading/trailing whitespace).
func (d *Document) RequireExactText(t testing.TB, selector, text string) {
	t.Helper()
	sel := d.doc.Find(selector)
	if sel.Length() == 0 {
		t.Fatalf("expected element matching %q to exist, but none found", selector)
	}
	got := strings.TrimSpace(sel.First().Text())
	if got != text {
		t.Fatalf("expected text of %q to equal %q, got %q", selector, text, got)
	}
}

// RequireValue asserts that the first element matching the selector has
// the given value attribute.
func (d *Document) RequireValue(t testing.TB, selector, val string) {
	t.Helper()
	d.RequireAttr(t, selector, "value", val)
}

// RequireAttr asserts that the first element matching the selector has
// an attribute with the given value.
func (d *Document) RequireAttr(t testing.TB, selector, attr, val string) {
	t.Helper()
	sel := d.doc.Find(selector)
	if sel.Length() == 0 {
		t.Fatalf("expected element matching %q to exist, but none found", selector)
	}
	got, exists := sel.First().Attr(attr)
	if !exists {
		t.Fatalf("expected element %q to have attribute %q, but it does not", selector, attr)
	}
	if got != val {
		t.Fatalf("expected attribute %q of %q to equal %q, got %q", attr, selector, val, got)
	}
}

// RequireCount asserts that exactly n elements match the CSS selector.
func (d *Document) RequireCount(t testing.TB, selector string, n int) {
	t.Helper()
	got := d.doc.Find(selector).Length()
	if got != n {
		t.Fatalf("expected %d elements matching %q, got %d", n, selector, got)
	}
}

// Find returns the goquery.Selection for the given CSS selector.
// Use this for custom assertions beyond what the helper methods provide.
func (d *Document) Find(selector string) *goquery.Selection {
	return d.doc.Find(selector)
}
