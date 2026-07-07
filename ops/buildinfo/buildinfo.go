package buildinfo

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"runtime/debug"
	"strings"
)

// Link-time overrides. Set with, e.g.:
//
//	go build -ldflags "\
//	  -X github.com/dmitrymomot/forge/ops/buildinfo.version=$(git describe --tags --always) \
//	  -X github.com/dmitrymomot/forge/ops/buildinfo.commit=$(git rev-parse --short HEAD) \
//	  -X github.com/dmitrymomot/forge/ops/buildinfo.buildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
//
// They are the ONLY package-level vars, are written once by the linker, never
// mutated at runtime, and are read solely by Read.
var (
	version   string
	commit    string
	buildTime string
)

// Info is a snapshot of the binary's build identity. The zero value is valid;
// unknown fields are empty (Version reads "dev" via String/LogValue).
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
	GoVersion string `json:"go_version"`
	Dirty     bool   `json:"dirty"`
}

// Read merges link-time ldflags (authoritative when set) over
// runtime/debug.ReadBuildInfo: VCS revision/time/modified from `go build`
// stamping, and the module version from `go install pkg@version`. With neither
// source, fields stay empty and String/LogValue substitute "dev".
func Read() Info {
	i := Info{Version: version, Commit: commit, BuildTime: buildTime, GoVersion: runtime.Version()}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if i.Version == "" && bi.Main.Version != "" {
			i.Version = bi.Main.Version
		}
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if i.Commit == "" {
					i.Commit = s.Value
				}
			case "vcs.time":
				if i.BuildTime == "" {
					i.BuildTime = s.Value
				}
			case "vcs.modified":
				if s.Value == "true" {
					i.Dirty = true
				}
			}
		}
	}
	return i
}

func (i Info) versionOrDev() string {
	if i.Version == "" {
		return "dev"
	}
	return i.Version
}

func (i Info) shortCommit() string {
	if len(i.Commit) > 12 {
		return i.Commit[:12]
	}
	return i.Commit
}

// String is a single-line summary: "1.2.3 (abc1234def0 2026-07-07T12:00:00Z, dirty)".
func (i Info) String() string {
	var b strings.Builder
	b.WriteString(i.versionOrDev())
	var parens []string
	if c := i.shortCommit(); c != "" {
		parens = append(parens, c)
	}
	if i.BuildTime != "" {
		parens = append(parens, i.BuildTime)
	}
	if len(parens) > 0 {
		fmt.Fprintf(&b, " (%s", strings.Join(parens, " "))
		if i.Dirty {
			b.WriteString(", dirty")
		}
		b.WriteByte(')')
	} else if i.Dirty {
		b.WriteString(" (dirty)")
	}
	return b.String()
}

// LogValue renders build identity as a grouped slog attribute, so
// log.Info("starting", slog.Any("build", info)) nests version/commit/… .
func (i Info) LogValue() slog.Value {
	attrs := []slog.Attr{slog.String("version", i.versionOrDev())}
	if c := i.shortCommit(); c != "" {
		attrs = append(attrs, slog.String("commit", c))
	}
	if i.BuildTime != "" {
		attrs = append(attrs, slog.String("build_time", i.BuildTime))
	}
	if i.GoVersion != "" {
		attrs = append(attrs, slog.String("go", i.GoVersion))
	}
	if i.Dirty {
		attrs = append(attrs, slog.Bool("dirty", true))
	}
	return slog.GroupValue(attrs...)
}

// Handler serves the Info as JSON. Mount at /version behind an auth guard if the
// commit is sensitive.
func (i Info) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(i)
	})
}
