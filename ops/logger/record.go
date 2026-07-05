package logger

import (
	"context"
	"log/slog"
	"maps"
	"sync"
	"time"
)

// Record is one captured log record with attributes flattened to dotted keys (a
// WithGroup("http") + Int("status", …) attr becomes "http.status").
type Record struct {
	Time    time.Time
	Attrs   map[string]any
	Message string
	Level   slog.Level
}

// Recorder is a concurrency-safe slog.Handler sink that captures records for test
// assertions. It is the seam owner's test double — there is no central fakes
// package. Construct it with NewRecorder.
type Recorder struct {
	records []Record
	mu      sync.Mutex
}

// NewRecorder returns a *slog.Logger writing into the returned *Recorder.
func NewRecorder() (*slog.Logger, *Recorder) {
	r := &Recorder{}
	return slog.New(&recordHandler{rec: r}), r
}

// Records returns a snapshot copy of the captured records.
func (r *Recorder) Records() []Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Record, len(r.records))
	copy(out, r.records)
	return out
}

// Len returns the number of captured records.
func (r *Recorder) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.records)
}

// Reset discards all captured records.
func (r *Recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = nil
}

// Contains reports whether any captured record has the given level and message.
func (r *Recorder) Contains(level slog.Level, msg string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rec := range r.records {
		if rec.Level == level && rec.Message == msg {
			return true
		}
	}
	return false
}

func (r *Recorder) append(rec Record) {
	r.mu.Lock()
	r.records = append(r.records, rec)
	r.mu.Unlock()
}

// recordHandler is the slog.Handler that flattens attributes and appends to the
// shared Recorder. WithAttrs/WithGroup return fresh handlers (no mutation).
type recordHandler struct {
	rec    *Recorder
	attrs  map[string]any // pre-bound (WithAttrs) attrs, already flattened
	prefix string         // dotted group prefix, e.g. "http."
}

func (h *recordHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordHandler) Handle(_ context.Context, rec slog.Record) error {
	flat := make(map[string]any, len(h.attrs)+rec.NumAttrs())
	maps.Copy(flat, h.attrs)
	rec.Attrs(func(a slog.Attr) bool {
		flattenAttr(flat, h.prefix, a)
		return true
	})
	h.rec.append(Record{
		Time:    rec.Time,
		Level:   rec.Level,
		Message: rec.Message,
		Attrs:   flat,
	})
	return nil
}

func (h *recordHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := h.clone()
	for _, a := range attrs {
		flattenAttr(next.attrs, h.prefix, a)
	}
	return next
}

func (h *recordHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	next := h.clone()
	next.prefix = h.prefix + name + "."
	return next
}

func (h *recordHandler) clone() *recordHandler {
	attrs := make(map[string]any, len(h.attrs))
	maps.Copy(attrs, h.attrs)
	return &recordHandler{rec: h.rec, prefix: h.prefix, attrs: attrs}
}

// flattenAttr writes a into dst under prefix, recursing into group values so nested
// keys become dotted (prefix + group + "." + key).
func flattenAttr(dst map[string]any, prefix string, a slog.Attr) {
	a.Value = a.Value.Resolve()
	if a.Value.Kind() == slog.KindGroup {
		gp := prefix
		if a.Key != "" {
			gp = prefix + a.Key + "."
		}
		for _, ga := range a.Value.Group() {
			flattenAttr(dst, gp, ga)
		}
		return
	}
	dst[prefix+a.Key] = a.Value.Any()
}
