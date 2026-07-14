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
// _txlock=deferred, and applies query_only(1) as the final pragma. extra holds
// WithPragma additions, applied after the base set (later pragmas win) and before
// query_only on the reader.
func buildDSN(cfg Config, extra []pragma, memory bool, memName string, write bool) string {
	var pragmas []pragma
	if write && !memory && cfg.JournalMode != "" {
		pragmas = append(pragmas, pragma{"journal_mode", strings.ToUpper(cfg.JournalMode)})
	}
	pragmas = append(pragmas, basePragmas(cfg)...)
	pragmas = append(pragmas, extra...)
	if !write {
		pragmas = append(pragmas, pragma{"query_only", "1"}) // must remain last
	}

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
			// Paths starting with // are ambiguous with file:// authority separator
			// Use three slashes to ensure proper parsing
			base = "file:///" + pathEncoded[1:]
		} else {
			base = "file:" + pathEncoded
		}
	}
	return base + "?" + strings.Join(params, "&")
}

// pathToURI renders an OS file path as the path portion of a file: URI, percent-
// encoding reserved characters (space, ?, #, …) so the DSN stays parseable.
func pathToURI(p string) string {
	u := url.URL{Path: p}
	return u.String()
}
