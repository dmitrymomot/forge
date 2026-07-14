package sqlite

import (
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
)

// pragma is one ordered PRAGMA applied per connection via a _pragma= DSN param.
type pragma struct{ name, value string }

var memCounter atomic.Uint64

// nextMemName returns a process-unique shared-cache in-memory database name.
func nextMemName() string {
	return "memdb-" + strconv.FormatUint(memCounter.Add(1), 10)
}

// isMemory reports whether path requests an in-memory database.
func isMemory(path string) bool {
	return path == ":memory:" || strings.Contains(path, "mode=memory")
}

// boolPragma renders a bool as a SQLite pragma argument.
func boolPragma(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// basePragmas returns the ordered connection pragmas shared by both pools.
// journal_mode is excluded (writer-only) and query_only is added by the reader path.
func basePragmas(cfg Config) []pragma {
	return []pragma{
		{"busy_timeout", strconv.FormatInt(cfg.BusyTimeout.Milliseconds(), 10)},
		{"synchronous", strings.ToUpper(cfg.Synchronous)},
		{"foreign_keys", boolPragma(cfg.ForeignKeys)},
		{"cache_size", strconv.Itoa(cfg.CacheSize)},
		{"mmap_size", strconv.FormatInt(cfg.MmapSize, 10)},
		{"temp_store", "MEMORY"},
	}
}

// buildDSN assembles the modernc DSN for one pool. The writer sets journal_mode(WAL)
// (file DBs only) and _txlock=immediate; the reader omits journal_mode, uses
// _txlock=deferred, and applies query_only(1) so it stays read-only. extra holds
// WithPragma additions, appended after the base set so they take precedence over the
// Config-derived pragma of the same name (see dedupePragmas).
//
// modernc.org/sqlite re-sorts every _pragma param (busy_timeout first, then ascending
// by the full "name(value)" string) before applying it per connection, so DSN order
// does not decide which value wins — including query_only, whose position here is
// irrelevant to the driver. dedupePragmas collapses the slice to one entry per name
// (last value wins) before rendering, so only one _pragma param per name ever reaches
// the driver and its re-sort has nothing left to reorder.
func buildDSN(cfg Config, extra []pragma, memory bool, memName string, write bool) string {
	var pragmas []pragma
	if write && !memory && cfg.JournalMode != "" {
		pragmas = append(pragmas, pragma{"journal_mode", strings.ToUpper(cfg.JournalMode)})
	}
	pragmas = append(pragmas, basePragmas(cfg)...)
	pragmas = append(pragmas, extra...)
	if !write {
		pragmas = append(pragmas, pragma{"query_only", "1"})
	}
	pragmas = dedupePragmas(pragmas)

	params := make([]string, 0, len(pragmas)+3)
	if memory {
		params = append(params, "mode=memory", "cache=shared")
	}
	if write {
		params = append(params, "_txlock=immediate")
	} else {
		params = append(params, "_txlock=deferred")
	}
	for _, p := range pragmas {
		if p.value == "" {
			continue
		}
		params = append(params, "_pragma="+p.name+"("+p.value+")")
	}

	var base string
	if memory {
		base = "file:" + memName
	} else {
		pathEncoded := pathToURI(cfg.Path)
		if strings.HasPrefix(pathEncoded, "//") {
			// An encoded path starting with "//" is ambiguous with the file://
			// authority separator (url.Parse would read the next segment as a host).
			// pathEncoded already begins with "//", so prefixing "file:///" yields
			// "file:////…": an empty authority, after which url.Parse/SQLite treat
			// the remaining "//…" as the path, round-tripping back to cfg.Path.
			base = "file:///" + pathEncoded[1:]
		} else {
			base = "file:" + pathEncoded
		}
	}
	return base + "?" + strings.Join(params, "&")
}

// dedupePragmas collapses pragmas to one entry per name, keeping the last-appended
// value. modernc.org/sqlite re-sorts _pragma params before applying them, so DSN
// position cannot express override precedence; collapsing to a single entry per name
// here (before rendering) is what makes an extra/WithPragma value deterministically
// override the Config-derived one of the same name.
func dedupePragmas(pragmas []pragma) []pragma {
	seen := make(map[string]int, len(pragmas))
	deduped := make([]pragma, 0, len(pragmas))
	for _, p := range pragmas {
		if i, ok := seen[p.name]; ok {
			deduped[i].value = p.value
			continue
		}
		seen[p.name] = len(deduped)
		deduped = append(deduped, p)
	}
	return deduped
}

// pathToURI renders an OS file path as the path portion of a file: URI, percent-
// encoding reserved characters (space, ?, #, …) so the DSN stays parseable.
func pathToURI(p string) string {
	u := url.URL{Path: p}
	return u.String()
}
