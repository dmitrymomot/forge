package apikey_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/apikey"
	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/crypto/redact"
)

// mustConfig builds a validated Config or fails the test.
func mustConfig(tb testing.TB, opts ...apikey.Option) apikey.Config {
	tb.Helper()
	cfg, err := apikey.NewConfig(opts...)
	require.NoError(tb, err)
	return cfg
}

// expose unwraps a Create or Rotate result so a test body can use the
// plaintext as the credential it is. Production callers reach the value
// the same way, through Expose, at their single display site.
func expose(k apikey.Key, s redact.Secret[string], err error) (apikey.Key, string, error) {
	return k, s.Expose(), err
}

// discardKey is a SaveFunc that accepts every record.
func discardKey(context.Context, apikey.Key) error { return nil }

// captureKey returns a SaveFunc that records what the operation minted.
func captureKey(dst *apikey.Key) apikey.SaveFunc {
	return func(_ context.Context, k apikey.Key) error {
		*dst = k
		return nil
	}
}

// listsNothing is a ListFunc that reports an empty result set.
func listsNothing(context.Context, apikey.Filter) ([]apikey.Key, error) {
	return []apikey.Key{}, nil
}

// discardStamp is a RevokeFunc that accepts every stamp.
func discardStamp(context.Context, id.UUID, time.Time) error { return nil }

// loadsKey returns a LoadFunc that answers every id with k.
func loadsKey(k apikey.Key) apikey.LoadFunc {
	return func(context.Context, id.UUID) (apikey.Key, error) { return k, nil }
}

// loadsKeyByHash returns a LoadByHashFunc that answers k's own hash with k
// and every other hash with ErrNotFound.
func loadsKeyByHash(k apikey.Key) apikey.LoadByHashFunc {
	return func(_ context.Context, hash string) (apikey.Key, error) {
		if hash != k.Hash {
			return apikey.Key{}, apikey.ErrNotFound
		}
		return k, nil
	}
}

// issueKey mints one real key so tests that need a valid credential do not
// hand-build a checksum.
func issueKey(tb testing.TB, cfg apikey.Config, p apikey.CreateParams) (apikey.Key, string) {
	tb.Helper()
	k, plaintext, err := expose(apikey.Create(context.Background(), cfg, p, discardKey))
	require.NoError(tb, err)
	return k, plaintext
}
