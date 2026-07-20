package auditlog

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"

	"github.com/dmitrymomot/forge/ops/logger"
)

// Sink receives finalized audit events. Implementations must be safe for
// concurrent use and must persist (or durably hand off) the event before
// returning nil — Record reports the sink's error to the caller, and a
// chained recorder advances the stream head only on success.
type Sink interface {
	Write(ctx context.Context, e Event) error
}

// ChainHead is optionally implemented by a Sink that can report the last
// persisted chain hash of a stream ("" when the stream is empty). A
// WithChain recorder uses it to resume a stream's chain across process
// restarts instead of starting a new one.
type ChainHead interface {
	ChainHead(ctx context.Context, stream string) (string, error)
}

// SlogSink writes audit events to a slog.Logger as level-Info "audit"
// records with structured attributes. It is an observability sink — logs
// are typically rotated and unqueryable per tenant — so pair it with a
// durable sink (pgsink, JSONL) when the trail must be retained.
type SlogSink struct {
	log *slog.Logger
}

// NewSlogSink builds a SlogSink over l; a nil l discards events
// (logger.NewNope).
func NewSlogSink(l *slog.Logger) *SlogSink {
	if l == nil {
		l = logger.NewNope()
	}
	return &SlogSink{log: l}
}

// Write logs e. It never fails.
func (s *SlogSink) Write(ctx context.Context, e Event) error {
	attrs := make([]slog.Attr, 0, 10)
	attrs = append(attrs,
		slog.String("event_id", e.ID.String()),
		slog.Time("occurred_at", e.Time),
		slog.String("action", e.Action),
		slog.String("outcome", string(e.Outcome)),
	)
	if e.Tenant != "" {
		attrs = append(attrs, slog.String("tenant", e.Tenant))
	}
	if e.Actor != "" {
		attrs = append(attrs, slog.String("actor", e.Actor))
	}
	if e.Resource != "" {
		attrs = append(attrs, slog.String("resource", e.Resource))
	}
	if len(e.Meta) > 0 {
		attrs = append(attrs, slog.Any("meta", e.Meta))
	}
	if e.Hash != "" {
		attrs = append(attrs, slog.String("prev_hash", e.PrevHash), slog.String("hash", e.Hash))
	}
	s.log.LogAttrs(ctx, slog.LevelInfo, "audit", attrs...)
	return nil
}

// JSONLSink appends one JSON object per line to w — a file-backed audit
// trail. Writes are serialized so lines never interleave; the caller owns
// w's lifecycle (open with O_APPEND, close after the recorder stops).
type JSONLSink struct {
	enc *json.Encoder
	mu  sync.Mutex
}

// NewJSONLSink builds a JSONLSink over w. It panics on a nil w — a wiring
// bug caught at startup.
func NewJSONLSink(w io.Writer) *JSONLSink {
	if w == nil {
		panic("auditlog: nil writer")
	}
	return &JSONLSink{enc: json.NewEncoder(w)}
}

// Write appends e as one JSON line.
func (s *JSONLSink) Write(_ context.Context, e Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enc.Encode(e)
}
