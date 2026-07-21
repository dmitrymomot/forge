package risk_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/web/risk"
)

func mwEngine(t *testing.T, score float64, opts ...risk.Option[string]) *risk.Engine[string] {
	t.Helper()
	e, err := risk.New(append([]risk.Option[string]{
		risk.WithScorer(constScorer(score)),
		risk.WithGate[string](0.8),
	}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func buildInput(r *http.Request) string { return r.URL.Path }

func serve(mw func(http.Handler) http.Handler, nextCalled *bool) *httptest.ResponseRecorder {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*nextCalled = true
		w.WriteHeader(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/click", nil))
	return rec
}

func TestMiddlewarePass(t *testing.T) {
	t.Parallel()
	var nextCalled bool
	rec := serve(risk.Middleware(mwEngine(t, 0.2), buildInput), &nextCalled)
	if !nextCalled || rec.Code != http.StatusOK {
		t.Fatalf("pass path: nextCalled=%v code=%d, want true/200", nextCalled, rec.Code)
	}
}

func TestMiddlewareFraudDefault403(t *testing.T) {
	t.Parallel()
	var nextCalled bool
	rec := serve(risk.Middleware(mwEngine(t, 0.9), buildInput), &nextCalled)
	if nextCalled || rec.Code != http.StatusForbidden {
		t.Fatalf("fraud path: nextCalled=%v code=%d, want false/403", nextCalled, rec.Code)
	}
}

func TestMiddlewareScorerErrorFailsClosed(t *testing.T) {
	t.Parallel()
	e, err := risk.New(
		risk.WithScorer(func(context.Context, string) (float64, error) { return 0, errors.New("lookup down") }),
		risk.WithGate[string](0.8),
	)
	if err != nil {
		t.Fatal(err)
	}
	var nextCalled bool
	rec := serve(risk.Middleware(e, buildInput), &nextCalled)
	if nextCalled || rec.Code != http.StatusForbidden {
		t.Fatalf("infra error path: nextCalled=%v code=%d, want false/403", nextCalled, rec.Code)
	}
}

func TestMiddlewareRejectHandlerOverride(t *testing.T) {
	t.Parallel()
	var gotErr error
	mw := risk.Middleware(mwEngine(t, 0.9), buildInput,
		risk.WithRejectHandler(func(w http.ResponseWriter, r *http.Request, err error) {
			gotErr = err
			http.Redirect(w, r, "https://decoy.example", http.StatusFound)
		}))
	var nextCalled bool
	rec := serve(mw, &nextCalled)
	if nextCalled || rec.Code != http.StatusFound {
		t.Fatalf("override path: nextCalled=%v code=%d, want false/302", nextCalled, rec.Code)
	}
	if !errors.Is(gotErr, risk.ErrFraud) {
		t.Fatalf("reject handler err = %v, want ErrFraud match", gotErr)
	}
}

func TestMiddlewareShadowModeProceeds(t *testing.T) {
	t.Parallel()
	handlerRan := false
	e := mwEngine(t, 0.9, risk.OnFraud(func(context.Context, string, risk.Score) error {
		handlerRan = true
		return nil
	}))
	var nextCalled bool
	rec := serve(risk.Middleware(e, buildInput), &nextCalled)
	if !nextCalled || rec.Code != http.StatusOK {
		t.Fatalf("shadow path: nextCalled=%v code=%d, want true/200", nextCalled, rec.Code)
	}
	if !handlerRan {
		t.Fatal("OnFraud handler did not run in shadow mode")
	}
}

// recordHandler captures slog records for assertions.
type recordHandler struct {
	records *[]slog.Record
}

func (h recordHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h recordHandler) Handle(_ context.Context, r slog.Record) error {
	*h.records = append(*h.records, r)
	return nil
}
func (h recordHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h recordHandler) WithGroup(string) slog.Handler      { return h }

func TestMiddlewareLogsFraudAtWarnWithAttrs(t *testing.T) {
	t.Parallel()
	var records []slog.Record
	log := slog.New(recordHandler{records: &records})
	var nextCalled bool
	serve(risk.Middleware(mwEngine(t, 0.9), buildInput, risk.WithLogger(log)), &nextCalled)
	if len(records) != 1 || records[0].Level != slog.LevelWarn {
		t.Fatalf("records = %d, want 1 Warn record; got %+v", len(records), records)
	}
	attrs := map[string]slog.Value{}
	records[0].Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value
		return true
	})
	if got := attrs["score"].Float64(); got != 0.9 {
		t.Errorf("score attr = %v, want 0.9", got)
	}
	if got := attrs["peak_scorer"].Int64(); got != 0 {
		t.Errorf("peak_scorer attr = %v, want 0", got)
	}
	if got := attrs["path"].String(); got != "/click" {
		t.Errorf("path attr = %q, want /click", got)
	}
}

func TestMiddlewareLogsInfraErrorAtError(t *testing.T) {
	t.Parallel()
	e, err := risk.New(
		risk.WithScorer(func(context.Context, string) (float64, error) { return 0, errors.New("lookup down") }),
		risk.WithGate[string](0.8),
	)
	if err != nil {
		t.Fatal(err)
	}
	var records []slog.Record
	log := slog.New(recordHandler{records: &records})
	var nextCalled bool
	serve(risk.Middleware(e, buildInput, risk.WithLogger(log)), &nextCalled)
	if len(records) != 1 || records[0].Level != slog.LevelError {
		t.Fatalf("records = %d, want 1 Error record; got %+v", len(records), records)
	}
}

func TestMiddlewareWithLoggerNilKeepsDefault(t *testing.T) {
	t.Parallel()
	var nextCalled bool
	// Must not panic: nil is ignored and the NewNope default stays.
	rec := serve(risk.Middleware(mwEngine(t, 0.9), buildInput, risk.WithLogger(nil)), &nextCalled)
	if nextCalled || rec.Code != http.StatusForbidden {
		t.Fatalf("nil-logger path: nextCalled=%v code=%d, want false/403", nextCalled, rec.Code)
	}
}

func TestMiddlewareNilArgsPanic(t *testing.T) {
	t.Parallel()
	assertPanics := func(name string, fn func()) {
		defer func() {
			if recover() == nil {
				t.Errorf("%s: expected panic", name)
			}
		}()
		fn()
	}
	assertPanics("nil engine", func() { risk.Middleware(nil, buildInput) })
	assertPanics("nil buildInput", func() { risk.Middleware(mwEngine(t, 0.1), nil) })
}
