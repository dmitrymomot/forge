package errorsx_test

import (
	"errors"
	"testing"

	"github.com/dmitrymomot/forge/errorsx"
)

func BenchmarkNew(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = errorsx.New("E_CODE", "something went wrong")
	}
}

func BenchmarkErrorf(b *testing.B) {
	cause := errors.New("boom")
	b.ReportAllocs()
	for b.Loop() {
		_ = errorsx.Errorf("E_CODE", "wrapping: %w", cause)
	}
}

func BenchmarkWithCode(b *testing.B) {
	cause := errors.New("boom")
	b.ReportAllocs()
	for b.Loop() {
		_ = errorsx.WithCode(cause, "E_CODE")
	}
}

func BenchmarkCode(b *testing.B) {
	// Nested chain: an outer code-less wrapper over a coded leaf so Code must
	// walk past the shallow node to find the real code.
	leaf := errorsx.New("E_DEEP", "leaf")
	chain := errorsx.WithCode(leaf, "")
	b.ReportAllocs()
	for b.Loop() {
		_, _ = errorsx.Code(chain)
	}
}

func BenchmarkMarkPermanent(b *testing.B) {
	cause := errors.New("boom")
	b.ReportAllocs()
	for b.Loop() {
		_ = errorsx.MarkPermanent(cause)
	}
}

func BenchmarkIsPermanent(b *testing.B) {
	err := errorsx.MarkPermanent(errorsx.New("E_CODE", "boom"))
	b.ReportAllocs()
	for b.Loop() {
		_ = errorsx.IsPermanent(err)
	}
}

func BenchmarkIsRetryable(b *testing.B) {
	err := errorsx.New("E_CODE", "boom")
	b.ReportAllocs()
	for b.Loop() {
		_ = errorsx.IsRetryable(err)
	}
}
