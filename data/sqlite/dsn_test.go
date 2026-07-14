package sqlite

import (
	"net/url"
	"strings"
	"testing"
)

func TestBuildDSN_WriterHasImmediateWALNoQueryOnly(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Path = "app.db"
	dsn := buildDSN(cfg, nil, false, "", true)

	for _, want := range []string{
		"file:app.db?",
		"_txlock=immediate",
		"_pragma=journal_mode(WAL)",
		"_pragma=busy_timeout(5000)",
		"_pragma=synchronous(NORMAL)",
		"_pragma=foreign_keys(1)",
		"_pragma=temp_store(MEMORY)",
	} {
		if !strings.Contains(dsn, want) {
			t.Errorf("writer DSN missing %q\n got: %s", want, dsn)
		}
	}
	if strings.Contains(dsn, "query_only") {
		t.Errorf("writer DSN must not set query_only: %s", dsn)
	}
}

func TestBuildDSN_ReaderIsQueryOnlyDeferredNoJournalMode(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Path = "app.db"
	dsn := buildDSN(cfg, nil, false, "", false)

	if !strings.Contains(dsn, "_txlock=deferred") {
		t.Errorf("reader DSN must be deferred: %s", dsn)
	}
	if strings.Contains(dsn, "journal_mode") {
		t.Errorf("reader DSN must not set journal_mode: %s", dsn)
	}
	// query_only must be the LAST _pragma entry.
	last := strings.LastIndex(dsn, "_pragma=")
	if !strings.HasPrefix(dsn[last:], "_pragma=query_only(1)") {
		t.Errorf("query_only must be the final pragma: %s", dsn)
	}
}

func TestBuildDSN_MemorySkipsWALUsesSharedCache(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Path = ":memory:"
	dsn := buildDSN(cfg, nil, true, "memdb-7", true)

	for _, want := range []string{"file:memdb-7", "mode=memory", "cache=shared"} {
		if !strings.Contains(dsn, want) {
			t.Errorf("memory DSN missing %q: %s", want, dsn)
		}
	}
	if strings.Contains(dsn, "journal_mode") {
		t.Errorf("memory DSN must not set journal_mode: %s", dsn)
	}
}

func TestBuildDSN_ExtraPragmaOverrideAppendedAfterBase(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Path = "app.db"
	dsn := buildDSN(cfg, []pragma{{"cache_size", "-2000"}}, false, "", true)
	base := strings.Index(dsn, "cache_size(-16000)")
	over := strings.Index(dsn, "cache_size(-2000)")
	if base < 0 || over < 0 || over < base {
		t.Errorf("override cache_size must appear after the default: %s", dsn)
	}
}

func TestPathToURI_EscapesReservedChars(t *testing.T) {
	got := pathToURI("my data/app?.db")
	if strings.ContainsAny(got, " ?") {
		t.Errorf("path not escaped: %q", got)
	}
	if _, err := url.Parse("file:" + got + "?x=1"); err != nil {
		t.Errorf("escaped path not URL-parseable: %v", err)
	}
}

func TestIsMemory(t *testing.T) {
	for _, p := range []string{":memory:", "file:x?mode=memory&cache=shared"} {
		if !isMemory(p) {
			t.Errorf("isMemory(%q) = false, want true", p)
		}
	}
	if isMemory("/var/db/app.db") {
		t.Errorf("isMemory(file path) = true, want false")
	}
}

func TestNextMemName_Unique(t *testing.T) {
	a, b := nextMemName(), nextMemName()
	if a == b {
		t.Errorf("nextMemName not unique: %q == %q", a, b)
	}
}

func FuzzBuildDSN_AlwaysParseable(f *testing.F) {
	f.Add("app.db")
	f.Add("/var/db/my app.db")
	f.Add("weird?#name.db")
	f.Fuzz(func(t *testing.T, path string) {
		if path == "" || isMemory(path) {
			t.Skip()
		}
		cfg := DefaultConfig()
		cfg.Path = path
		dsn := buildDSN(cfg, nil, false, "", true)
		if _, err := url.Parse(dsn); err != nil {
			t.Errorf("unparseable DSN for path %q: %v", path, err)
		}
	})
}
