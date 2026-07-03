package postgres_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/data/postgres"
)

func pgErr(code string) error {
	// Wrap to prove the predicates unwrap with errors.As, not a bare type assert.
	return fmt.Errorf("query failed: %w", &pgconn.PgError{Code: code})
}

func TestIsUniqueViolation(t *testing.T) {
	assert.True(t, postgres.IsUniqueViolation(pgErr("23505")))
	assert.False(t, postgres.IsUniqueViolation(pgErr("23503")))
	assert.False(t, postgres.IsUniqueViolation(errors.New("plain")))
	assert.False(t, postgres.IsUniqueViolation(nil))
}

func TestIsForeignKeyViolation(t *testing.T) {
	assert.True(t, postgres.IsForeignKeyViolation(pgErr("23503")))
	assert.False(t, postgres.IsForeignKeyViolation(pgErr("23505")))
	assert.False(t, postgres.IsForeignKeyViolation(nil))
}

func TestIsNotFound(t *testing.T) {
	assert.True(t, postgres.IsNotFound(pgx.ErrNoRows))
	assert.True(t, postgres.IsNotFound(fmt.Errorf("wrapped: %w", pgx.ErrNoRows)))
	assert.False(t, postgres.IsNotFound(pgErr("23505")))
	assert.False(t, postgres.IsNotFound(nil))
}

func TestIsSerializationFailure(t *testing.T) {
	assert.True(t, postgres.IsSerializationFailure(pgErr("40001")))
	assert.True(t, postgres.IsSerializationFailure(pgErr("40P01")))
	assert.False(t, postgres.IsSerializationFailure(pgErr("23505")))
	assert.False(t, postgres.IsSerializationFailure(nil))
}
