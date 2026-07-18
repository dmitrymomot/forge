package pagination_test

import (
	"net/url"
	"testing"

	"github.com/dmitrymomot/forge/data/pagination"
)

var (
	benchKeyset = pagination.Keyset{
		{Column: "created_at", Desc: true},
		{Column: "id", Desc: true},
	}
	benchCursor = pagination.Cursor{Keys: []any{"2026-01-01T00:00:00Z", int64(4242)}}
)

func BenchmarkWhereDollar(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if _, err := benchKeyset.Where(benchCursor, pagination.Dollar, 1); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWhereQuestion(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if _, err := benchKeyset.Where(benchCursor, pagination.Question, 1); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOrderBy(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if _, err := benchKeyset.OrderBy(false); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncode(b *testing.B) {
	codec, err := pagination.NewCodec()
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := codec.Encode(benchCursor); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecode(b *testing.B) {
	codec, err := pagination.NewCodec()
	if err != nil {
		b.Fatal(err)
	}
	enc, _ := codec.Encode(benchCursor)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := codec.Decode(enc); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNewWindow(b *testing.B) {
	q := url.Values{"sort": {"name"}, "status": {"active"}}
	cfg := pagination.WindowConfig{Page: 10, PerPage: 25, Total: 5000, Span: 2, Query: q}
	b.ReportAllocs()
	for b.Loop() {
		_ = pagination.NewWindow(cfg)
	}
}
