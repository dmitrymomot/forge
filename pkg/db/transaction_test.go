package db

import (
	"context"
	"errors"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

// fakeTx is a minimal pgx.Tx implementation that records Commit/Rollback calls.
// It embeds pgx.Tx so the (large) interface is satisfied; only the two lifecycle
// methods exercised by WithTx are overridden. Any other method call would panic
// with a nil-pointer deref, which keeps the test honest: WithTx must only ever
// call Commit and Rollback on the tx it manages.
type fakeTx struct {
	pgx.Tx
	commitCalls   atomic.Int32
	rollbackCalls atomic.Int32
	commitErr     error
	rollbackErr   error
}

func (f *fakeTx) Commit(context.Context) error {
	f.commitCalls.Add(1)
	return f.commitErr
}

func (f *fakeTx) Rollback(context.Context) error {
	f.rollbackCalls.Add(1)
	return f.rollbackErr
}

// fakeBeginner is a txBeginner that hands out a preconfigured fakeTx, or an
// error if beginErr is set.
type fakeBeginner struct {
	tx         *fakeTx
	beginErr   error
	beginCalls atomic.Int32
}

func (b *fakeBeginner) Begin(context.Context) (pgx.Tx, error) {
	b.beginCalls.Add(1)
	if b.beginErr != nil {
		return nil, b.beginErr
	}
	return b.tx, nil
}

func TestWithTx_CommitsOnSuccess(t *testing.T) {
	t.Parallel()

	tx := &fakeTx{}
	b := &fakeBeginner{tx: tx}

	var fnCalled bool
	err := withTx(context.Background(), b, func(got pgx.Tx) error {
		fnCalled = true
		require.Same(t, tx, got, "fn must receive the tx returned by Begin")
		return nil
	})

	require.NoError(t, err)
	require.True(t, fnCalled, "fn must be invoked")
	require.Equal(t, int32(1), tx.commitCalls.Load(), "successful fn must commit exactly once")
	require.Equal(t, int32(0), tx.rollbackCalls.Load(), "successful fn must not roll back")
}

func TestWithTx_RollsBackOnError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("business failure")
	tx := &fakeTx{}
	b := &fakeBeginner{tx: tx}

	err := withTx(context.Background(), b, func(pgx.Tx) error {
		return wantErr
	})

	require.ErrorIs(t, err, wantErr, "WithTx must return fn's error unchanged")
	require.Equal(t, int32(1), tx.rollbackCalls.Load(), "errored fn must roll back exactly once")
	require.Equal(t, int32(0), tx.commitCalls.Load(), "errored fn must not commit")
}

func TestWithTx_RollsBackAndRepanicsOnPanic(t *testing.T) {
	t.Parallel()

	tx := &fakeTx{}
	b := &fakeBeginner{tx: tx}
	sentinel := "boom"

	require.PanicsWithValue(t, sentinel, func() {
		_ = withTx(context.Background(), b, func(pgx.Tx) error {
			panic(sentinel)
		})
	}, "panic value must propagate unchanged")

	require.Equal(t, int32(1), tx.rollbackCalls.Load(), "panicking fn must roll back exactly once")
	require.Equal(t, int32(0), tx.commitCalls.Load(), "panicking fn must not commit")
}

// Go 1.21+ turns panic(nil) into a *runtime.PanicNilError; recover() returns
// that non-nil value, so WithTx must still roll back and re-panic.
func TestWithTx_RollsBackAndRepanicsOnNilPanic(t *testing.T) {
	t.Parallel()

	tx := &fakeTx{}
	b := &fakeBeginner{tx: tx}

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_ = withTx(context.Background(), b, func(pgx.Tx) error {
			panic(nil) //nolint:govet // intentionally testing the nil-panic path
		})
	}()

	require.IsType(t, &runtime.PanicNilError{}, recovered, "nil panic must re-raise as *runtime.PanicNilError")
	require.Equal(t, int32(1), tx.rollbackCalls.Load(), "nil-panicking fn must roll back exactly once")
	require.Equal(t, int32(0), tx.commitCalls.Load(), "nil-panicking fn must not commit")
}

func TestWithTx_ReturnsBeginError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("cannot begin")
	b := &fakeBeginner{beginErr: wantErr}

	var fnCalled bool
	err := withTx(context.Background(), b, func(pgx.Tx) error {
		fnCalled = true
		return nil
	})

	require.ErrorIs(t, err, wantErr, "Begin failure must be returned unchanged")
	require.False(t, fnCalled, "fn must not run when Begin fails")
	require.Equal(t, int32(1), b.beginCalls.Load())
}

func TestWithTx_ReturnsCommitError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("commit failed")
	tx := &fakeTx{commitErr: wantErr}
	b := &fakeBeginner{tx: tx}

	err := withTx(context.Background(), b, func(pgx.Tx) error {
		return nil
	})

	require.ErrorIs(t, err, wantErr, "commit error must surface to the caller")
	require.Equal(t, int32(1), tx.commitCalls.Load())
	require.Equal(t, int32(0), tx.rollbackCalls.Load())
}
