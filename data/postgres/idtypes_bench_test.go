package postgres_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/data/postgres"
)

// BenchmarkEncodeUUID compares the two routes to the same 16 wire bytes. Unregistered,
// pgx reaches id.UUID only through driver.Valuer, so every bind formats the canonical
// 36-character string and parses it back. Registered, the wrap plan hands pgx the
// underlying [16]byte and the string never exists.
func BenchmarkEncodeUUID(b *testing.B) {
	u := id.NewUUID()
	buf := make([]byte, 0, 16)

	b.Run("unregistered", func(b *testing.B) {
		m := pgtype.NewMap()
		b.ReportAllocs()
		for b.Loop() {
			if _, err := m.Encode(pgtype.UUIDOID, pgtype.BinaryFormatCode, u, buf[:0]); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("registered", func(b *testing.B) {
		m := pgtype.NewMap()
		postgres.RegisterIDTypes(m)
		b.ReportAllocs()
		for b.Loop() {
			if _, err := m.Encode(pgtype.UUIDOID, pgtype.BinaryFormatCode, u, buf[:0]); err != nil {
				b.Fatal(err)
			}
		}
	})
}
