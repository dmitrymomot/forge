package auditlog_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/ops/auditlog"
)

func TestRecord_StampsIDAndTime(t *testing.T) {
	t.Parallel()
	sink := auditlog.NewMemorySink()
	now := time.Date(2026, 7, 21, 12, 0, 0, 123456789, time.UTC)
	rec := auditlog.New(sink, auditlog.WithClock(clock.NewMock(now)))

	e, err := rec.Record(context.Background(), auditlog.Event{
		Actor:   "user_1",
		Action:  "user.login",
		Outcome: auditlog.OutcomeSuccess,
	})
	require.NoError(t, err)
	assert.False(t, e.ID.IsZero())
	assert.Equal(t, now.Truncate(time.Microsecond), e.Time, "time is stamped and truncated to microseconds")
	assert.Empty(t, e.Hash, "no chain hash without WithChain")

	events := sink.Events()
	require.Len(t, events, 1)
	assert.Equal(t, e, events[0], "sink receives the finalized event")
}

func TestRecord_KeepsCallerTime(t *testing.T) {
	t.Parallel()
	sink := auditlog.NewMemorySink()
	rec := auditlog.New(sink)
	at := time.Date(2025, 1, 2, 3, 4, 5, 678901234, time.FixedZone("X", 3600))

	e, err := rec.Record(context.Background(), auditlog.Event{
		Action:  "import.backfill",
		Outcome: auditlog.OutcomeSuccess,
		Time:    at,
	})
	require.NoError(t, err)
	assert.Equal(t, at.UTC().Truncate(time.Microsecond), e.Time, "caller time normalized to UTC microseconds")
}

func TestRecord_InvalidEvent(t *testing.T) {
	t.Parallel()
	rec := auditlog.New(auditlog.NewMemorySink())

	_, err := rec.Record(context.Background(), auditlog.Event{Outcome: auditlog.OutcomeSuccess})
	assert.ErrorIs(t, err, auditlog.ErrInvalidEvent, "missing action")

	_, err = rec.Record(context.Background(), auditlog.Event{Action: "user.login"})
	assert.ErrorIs(t, err, auditlog.ErrInvalidEvent, "missing outcome")
}

func TestRecord_RejectsNULBytes(t *testing.T) {
	t.Parallel()
	sink := auditlog.NewMemorySink()
	rec := auditlog.New(sink)

	events := []auditlog.Event{
		{Action: "a\x00b", Outcome: "ok"},
		{Action: "a", Outcome: "ok", Actor: "u\x00"},
		{Action: "a", Outcome: "ok", Tenant: "t\x00"},
		{Action: "a", Outcome: "ok", Resource: "r\x00"},
		{Action: "a", Outcome: auditlog.Outcome("o\x00k")},
		{Action: "a", Outcome: "ok", Meta: map[string]string{"k\x00": "v"}},
		{Action: "a", Outcome: "ok", Meta: map[string]string{"k": "v\x00"}},
	}
	for _, e := range events {
		_, err := rec.Record(context.Background(), e)
		assert.ErrorIs(t, err, auditlog.ErrInvalidEvent, "NUL bytes cannot be stored by Postgres and must fail at the audit point")
	}
	assert.Empty(t, sink.Events())
}

func TestRecord_IDsAreTimeOrdered(t *testing.T) {
	t.Parallel()
	sink := auditlog.NewMemorySink()
	rec := auditlog.New(sink)
	for range 100 {
		_, err := rec.Record(context.Background(), auditlog.Event{Action: "a", Outcome: "ok"})
		require.NoError(t, err)
	}
	events := sink.Events()
	for i := 1; i < len(events); i++ {
		assert.Equal(t, -1, compareIDs(events[i-1].ID[:], events[i].ID[:]), "monotonic ids")
	}
}

func compareIDs(a, b []byte) int {
	for i := range a {
		switch {
		case a[i] < b[i]:
			return -1
		case a[i] > b[i]:
			return 1
		}
	}
	return 0
}

func TestRecord_ScopeStampsTenant(t *testing.T) {
	t.Parallel()
	sink := auditlog.NewMemorySink()
	rec := auditlog.New(sink, auditlog.WithScope(func(context.Context) (string, error) {
		return "org_7", nil
	}))

	e, err := rec.Record(context.Background(), auditlog.Event{Action: "a", Outcome: "ok"})
	require.NoError(t, err)
	assert.Equal(t, "org_7", e.Tenant)

	// Matching explicit tenant is accepted.
	e, err = rec.Record(context.Background(), auditlog.Event{Action: "a", Outcome: "ok", Tenant: "org_7"})
	require.NoError(t, err)
	assert.Equal(t, "org_7", e.Tenant)

	// Conflicting explicit tenant fails.
	_, err = rec.Record(context.Background(), auditlog.Event{Action: "a", Outcome: "ok", Tenant: "org_9"})
	assert.ErrorIs(t, err, auditlog.ErrTenantMismatch)
}

func TestRecord_ScopeFailsClosed(t *testing.T) {
	t.Parallel()
	sink := auditlog.NewMemorySink()

	rec := auditlog.New(sink, auditlog.WithScope(func(context.Context) (string, error) {
		return "", nil
	}))
	_, err := rec.Record(context.Background(), auditlog.Event{Action: "a", Outcome: "ok"})
	assert.ErrorIs(t, err, auditlog.ErrScope, "empty tenant fails closed")

	hookErr := errors.New("boom")
	rec = auditlog.New(sink, auditlog.WithScope(func(context.Context) (string, error) {
		return "", hookErr
	}))
	_, err = rec.Record(context.Background(), auditlog.Event{Action: "a", Outcome: "ok"})
	assert.ErrorIs(t, err, auditlog.ErrScope)
	assert.ErrorIs(t, err, hookErr, "hook error preserved for errors.Is")

	assert.Empty(t, sink.Events(), "nothing reaches the sink on scope failure")
}

func TestRecord_UnscopedKeepsExplicitTenant(t *testing.T) {
	t.Parallel()
	rec := auditlog.New(auditlog.NewMemorySink())
	e, err := rec.Record(context.Background(), auditlog.Event{Action: "a", Outcome: "ok", Tenant: "org_1"})
	require.NoError(t, err)
	assert.Equal(t, "org_1", e.Tenant)
}

// failSink wraps a MemorySink and fails every write until healed.
type failSink struct {
	inner *auditlog.MemorySink
	mu    sync.Mutex
	fail  bool
}

func newFailSink() *failSink { return &failSink{inner: auditlog.NewMemorySink()} }

func (s *failSink) Write(ctx context.Context, e auditlog.Event) error {
	s.mu.Lock()
	fail := s.fail
	s.mu.Unlock()
	if fail {
		return errors.New("sink down")
	}
	return s.inner.Write(ctx, e)
}

func (s *failSink) ChainHead(ctx context.Context, stream string) (string, error) {
	return s.inner.ChainHead(ctx, stream)
}

func (s *failSink) setFail(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail = v
}

func TestRecord_SinkErrorPropagates(t *testing.T) {
	t.Parallel()
	sink := newFailSink()
	sink.setFail(true)
	rec := auditlog.New(sink)
	_, err := rec.Record(context.Background(), auditlog.Event{Action: "a", Outcome: "ok"})
	assert.ErrorContains(t, err, "sink down")
}

func TestNew_NilSinkPanics(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() { auditlog.New(nil) })
}
