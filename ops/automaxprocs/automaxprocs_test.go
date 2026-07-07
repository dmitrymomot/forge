package automaxprocs_test

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"testing"

	"github.com/dmitrymomot/forge/ops/automaxprocs"
)

func nopLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// clearRuntimeEnv blanks GOMAXPROCS/GOMEMLIMIT for the test; Set treats "" as
// unset. t.Setenv restores originals on cleanup.
func clearRuntimeEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GOMAXPROCS", "")
	t.Setenv("GOMEMLIMIT", "")
}

func writeV2(t *testing.T, cpuMax, memMax string) string {
	t.Helper()
	root := t.TempDir()
	if cpuMax != "" {
		if err := os.WriteFile(filepath.Join(root, "cpu.max"), []byte(cpuMax), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if memMax != "" {
		if err := os.WriteFile(filepath.Join(root, "memory.max"), []byte(memMax), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestSetV2CPUQuota(t *testing.T) {
	clearRuntimeEnv(t)
	prev := runtime.GOMAXPROCS(0)
	undo := automaxprocs.Set(nopLogger(),
		automaxprocs.WithCgroupRoot(writeV2(t, "300000 100000", "")),
		automaxprocs.WithMemory(false))
	if got := runtime.GOMAXPROCS(0); got != 3 {
		t.Errorf("GOMAXPROCS = %d, want 3", got)
	}
	undo()
	if got := runtime.GOMAXPROCS(0); got != prev {
		t.Errorf("undo left GOMAXPROCS = %d, want %d", got, prev)
	}
}

func TestSetV2CPUFloorsToMinOne(t *testing.T) {
	clearRuntimeEnv(t)
	undo := automaxprocs.Set(nopLogger(),
		automaxprocs.WithCgroupRoot(writeV2(t, "150000 100000", "")), // 1.5 -> floor 1
		automaxprocs.WithMemory(false))
	defer undo()
	if got := runtime.GOMAXPROCS(0); got != 1 {
		t.Errorf("GOMAXPROCS = %d, want 1", got)
	}
}

func TestSetV2CPUUnlimitedLeavesDefault(t *testing.T) {
	clearRuntimeEnv(t)
	prev := runtime.GOMAXPROCS(0)
	undo := automaxprocs.Set(nopLogger(),
		automaxprocs.WithCgroupRoot(writeV2(t, "max 100000", "")),
		automaxprocs.WithMemory(false))
	defer undo()
	if got := runtime.GOMAXPROCS(0); got != prev {
		t.Errorf("GOMAXPROCS = %d, want unchanged %d", got, prev)
	}
}

func TestSetV1CPUQuota(t *testing.T) {
	clearRuntimeEnv(t)
	root := t.TempDir()
	cpuDir := filepath.Join(root, "cpu")
	if err := os.MkdirAll(cpuDir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(cpuDir, "cpu.cfs_quota_us"), []byte("400000"), 0o644)
	os.WriteFile(filepath.Join(cpuDir, "cpu.cfs_period_us"), []byte("100000"), 0o644)
	undo := automaxprocs.Set(nopLogger(), automaxprocs.WithCgroupRoot(root), automaxprocs.WithMemory(false))
	defer undo()
	if got := runtime.GOMAXPROCS(0); got != 4 {
		t.Errorf("v1 GOMAXPROCS = %d, want 4", got)
	}
}

func TestSetV2MemoryHeadroom(t *testing.T) {
	clearRuntimeEnv(t)
	prev := debug.SetMemoryLimit(-1)
	undo := automaxprocs.Set(nopLogger(),
		automaxprocs.WithCgroupRoot(writeV2(t, "", "1000000000")),
		automaxprocs.WithCPU(false),
		automaxprocs.WithMemoryHeadroom(0.9))
	if got := debug.SetMemoryLimit(-1); got != 900000000 {
		t.Errorf("GOMEMLIMIT = %d, want 900000000", got)
	}
	undo()
	if got := debug.SetMemoryLimit(-1); got != prev {
		t.Errorf("undo left GOMEMLIMIT = %d, want %d", got, prev)
	}
}

func TestSetV2MemoryUnlimited(t *testing.T) {
	clearRuntimeEnv(t)
	prev := debug.SetMemoryLimit(-1)
	undo := automaxprocs.Set(nopLogger(),
		automaxprocs.WithCgroupRoot(writeV2(t, "", "max")),
		automaxprocs.WithCPU(false))
	defer undo()
	if got := debug.SetMemoryLimit(-1); got != prev {
		t.Errorf("GOMEMLIMIT = %d, want unchanged %d", got, prev)
	}
}

func TestSetEnvPresentSkipsCPU(t *testing.T) {
	t.Setenv("GOMAXPROCS", "7") // runtime read env at startup; Set must not override
	t.Setenv("GOMEMLIMIT", "")
	prev := runtime.GOMAXPROCS(0)
	undo := automaxprocs.Set(nopLogger(),
		automaxprocs.WithCgroupRoot(writeV2(t, "300000 100000", "")),
		automaxprocs.WithMemory(false))
	defer undo()
	if got := runtime.GOMAXPROCS(0); got != prev {
		t.Errorf("env present must skip CPU leg; GOMAXPROCS = %d, want %d", got, prev)
	}
}

func TestSetMissingRootIsNoOp(t *testing.T) {
	clearRuntimeEnv(t)
	prevP, prevM := runtime.GOMAXPROCS(0), debug.SetMemoryLimit(-1)
	undo := automaxprocs.Set(nopLogger(),
		automaxprocs.WithCgroupRoot(filepath.Join(t.TempDir(), "nonexistent")))
	defer undo()
	if runtime.GOMAXPROCS(0) != prevP || debug.SetMemoryLimit(-1) != prevM {
		t.Error("missing cgroup root should be a no-op")
	}
}
