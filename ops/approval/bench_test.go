package approval_test

import (
	"context"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/core/clock"
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

func BenchmarkApproveContended(b *testing.B) {
	m := benchManager(b, approval.Policy{Quorum: 100}, approval.WithMaxRetries(1000))
	ctx := context.Background()
	r, err := approval.Submit(ctx, m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "alice"})
	if err != nil {
		b.Fatal(err)
	}

	var next atomic.Int64

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// A shared counter keeps approver ids distinct across goroutines,
			// not just within one. ErrAlreadyVoted/ErrNotPending are the
			// expected steady state once quorum is met, so ignore errors and
			// measure the CAS path itself.
			i := next.Add(1)
			_, _ = m.Approve(ctx, r.ID, actor("approver-"+strconv.FormatInt(i, 10)))
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
