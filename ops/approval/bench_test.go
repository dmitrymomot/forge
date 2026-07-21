package approval_test

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/ops/approval"
)

func benchManager(b *testing.B, p approval.Policy, opts ...approval.Option) *approval.Manager {
	b.Helper()
	all := append([]approval.Option{
		approval.WithKind(kindPayout, p),
		approval.WithClock(clock.NewMock(fixedNow)),
	}, opts...)
	return approval.New(approval.NewMemoryStore(), all...)
}

func BenchmarkSubmit(b *testing.B) {
	m := benchManager(b, approval.Policy{Quorum: 2, TTL: 24 * time.Hour})
	ctx := context.Background()
	payload := payoutPayload{PayoutID: "po_88", Amount: 250000}
	p := approval.SubmitParams{Requester: "alice", Reason: "invoice"}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := approval.Submit(ctx, m, kindPayout, payload, p); err != nil {
			b.Fatal(err)
		}
	}
}

// benchmarkApprove times a single Approve call on an otherwise-fresh Pending
// request. Every request it approves must be distinct — approving the same
// request twice with the same actor hits ErrAlreadyVoted — so the b.N
// requests are submitted up front and only the Approve calls are timed.
//
// This uses the classic b.N loop rather than b.Loop(), because b.N must be
// known before the timed loop starts to size that fixture pool. Gating the
// setup with b.StopTimer/b.StartTimer per iteration instead measured mostly
// timer overhead: on this toolchain that combination inside a b.Loop() loop
// turned a sub-second benchmark into one that did not finish in minutes.
func benchmarkApprove(b *testing.B, quorum int) {
	ctx := context.Background()
	m := benchManager(b, approval.Policy{Quorum: quorum})
	reqs := make([]approval.Request, b.N)
	for i := range reqs {
		r, err := approval.Submit(ctx, m, kindPayout, payoutPayload{},
			approval.SubmitParams{Requester: "alice"})
		if err != nil {
			b.Fatal(err)
		}
		reqs[i] = r
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := m.Approve(ctx, reqs[i].ID, actor("bob")); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkApproveQuorum2(b *testing.B) { benchmarkApprove(b, 2) }
func BenchmarkApproveQuorum5(b *testing.B) { benchmarkApprove(b, 5) }

// BenchmarkApproveContended measures Approve's CAS-retry path (mutate's
// optimistic-concurrency loop) under contention that is SUSTAINED across the
// whole run, not just an initial ramp-up.
//
// A single Pending request cannot sustain this alone: once its quorum is
// met it goes terminal, and every later Approve call on it costs only a
// cheap Get+early-reject — no CAS, no retry. That was this benchmark's
// original shape (one request, Quorum: 100) and it does not measure what
// its name claims: B/op at -benchtime=2000x (7016 B/op) was statistically
// indistinguishable from a 1,000,000-iteration run (6872 B/op), which is
// only possible if the ~100-vote contended ramp-up is a negligible sliver
// of both runs and nearly every iteration in either run was the cheap
// rejected path, not a real CAS race.
//
// Fix: pre-create a pool of many low-quorum Pending requests, sized to
// b.N/quorum plus headroom, and route every parallel worker through one
// shared "current" index. All live workers hammer the SAME request at
// once — quorum is far below the worker count, so simultaneous CAS
// conflicts are all but guaranteed — and the instant it fills, whichever
// worker notices advances the shared index so every worker rotates onto a
// fresh Pending request together. Because the pool is sized off b.N, this
// keeps the entire run inside the contended CAS path regardless of
// -benchtime, instead of degenerating into cheap rejects after ~100 votes.
func BenchmarkApproveContended(b *testing.B) {
	const quorum = 4 // well below the worker count: guarantees real simultaneous CAS conflicts per request
	m := benchManager(b, approval.Policy{Quorum: quorum}, approval.WithMaxRetries(1000))
	ctx := context.Background()

	poolSize := b.N/quorum + 64 // headroom absorbs tail races so the pool never runs dry mid-run
	reqs := make([]id.UUID, poolSize)
	for i := range reqs {
		r, err := approval.Submit(ctx, m, kindPayout, payoutPayload{},
			approval.SubmitParams{Requester: "alice"})
		if err != nil {
			b.Fatal(err)
		}
		reqs[i] = r.ID
	}

	var cur atomic.Int64 // index into reqs of the request every worker is currently contending
	var approverSeq atomic.Int64

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			approver := actor("approver-" + strconv.FormatInt(approverSeq.Add(1), 10))
			for {
				i := cur.Load()
				if i >= int64(len(reqs)) {
					break // pool exhausted; headroom keeps this vanishingly rare
				}
				_, err := m.Approve(ctx, reqs[i], approver)
				if err == nil {
					break
				}
				if errors.Is(err, approval.ErrNotPending) || errors.Is(err, approval.ErrExpired) {
					cur.CompareAndSwap(i, i+1) // whoever notices first rotates every worker forward together
					continue
				}
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkGet(b *testing.B) {
	m := benchManager(b, approval.Policy{Quorum: 2, TTL: 24 * time.Hour})
	ctx := context.Background()
	r, err := approval.Submit(ctx, m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "alice"})
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := m.Get(ctx, r.ID); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkList100(b *testing.B) {
	m := benchManager(b, approval.Policy{Quorum: 2, TTL: 24 * time.Hour})
	ctx := context.Background()
	for range 100 {
		if _, err := approval.Submit(ctx, m, kindPayout, payoutPayload{},
			approval.SubmitParams{Requester: "alice"}); err != nil {
			b.Fatal(err)
		}
	}
	f := approval.Filter{Statuses: []approval.Status{approval.Pending}}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := m.List(ctx, f); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPayloadOf(b *testing.B) {
	m := benchManager(b, approval.Policy{Quorum: 2})
	r, err := approval.Submit(context.Background(), m, kindPayout,
		payoutPayload{PayoutID: "po_88", Amount: 250000},
		approval.SubmitParams{Requester: "alice"})
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := approval.PayloadOf(kindPayout, r); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkApproveWithDecider mirrors benchmarkApprove's fixture-pool
// approach: b.N Pending requests are submitted up front so only the
// decider-gated Approve calls are timed.
func BenchmarkApproveWithDecider(b *testing.B) {
	allow := access.DeciderFunc(func(context.Context, access.Subject, access.Action, access.Resource) (access.Decision, error) {
		return access.Allow.Because("bench"), nil
	})
	ctx := context.Background()
	m := benchManager(b, approval.Policy{Quorum: 2}, approval.WithDecider(allow))
	reqs := make([]approval.Request, b.N)
	for i := range reqs {
		r, err := approval.Submit(ctx, m, kindPayout, payoutPayload{},
			approval.SubmitParams{Requester: "alice"})
		if err != nil {
			b.Fatal(err)
		}
		reqs[i] = r
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := m.Approve(ctx, reqs[i].ID, actor("bob")); err != nil {
			b.Fatal(err)
		}
	}
}
