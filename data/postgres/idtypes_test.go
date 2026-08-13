package postgres_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/data/postgres"
)

func registeredMap() *pgtype.Map {
	m := pgtype.NewMap()
	postgres.RegisterIDTypes(m)
	return m
}

func TestRegisterIDTypes_EncodesUUIDAsSixteenBytes(t *testing.T) {
	u := id.NewUUID()

	buf, err := registeredMap().Encode(pgtype.UUIDOID, pgtype.BinaryFormatCode, u, nil)
	require.NoError(t, err)
	assert.Equal(t, u[:], buf)
}

// TestRegisterIDTypes_MatchesUnregisteredBytes pins the registration as a pure fast
// path: the wire bytes are identical either way, only the route to them changes (see
// BenchmarkEncodeUUID). A drift here means the wrap plan altered the encoding.
func TestRegisterIDTypes_MatchesUnregisteredBytes(t *testing.T) {
	u := id.NewUUID()

	slow, err := pgtype.NewMap().Encode(pgtype.UUIDOID, pgtype.BinaryFormatCode, u, nil)
	require.NoError(t, err)
	fast, err := registeredMap().Encode(pgtype.UUIDOID, pgtype.BinaryFormatCode, u, nil)
	require.NoError(t, err)

	assert.Equal(t, slow, fast)
}

func TestRegisterIDTypes_EncodesPointerAndNullUUID(t *testing.T) {
	u := id.NewUUID()
	m := registeredMap()

	buf, err := m.Encode(pgtype.UUIDOID, pgtype.BinaryFormatCode, &u, nil)
	require.NoError(t, err)
	assert.Equal(t, u[:], buf)

	buf, err = m.Encode(pgtype.UUIDOID, pgtype.BinaryFormatCode, id.NullOf(u), nil)
	require.NoError(t, err)
	assert.Equal(t, u[:], buf)
}

func TestRegisterIDTypes_InvalidNullUUIDEncodesAsNull(t *testing.T) {
	buf, err := registeredMap().Encode(pgtype.UUIDOID, pgtype.BinaryFormatCode, id.NullUUID{}, nil)
	require.NoError(t, err)
	assert.Nil(t, buf)
}

func TestRegisterIDTypes_ScansBinaryAndText(t *testing.T) {
	u := id.NewUUID()
	m := registeredMap()

	tests := []struct {
		name   string
		format int16
		src    []byte
	}{
		{"binary", pgtype.BinaryFormatCode, u[:]},
		{"text", pgtype.TextFormatCode, []byte(u.String())},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got id.UUID
			require.NoError(t, m.Scan(pgtype.UUIDOID, tt.format, tt.src, &got))
			assert.Equal(t, u, got)
		})
	}
}

func TestRegisterIDTypes_ScansNullIntoNullUUID(t *testing.T) {
	m := registeredMap()

	got := id.NullOf(id.NewUUID())
	require.NoError(t, m.Scan(pgtype.UUIDOID, pgtype.BinaryFormatCode, nil, &got))
	assert.False(t, got.Valid)
}

func TestRegisterIDTypes_RoundTripsUUIDArray(t *testing.T) {
	in := []id.UUID{id.NewUUID(), id.NewUUID(), id.NewUUID()}
	m := registeredMap()

	buf, err := m.Encode(pgtype.UUIDArrayOID, pgtype.BinaryFormatCode, in, nil)
	require.NoError(t, err)

	var got []id.UUID
	require.NoError(t, m.Scan(pgtype.UUIDArrayOID, pgtype.BinaryFormatCode, buf, &got))
	assert.Equal(t, in, got)
}

func TestRegisterIDTypes_IsIdempotent(t *testing.T) {
	u := id.NewUUID()
	m := pgtype.NewMap()
	postgres.RegisterIDTypes(m)
	postgres.RegisterIDTypes(m)

	buf, err := m.Encode(pgtype.UUIDOID, pgtype.BinaryFormatCode, u, nil)
	require.NoError(t, err)
	assert.Equal(t, u[:], buf)
}
