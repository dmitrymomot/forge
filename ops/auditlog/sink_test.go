package auditlog_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/auditlog"
)

func TestJSONLSink_RoundTripsAndVerifies(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	rec := auditlog.New(auditlog.NewJSONLSink(&buf), auditlog.WithChain())
	recordN(t, rec, 3, auditlog.Event{
		Actor: "user_1", Action: "doc.edit", Resource: "doc:9",
		Outcome: auditlog.OutcomeSuccess, Meta: map[string]string{"field": "title"},
	})

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 3)

	events := make([]auditlog.Event, 0, len(lines))
	for _, line := range lines {
		var e auditlog.Event
		require.NoError(t, json.Unmarshal([]byte(line), &e))
		events = append(events, e)
	}
	assert.Equal(t, "user_1", events[0].Actor)
	assert.Equal(t, map[string]string{"field": "title"}, events[0].Meta)

	_, err := auditlog.VerifyChain("", events)
	assert.NoError(t, err, "a decoded JSONL trail verifies offline")
}

func TestNewJSONLSink_NilWriterPanics(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() { auditlog.NewJSONLSink(nil) })
}

func TestSlogSink_EmitsStructuredRecord(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	sink := auditlog.NewSlogSink(slog.New(slog.NewJSONHandler(&buf, nil)))
	rec := auditlog.New(sink, auditlog.WithChain())

	e, err := rec.Record(context.Background(), auditlog.Event{
		Tenant: "org_1", Actor: "user_1", Action: "member.remove",
		Resource: "member:2", Outcome: auditlog.OutcomeDenied,
		Meta: map[string]string{"reason": "last admin"},
	})
	require.NoError(t, err)

	var out map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &out))
	assert.Equal(t, "audit", out["msg"])
	assert.Equal(t, e.ID.String(), out["event_id"])
	assert.Equal(t, "member.remove", out["action"])
	assert.Equal(t, "denied", out["outcome"])
	assert.Equal(t, "org_1", out["tenant"])
	assert.Equal(t, "user_1", out["actor"])
	assert.Equal(t, "member:2", out["resource"])
	assert.Equal(t, e.Hash, out["hash"])
	assert.Equal(t, map[string]any{"reason": "last admin"}, out["meta"])
}

func TestSlogSink_SkipsEmptyFields(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	sink := auditlog.NewSlogSink(slog.New(slog.NewJSONHandler(&buf, nil)))
	require.NoError(t, sink.Write(context.Background(), auditlog.Event{Action: "a", Outcome: "ok"}))

	var out map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &out))
	for _, absent := range []string{"tenant", "actor", "resource", "meta", "hash", "prev_hash"} {
		assert.NotContains(t, out, absent)
	}
}

func TestSlogSink_NilLoggerDiscards(t *testing.T) {
	t.Parallel()
	sink := auditlog.NewSlogSink(nil)
	assert.NoError(t, sink.Write(context.Background(), auditlog.Event{Action: "a", Outcome: "ok"}))
}

func TestMemorySink_EventsReturnsCopy(t *testing.T) {
	t.Parallel()
	sink := auditlog.NewMemorySink()
	require.NoError(t, sink.Write(context.Background(), auditlog.Event{Action: "a", Outcome: "ok"}))
	events := sink.Events()
	events[0].Action = "mutated"
	assert.Equal(t, "a", sink.Events()[0].Action)
}
