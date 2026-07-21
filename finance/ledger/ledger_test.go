package ledger_test

import (
	"context"
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/core/money"
	"github.com/dmitrymomot/forge/finance/ledger"
)

// Validation and scope failures return before any SQL executes: writes take a
// nil pgx.Tx and reads a panicking stub, so any touch of the database fails
// the test loudly.

type noDB struct{}

func (noDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("ledger touched the database")
}

func (noDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("ledger touched the database")
}

func (noDB) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("ledger touched the database")
}

var _ ledger.DB = noDB{}

func TestPost_Validation(t *testing.T) {
	t.Parallel()
	l := ledger.New()
	ctx := context.Background()
	src, dst := id.NewUUID(), id.NewUUID()
	amt := money.FromMinor(100, money.EUR)

	t.Run("empty ref", func(t *testing.T) {
		t.Parallel()
		_, err := l.Post(ctx, nil, ledger.Posting{Src: src, Dst: dst, Amount: amt})
		require.ErrorIs(t, err, ledger.ErrInvalidRef)
	})
	t.Run("zero amount", func(t *testing.T) {
		t.Parallel()
		_, err := l.Post(ctx, nil, ledger.Posting{Ref: "r", Src: src, Dst: dst})
		require.ErrorIs(t, err, ledger.ErrInvalidAmount)
	})
	t.Run("negative amount", func(t *testing.T) {
		t.Parallel()
		_, err := l.Post(ctx, nil, ledger.Posting{Ref: "r", Src: src, Dst: dst, Amount: amt.Neg()})
		require.ErrorIs(t, err, ledger.ErrInvalidAmount)
	})
	t.Run("same account", func(t *testing.T) {
		t.Parallel()
		_, err := l.Post(ctx, nil, ledger.Posting{Ref: "r", Src: src, Dst: src, Amount: amt})
		require.ErrorIs(t, err, ledger.ErrSameAccount)
	})
}

func TestHold_Validation(t *testing.T) {
	t.Parallel()
	l := ledger.New()
	ctx := context.Background()

	t.Run("empty ref", func(t *testing.T) {
		t.Parallel()
		_, err := l.Hold(ctx, nil, ledger.Hold{Account: id.NewUUID(), Amount: money.FromMinor(1, money.EUR)})
		require.ErrorIs(t, err, ledger.ErrInvalidRef)
	})
	t.Run("zero amount", func(t *testing.T) {
		t.Parallel()
		_, err := l.Hold(ctx, nil, ledger.Hold{Ref: "r", Account: id.NewUUID()})
		require.ErrorIs(t, err, ledger.ErrInvalidAmount)
	})
}

func TestSettleVoid_Validation(t *testing.T) {
	t.Parallel()
	l := ledger.New()
	ctx := context.Background()

	t.Run("settle empty ref", func(t *testing.T) {
		t.Parallel()
		_, err := l.Settle(ctx, nil, "", id.NewUUID())
		require.ErrorIs(t, err, ledger.ErrInvalidRef)
	})
	t.Run("settle non-positive amount", func(t *testing.T) {
		t.Parallel()
		_, err := l.Settle(ctx, nil, "r", id.NewUUID(), ledger.SettleAmount(money.FromMinor(0, money.EUR)))
		require.ErrorIs(t, err, ledger.ErrInvalidAmount)
	})
	t.Run("void empty ref", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, l.Void(ctx, nil, ""), ledger.ErrInvalidRef)
	})
}

func TestEnsureAccount_Validation(t *testing.T) {
	t.Parallel()
	l := ledger.New()
	ctx := context.Background()

	t.Run("empty fields", func(t *testing.T) {
		t.Parallel()
		for _, key := range []ledger.AccountKey{
			{Purpose: "wallet", Currency: money.EUR},
			{Owner: "alice", Currency: money.EUR},
			{Owner: "alice", Purpose: "wallet"},
		} {
			_, err := l.EnsureAccount(ctx, nil, key)
			require.ErrorIs(t, err, ledger.ErrInvalidKey)
		}
	})
	t.Run("floor currency mismatch", func(t *testing.T) {
		t.Parallel()
		key := ledger.AccountKey{Owner: "alice", Purpose: "wallet", Currency: money.EUR}
		_, err := l.EnsureAccount(ctx, nil, key, ledger.WithFloor(money.FromMinor(0, money.USD)))
		require.ErrorIs(t, err, ledger.ErrCurrencyMismatch)
	})
	t.Run("floor and floor-free are exclusive", func(t *testing.T) {
		t.Parallel()
		key := ledger.AccountKey{Owner: "alice", Purpose: "wallet", Currency: money.EUR}
		_, err := l.EnsureAccount(ctx, nil, key,
			ledger.WithFloor(money.FromMinor(0, money.EUR)), ledger.WithoutFloor())
		require.ErrorIs(t, err, ledger.ErrInvalidKey)
	})
}

