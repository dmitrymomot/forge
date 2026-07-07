// Package buildinfo gives a binary its build identity: version, commit,
// build time, Go toolchain version, and a dirty-worktree flag.
//
// Read merges link-time ldflags (authoritative when set via `-X`) over
// runtime/debug.ReadBuildInfo, which supplies the VCS revision/time/dirty
// bit stamped by `go build` and the module version recorded by
// `go install pkg@version`. With neither source, fields stay empty and
// String/LogValue substitute "dev":
//
//	go build -ldflags "\
//	  -X github.com/dmitrymomot/forge/ops/buildinfo.version=$(git describe --tags --always) \
//	  -X github.com/dmitrymomot/forge/ops/buildinfo.commit=$(git rev-parse --short HEAD) \
//	  -X github.com/dmitrymomot/forge/ops/buildinfo.buildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
//
// Info is a plain value type: it satisfies fmt.Stringer for a one-line
// summary, slog.LogValuer for a grouped log attribute, and it can serve
// itself as a JSON http.Handler (e.g. mounted at /version).
package buildinfo
