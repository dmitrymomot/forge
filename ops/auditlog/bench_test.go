package auditlog_test

import (
	"context"
	"testing"

	"github.com/dmitrymomot/forge/ops/auditlog"
)

// discardSink accepts and drops every event, isolating recorder overhead.
type discardSink struct{}

func (discardSink) Write(context.Context, auditlog.Event) error { return nil }

var benchEvent = auditlog.Event{
	Tenant: "org_1", Actor: "user_42", Action: "member.invite",
	Resource: "member:bob@example.com", Outcome: auditlog.OutcomeSuccess,
	Meta: map[string]string{"role": "admin", "request_id": "req_123"},
}

func BenchmarkRecord(b *testing.B) {
	rec := auditlog.New(discardSink{})
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := rec.Record(ctx, benchEvent); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRecordChained(b *testing.B) {
	rec := auditlog.New(discardSink{}, auditlog.WithChain())
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := rec.Record(ctx, benchEvent); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkComputeHash(b *testing.B) {
	e := benchEvent
	e.PrevHash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	b.ReportAllocs()
	for b.Loop() {
		_ = auditlog.ComputeHash(e)
	}
}
