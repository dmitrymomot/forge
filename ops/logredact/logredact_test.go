package logredact_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/dmitrymomot/forge/ops/logredact"
)

func newLog(buf *bytes.Buffer, opts ...logredact.Option) *slog.Logger {
	return slog.New(logredact.New(slog.NewJSONHandler(buf, nil), opts...))
}

func decode(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("unmarshal %q: %v", buf.String(), err)
	}
	return m
}

func TestRedactTopLevelKey(t *testing.T) {
	var buf bytes.Buffer
	newLog(&buf, logredact.WithKeys("password")).Info("login", "password", "hunter2", "user", "alice")
	m := decode(t, &buf)
	if m["password"] != "[REDACTED]" {
		t.Errorf("password = %v, want [REDACTED]", m["password"])
	}
	if m["user"] != "alice" {
		t.Errorf("user = %v, want alice (untouched)", m["user"])
	}
}

func TestRedactKeyInsideGroupValue(t *testing.T) {
	var buf bytes.Buffer
	newLog(&buf, logredact.WithKeys("password")).
		Info("m", slog.Group("creds", slog.String("password", "hunter2"), slog.String("kind", "basic")))
	creds := decode(t, &buf)["creds"].(map[string]any)
	if creds["password"] != "[REDACTED]" {
		t.Errorf("creds.password = %v", creds["password"])
	}
	if creds["kind"] != "basic" {
		t.Errorf("creds.kind = %v", creds["kind"])
	}
}

func TestRedactKeyUnderWithGroup(t *testing.T) {
	var buf bytes.Buffer
	newLog(&buf, logredact.WithKeys("password")).WithGroup("auth").Info("m", "password", "hunter2")
	auth := decode(t, &buf)["auth"].(map[string]any)
	if auth["password"] != "[REDACTED]" {
		t.Errorf("auth.password = %v", auth["password"])
	}
}

func TestRedactByDottedPath(t *testing.T) {
	var buf bytes.Buffer
	newLog(&buf, logredact.WithPaths("user.ssn")).Info("m",
		slog.Group("user", slog.String("ssn", "123-45-6789"), slog.String("name", "alice")),
		slog.String("ssn", "top"))
	m := decode(t, &buf)
	if user := m["user"].(map[string]any); user["ssn"] != "[REDACTED]" {
		t.Errorf("user.ssn = %v, want redacted", user["ssn"])
	}
	if m["ssn"] != "top" {
		t.Errorf("top-level ssn = %v, want untouched", m["ssn"])
	}
}

func TestRedactByDottedPathTwoLevels(t *testing.T) {
	var buf bytes.Buffer
	// Two levels of nesting exercise joinPath/group-prefix accumulation at depth:
	// only a.b.token matches, not a same-named token one level up or at the root.
	newLog(&buf, logredact.WithPaths("a.b.token")).Info("m",
		slog.Group("a",
			slog.String("token", "shallow"),
			slog.Group("b", slog.String("token", "deep"), slog.String("kind", "basic"))),
		slog.String("token", "top"))
	m := decode(t, &buf)
	a := m["a"].(map[string]any)
	if b := a["b"].(map[string]any); b["token"] != "[REDACTED]" {
		t.Errorf("a.b.token = %v, want redacted", b["token"])
	}
	if a["token"] != "shallow" {
		t.Errorf("a.token = %v, want untouched", a["token"])
	}
	if m["token"] != "top" {
		t.Errorf("top-level token = %v, want untouched", m["token"])
	}
}

func TestRedactWithAttrsBakedIn(t *testing.T) {
	var buf bytes.Buffer
	// With(...) routes through WithAttrs, not Handle's record attrs — the
	// regression a naive Handle-only redactor misses.
	newLog(&buf, logredact.WithKeys("token")).With("token", "secret-abc").Info("m")
	if decode(t, &buf)["token"] != "[REDACTED]" {
		t.Errorf("WithAttrs-baked token not redacted: %s", buf.String())
	}
}

func TestCustomReplacement(t *testing.T) {
	var buf bytes.Buffer
	newLog(&buf, logredact.WithKeys("password"), logredact.WithReplacement("***")).Info("m", "password", "x")
	if got := decode(t, &buf)["password"]; got != "***" {
		t.Errorf("replacement = %v, want ***", got)
	}
}

func TestNonMatchingPassThrough(t *testing.T) {
	var buf bytes.Buffer
	newLog(&buf, logredact.WithKeys("password")).Info("m", "count", 42, "name", "alice")
	m := decode(t, &buf)
	if m["count"] != float64(42) || m["name"] != "alice" {
		t.Errorf("non-matching attrs altered: %+v", m)
	}
}

func TestEnabledDelegates(t *testing.T) {
	var buf bytes.Buffer
	h := logredact.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("Enabled(Info) should be false (delegates to Warn-level next)")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Error("Enabled(Error) should be true")
	}
}
