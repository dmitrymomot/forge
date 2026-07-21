package auditlog_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/auditlog"
)

func recordN(t *testing.T, rec *auditlog.Recorder, n int, e auditlog.Event) {
	t.Helper()
	for range n {
		_, err := rec.Record(context.Background(), e)
		require.NoError(t, err)
	}
}

func TestChain_LinksAndVerifies(t *testing.T) {
	t.Parallel()
	sink := auditlog.NewMemorySink()
	rec := auditlog.New(sink, auditlog.WithChain())
	recordN(t, rec, 5, auditlog.Event{Actor: "u", Action: "doc.edit", Outcome: "ok"})

	events := sink.Events()
	require.Len(t, events, 5)
	assert.Empty(t, events[0].PrevHash, "genesis event has no prev hash")
	for i := 1; i < len(events); i++ {
		assert.Equal(t, events[i-1].Hash, events[i].PrevHash)
	}

	head, err := auditlog.VerifyChain("", events)
	require.NoError(t, err)
	assert.Equal(t, events[4].Hash, head)
}

func TestChain_BatchedVerifyThreadsHead(t *testing.T) {
	t.Parallel()
	sink := auditlog.NewMemorySink()
	rec := auditlog.New(sink, auditlog.WithChain())
	recordN(t, rec, 6, auditlog.Event{Action: "a", Outcome: "ok"})

	events := sink.Events()
	head, err := auditlog.VerifyChain("", events[:3])
	require.NoError(t, err)
	head, err = auditlog.VerifyChain(head, events[3:])
	require.NoError(t, err)
	assert.Equal(t, events[5].Hash, head)
}

func TestChain_DetectsTampering(t *testing.T) {
	t.Parallel()
	sink := auditlog.NewMemorySink()
	rec := auditlog.New(sink, auditlog.WithChain())
	recordN(t, rec, 3, auditlog.Event{Actor: "u", Action: "a", Outcome: "ok"})
	events := sink.Events()

	tests := []struct {
		name   string
		mutate func([]auditlog.Event) []auditlog.Event
	}{
		{"payload rewritten", func(ev []auditlog.Event) []auditlog.Event { ev[1].Actor = "attacker"; return ev }},
		{"meta injected", func(ev []auditlog.Event) []auditlog.Event { ev[1].Meta = map[string]string{"x": "y"}; return ev }},
		{"outcome flipped", func(ev []auditlog.Event) []auditlog.Event { ev[1].Outcome = "denied"; return ev }},
		{"event deleted", func(ev []auditlog.Event) []auditlog.Event { return append(ev[:1], ev[2:]...) }},
		{"events reordered", func(ev []auditlog.Event) []auditlog.Event { ev[0], ev[1] = ev[1], ev[0]; return ev }},
		{"hash forged", func(ev []auditlog.Event) []auditlog.Event { ev[2].Hash = ev[1].Hash; return ev }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tampered := tt.mutate(slices.Clone(events))
			_, err := auditlog.VerifyChain("", tampered)
			assert.ErrorIs(t, err, auditlog.ErrChainBroken)
		})
	}
}

func TestChain_PerStreamChains(t *testing.T) {
	t.Parallel()
	sink := auditlog.NewMemorySink()
	tenant := "org_a"
	rec := auditlog.New(sink,
		auditlog.WithChain(),
		auditlog.WithScope(func(context.Context) (string, error) { return tenant, nil }),
	)
	recordN(t, rec, 3, auditlog.Event{Action: "a", Outcome: "ok"})
	tenant = "org_b"
	recordN(t, rec, 2, auditlog.Event{Action: "a", Outcome: "ok"})
	tenant = "org_a"
	recordN(t, rec, 1, auditlog.Event{Action: "a", Outcome: "ok"})

	byTenant := map[string][]auditlog.Event{}
	for _, e := range sink.Events() {
		byTenant[e.Tenant] = append(byTenant[e.Tenant], e)
	}
	require.Len(t, byTenant["org_a"], 4)
	require.Len(t, byTenant["org_b"], 2)
	for _, stream := range byTenant {
		_, err := auditlog.VerifyChain("", stream)
		assert.NoError(t, err, "each tenant stream verifies independently")
	}
}

