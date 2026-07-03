package sanitize_test

import (
	"testing"

	"github.com/dmitrymomot/forge/core/sanitize"
)

func BenchmarkApply(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = sanitize.Apply("  Hello   World  ", sanitize.Trim, sanitize.Collapse)
	}
}

func BenchmarkCompose(b *testing.B) {
	pipeline := sanitize.Compose(sanitize.Trim, sanitize.Collapse, sanitize.KeepAlphanumeric)
	b.ReportAllocs()
	for b.Loop() {
		_ = pipeline("  Hello   World 123  ")
	}
}

func BenchmarkCollapse(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = sanitize.Collapse("  the   quick \t brown \n fox  ")
	}
}

func BenchmarkSingleLine(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = sanitize.SingleLine("line one\r\nline two\r\nline three")
	}
}

func BenchmarkKeepAlphanumeric(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = sanitize.KeepAlphanumeric("Hello, World! 123 #$%")
	}
}

func BenchmarkEscapeHTML(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = sanitize.EscapeHTML(`<a href="x">Tom & "Jerry"</a>`)
	}
}

func BenchmarkStripTags(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = sanitize.StripTags(`<p>Hello <b>bold</b> and <i>italic</i> text</p>`)
	}
}

func BenchmarkEmail(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = sanitize.Email("  Ann.Lee@Example.COM  ")
	}
}

func BenchmarkUsername(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = sanitize.Username("  __John_Doe.v2!!__  ")
	}
}

func BenchmarkFilename(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = sanitize.Filename("  ../../etc/passwd  ")
	}
}

func BenchmarkSanitizeURL(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = sanitize.SanitizeURL("  https://example.com/path?q=1  ")
	}
}
