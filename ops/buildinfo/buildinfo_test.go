package buildinfo_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/ops/buildinfo"
)

func TestReadPopulatesGoVersion(t *testing.T) {
	if buildinfo.Read().GoVersion == "" {
		t.Fatal("Read().GoVersion must be populated from runtime.Version()")
	}
}

func TestInfoString(t *testing.T) {
	for _, tt := range []struct {
		name string
		in   buildinfo.Info
		want string
	}{
		{"empty renders dev", buildinfo.Info{}, "dev"},
		{"version only", buildinfo.Info{Version: "1.2.3"}, "1.2.3"},
		{"version commit time", buildinfo.Info{Version: "1.2.3", Commit: "abcdef1234567890", BuildTime: "2026-07-07T12:00:00Z"}, "1.2.3 (abcdef123456 2026-07-07T12:00:00Z)"},
		{"short commit kept whole", buildinfo.Info{Version: "1.2.3", Commit: "abc1234"}, "1.2.3 (abc1234)"},
		{"dirty flag", buildinfo.Info{Version: "1.2.3", Commit: "abc1234", Dirty: true}, "1.2.3 (abc1234, dirty)"},
		{"dev dirty no meta", buildinfo.Info{Dirty: true}, "dev (dirty)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInfoLogValueGroups(t *testing.T) {
	var buf strings.Builder
	slog.New(slog.NewJSONHandler(&buf, nil)).
		Info("msg", slog.Any("build", buildinfo.Info{Version: "1.2.3", Commit: "abc1234def", GoVersion: "go1.26", Dirty: true}))

	var rec map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &rec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	grp, ok := rec["build"].(map[string]any)
	if !ok {
		t.Fatalf("build attr is not a group: %T", rec["build"])
	}
	if grp["version"] != "1.2.3" {
		t.Errorf("version = %v", grp["version"])
	}
	if grp["dirty"] != true {
		t.Errorf("dirty = %v", grp["dirty"])
	}
}

func TestInfoLogValueOmitsDirtyWhenFalse(t *testing.T) {
	var buf strings.Builder
	slog.New(slog.NewJSONHandler(&buf, nil)).
		Info("msg", slog.Any("build", buildinfo.Info{Version: "1.2.3"}))
	if strings.Contains(buf.String(), "dirty") {
		t.Errorf("dirty should be omitted when false: %s", buf.String())
	}
}

func TestInfoHandlerServesJSON(t *testing.T) {
	rr := httptest.NewRecorder()
	buildinfo.Info{Version: "1.2.3", Commit: "abc"}.Handler().
		ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/version", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
	var got buildinfo.Info
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Version != "1.2.3" || got.Commit != "abc" {
		t.Errorf("decoded = %+v", got)
	}
}