func TestChain_ResumesFromChainHead(t *testing.T) {
	t.Parallel()
	sink := auditlog.NewMemorySink()
	rec := auditlog.New(sink, auditlog.WithChain())
	recordN(t, rec, 2, auditlog.Event{Action: "a", Outcome: "ok"})

	// A new recorder (process restart) resumes the chain from the sink.
	rec2 := auditlog.New(sink, auditlog.WithChain())
	recordN(t, rec2, 2, auditlog.Event{Action: "a", Outcome: "ok"})

	_, err := auditlog.VerifyChain("", sink.Events())
	assert.NoError(t, err, "chain continues across recorder restarts")
}

func TestChain_SinkFailureDoesNotAdvanceHead(t *testing.T) {
	t.Parallel()
	sink := newFailSink()
	rec := auditlog.New(sink, auditlog.WithChain())
	recordN(t, rec, 1, auditlog.Event{Action: "a", Outcome: "ok"})

	sink.setFail(true)
	_, err := rec.Record(context.Background(), auditlog.Event{Action: "a", Outcome: "ok"})
	require.Error(t, err)

	sink.setFail(false)
	recordN(t, rec, 1, auditlog.Event{Action: "a", Outcome: "ok"})

	_, err = auditlog.VerifyChain("", sink.inner.Events())
	assert.NoError(t, err, "failed write leaves no gap in the chain")
}

// ambiguousSink persists the event but still reports an error — the
// canceled-after-commit edge of a real database sink.
type ambiguousSink struct {
	inner *auditlog.MemorySink
	fail  bool
}

func (s *ambiguousSink) Write(ctx context.Context, e auditlog.Event) error {
	if err := s.inner.Write(ctx, e); err != nil {
		return err
	}
	if s.fail {
		s.fail = false
		return errors.New("context canceled")
	}
	return nil
}

func (s *ambiguousSink) ChainHead(ctx context.Context, stream string) (string, error) {
	return s.inner.ChainHead(ctx, stream)
}

func TestChain_AmbiguousWriteDoesNotForkChain(t *testing.T) {
	t.Parallel()
	sink := &ambiguousSink{inner: auditlog.NewMemorySink()}
	rec := auditlog.New(sink, auditlog.WithChain())
	recordN(t, rec, 1, auditlog.Event{Action: "a", Outcome: "ok"})

	sink.fail = true
	_, err := rec.Record(context.Background(), auditlog.Event{Action: "a", Outcome: "ok"})
	require.Error(t, err, "the write persisted but was reported failed")

	recordN(t, rec, 1, auditlog.Event{Action: "a", Outcome: "ok"})

	events := sink.inner.Events()
	require.Len(t, events, 3)
	_, err = auditlog.VerifyChain("", events)
	assert.NoError(t, err, "recorder re-seeds from the persisted head instead of forking")
}

func TestChain_ConcurrentRecordStaysVerifiable(t *testing.T) {
	t.Parallel()
	sink := auditlog.NewMemorySink()
	rec := auditlog.New(sink, auditlog.WithChain())

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 25 {
				_, err := rec.Record(context.Background(), auditlog.Event{Action: "a", Outcome: "ok"})
				assert.NoError(t, err)
			}
		})
	}
	wg.Wait()

	events := sink.Events()
	require.Len(t, events, 200)
	_, err := auditlog.VerifyChain("", events)
	assert.NoError(t, err, "write order equals chain order under concurrency")
}

func TestComputeHash_FieldBoundariesAreUnambiguous(t *testing.T) {
	t.Parallel()
	a := auditlog.Event{Actor: "ab", Action: "c", Outcome: "ok"}
	b := auditlog.Event{Actor: "a", Action: "bc", Outcome: "ok"}
	assert.NotEqual(t, auditlog.ComputeHash(a), auditlog.ComputeHash(b), "length prefixes prevent concatenation collisions")

	nilMeta := auditlog.Event{Action: "a", Outcome: "ok"}
	emptyMeta := auditlog.Event{Action: "a", Outcome: "ok", Meta: map[string]string{}}
	assert.Equal(t, auditlog.ComputeHash(nilMeta), auditlog.ComputeHash(emptyMeta), "nil and empty meta hash identically")
}
