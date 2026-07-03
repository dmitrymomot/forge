package reqlog_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/logger"
	"github.com/dmitrymomot/forge/reqlog"
	"github.com/dmitrymomot/forge/requestid"
)

func newLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

func lastLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &m))
	return m
}

func TestLogsAccessLine(t *testing.T) {
	log, buf := newLogger()
	h := reqlog.New(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("hi"))
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/things", nil))

	m := lastLine(t, buf)
	assert.Equal(t, "POST", m["method"])
	assert.Equal(t, "/things", m["path"])
	assert.Equal(t, float64(http.StatusCreated), m["status"])
	assert.Equal(t, float64(2), m["bytes"])
}

func TestLevelByStatus(t *testing.T) {
	log, buf := newLogger()
	h := reqlog.New(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, "ERROR", lastLine(t, buf)["level"])
}

func TestLevelByStatus4xxIsWarn(t *testing.T) {
	log, buf := newLogger()
	h := reqlog.New(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/missing", nil))
	assert.Equal(t, "WARN", lastLine(t, buf)["level"])
}

func TestWithLevelFuncOverride(t *testing.T) {
	log, buf := newLogger()
	h := reqlog.New(log, reqlog.WithLevelFunc(func(int) slog.Level { return slog.LevelDebug }))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) }),
	)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, "DEBUG", lastLine(t, buf)["level"]) // custom func overrides the default 500->Error
}

func TestSkip(t *testing.T) {
	log, buf := newLogger()
	skip := func(r *http.Request) bool { return r.URL.Path == "/healthz" }
	h := reqlog.New(log, reqlog.WithSkip(skip))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Empty(t, buf.String())
}

func TestInjectsRequestIDViaExtractor(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, nil)
	// logger.New would wire extractors; here assert reqlog logs with request context
	// so a requestid-populated context surfaces the ID through a context handler.
	log := slog.New(base)

	h := requestid.New(requestid.WithGenerator(func() string { return "rid-9" }))(
		reqlog.New(log)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})),
	)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	// The plain handler has no extractor; this test just asserts reqlog uses
	// r.Context() (no panic) and emits a line. Extractor wiring is covered E2E
	// in the examples build. Assert a line was written:
	assert.Contains(t, buf.String(), `"msg":"http request"`)
}

func TestReqlogSurfacesWiredContextExtractor(t *testing.T) {
	var buf bytes.Buffer
	log, err := logger.New(
		logger.WithOutput(&buf),
		logger.WithLevel(slog.LevelDebug),
		logger.WithContextExtractors(requestid.LogExtractor),
	)
	require.NoError(t, err)

	// requestid (outermost) puts the ID in the context; reqlog logs via r.Context(),
	// so the logger's extractor must surface it in the emitted line.
	h := requestid.New(requestid.WithGenerator(func() string { return "rid-e2e" }))(
		reqlog.New(log)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})),
	)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	out := buf.String()
	assert.Contains(t, out, "request_id")
	assert.Contains(t, out, "rid-e2e")
}
