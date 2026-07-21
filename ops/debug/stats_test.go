package debug_test

import (
	"runtime"
	"testing"

	"github.com/dmitrymomot/forge/ops/debug"
)

func TestSnapshot(t *testing.T) {
	t.Parallel()
	runtime.GC()
	s := debug.Snapshot()

	if s.GoVersion != runtime.Version() {
		t.Errorf("GoVersion = %q, want %q", s.GoVersion, runtime.Version())
	}
	if s.Goroutines <= 0 {
		t.Errorf("Goroutines = %d, want > 0", s.Goroutines)
	}
	if s.GOMAXPROCS <= 0 || s.NumCPU <= 0 {
		t.Errorf("GOMAXPROCS = %d, NumCPU = %d, want > 0", s.GOMAXPROCS, s.NumCPU)
	}
	if s.UptimeSeconds < 0 {
		t.Errorf("UptimeSeconds = %f, want >= 0", s.UptimeSeconds)
	}
	if s.Mem.Sys == 0 || s.Mem.HeapAlloc == 0 || s.Mem.Mallocs == 0 {
		t.Errorf("implausible mem stats: %+v", s.Mem)
	}
	if s.GC.NumGC == 0 {
		t.Errorf("GC.NumGC = 0 after runtime.GC()")
	}
	if s.GC.LastGC.IsZero() {
		t.Errorf("GC.LastGC is zero after runtime.GC()")
	}
	if s.GC.NextGC == 0 {
		t.Errorf("GC.NextGC = 0, want > 0")
	}
}