func TestReadValidation(t *testing.T) {
	t.Parallel()
	l := ledger.New()
	ctx := context.Background()

	_, err := l.PostingByRef(ctx, noDB{}, "")
	require.ErrorIs(t, err, ledger.ErrInvalidRef)
	_, err = l.PostingsByGroup(ctx, noDB{}, "")
	require.ErrorIs(t, err, ledger.ErrInvalidRef)
	_, err = l.HoldByRef(ctx, noDB{}, "")
	require.ErrorIs(t, err, ledger.ErrInvalidRef)
}

func TestScope_FailClosed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	hookErr := errors.New("no tenant in context")

	cases := map[string]*ledger.Ledger{
		"hook error": ledger.New(ledger.WithScope(func(context.Context) (string, error) {
			return "", hookErr
		})),
		"empty scope": ledger.New(ledger.WithScope(func(context.Context) (string, error) {
			return "", nil
		})),
	}
	for name, l := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			key := ledger.AccountKey{Owner: "alice", Purpose: "wallet", Currency: money.EUR}
			amt := money.FromMinor(100, money.EUR)

			_, err := l.EnsureAccount(ctx, nil, key)
			require.ErrorIs(t, err, ledger.ErrScopeMissing)
			_, err = l.Post(ctx, nil, ledger.Posting{Ref: "r", Src: id.NewUUID(), Dst: id.NewUUID(), Amount: amt})
			require.ErrorIs(t, err, ledger.ErrScopeMissing)
			_, err = l.Hold(ctx, nil, ledger.Hold{Ref: "r", Account: id.NewUUID(), Amount: amt})
			require.ErrorIs(t, err, ledger.ErrScopeMissing)
			_, err = l.Settle(ctx, nil, "r", id.NewUUID())
			require.ErrorIs(t, err, ledger.ErrScopeMissing)
			require.ErrorIs(t, l.Void(ctx, nil, "r"), ledger.ErrScopeMissing)
			_, err = l.Balance(ctx, noDB{}, id.NewUUID())
			require.ErrorIs(t, err, ledger.ErrScopeMissing)
			require.ErrorIs(t, l.Snapshot(ctx, noDB{}, id.NewUUID()), ledger.ErrScopeMissing)
			_, err = l.CheckDrift(ctx, noDB{}, id.NewUUID())
			require.ErrorIs(t, err, ledger.ErrScopeMissing)
			_, err = l.Accounts(ctx, noDB{}, id.UUID{}, 10)
			require.ErrorIs(t, err, ledger.ErrScopeMissing)
			_, err = l.ExpiredHolds(ctx, noDB{}, 10)
			require.ErrorIs(t, err, ledger.ErrScopeMissing)
		})
	}
}

func TestScope_HookErrorWrapped(t *testing.T) {
	t.Parallel()
	hookErr := errors.New("boom")
	l := ledger.New(ledger.WithScope(func(context.Context) (string, error) { return "", hookErr }))
	_, err := l.Balance(context.Background(), noDB{}, id.NewUUID())
	require.ErrorIs(t, err, ledger.ErrScopeMissing)
	require.ErrorIs(t, err, hookErr)
}

func TestMigrationsEmbedded(t *testing.T) {
	t.Parallel()
	entries, err := fs.ReadDir(ledger.Migrations, ".")
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	assert.Contains(t, entries[0].Name(), "ledger")
}

func TestHoldStatusValues(t *testing.T) {
	t.Parallel()
	assert.Equal(t, ledger.HoldStatus("open"), ledger.HoldOpen)
	assert.Equal(t, ledger.HoldStatus("settled"), ledger.HoldSettled)
	assert.Equal(t, ledger.HoldStatus("voided"), ledger.HoldVoided)
}

// Compile-time-ish check that zero ExpiresAt round-trips as "never expires".
func TestHoldZeroExpiry(t *testing.T) {
	t.Parallel()
	var h ledger.Hold
	assert.True(t, h.ExpiresAt.IsZero())
	assert.Equal(t, time.Time{}, h.ResolvedAt)
}
