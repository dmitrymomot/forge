package automaxprocs

import (
	"log/slog"
	"os"
	"runtime"
	"runtime/debug"
)

type config struct {
	cgroupRoot  string
	memHeadroom float64
	minProcs    int
	setCPU      bool
	setMemory   bool
}

// Option configures Set.
type Option func(*config)

// WithMemoryHeadroom sets the fraction of the cgroup memory limit used for
// GOMEMLIMIT (default 0.9); the remainder covers stacks, the runtime, and OS
// overhead. Values outside (0,1] are ignored.
func WithMemoryHeadroom(f float64) Option {
	return func(c *config) {
		if f > 0 && f <= 1 {
			c.memHeadroom = f
		}
	}
}

// WithMinProcs floors GOMAXPROCS (default 1); a computed quota below it is
// raised to it. Non-positive ignored.
func WithMinProcs(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.minProcs = n
		}
	}
}

// WithCPU toggles the GOMAXPROCS leg (default true).
func WithCPU(on bool) Option { return func(c *config) { c.setCPU = on } }

// WithMemory toggles the GOMEMLIMIT leg (default true).
func WithMemory(on bool) Option { return func(c *config) { c.setMemory = on } }

// WithCgroupRoot overrides the cgroup mount point (default "/sys/fs/cgroup").
// Its purpose is twofold: real systems that mount the cgroup hierarchy
// elsewhere, and black-box tests, which point it at a temp dir of fixture
// files. An empty dir is ignored.
func WithCgroupRoot(dir string) Option {
	return func(c *config) {
		if dir != "" {
			c.cgroupRoot = dir
		}
	}
}

// Set tunes GOMAXPROCS and GOMEMLIMIT from the process's cgroup CPU and memory
// limits, logging every decision at Info (and no-ops at Debug). It is
// fail-open: a missing/unparseable cgroup, or a non-Linux host, leaves the
// runtime defaults untouched with a logged no-op. An explicit GOMAXPROCS or
// GOMEMLIMIT environment variable is honored — that leg is skipped so the
// operator's choice wins. The returned undo restores the prior process values
// (for tests or staged shutdown) and is safe to call once.
func Set(log *slog.Logger, opts ...Option) (undo func()) {
	c := config{memHeadroom: 0.9, minProcs: 1, setCPU: true, setMemory: true, cgroupRoot: "/sys/fs/cgroup"}
	for _, o := range opts {
		o(&c)
	}

	prevProcs := runtime.GOMAXPROCS(0)
	prevMem := debug.SetMemoryLimit(-1) // negative reads without setting
	undo = func() {
		runtime.GOMAXPROCS(prevProcs)
		debug.SetMemoryLimit(prevMem)
	}

	if c.setCPU {
		applyCPU(log, c)
	}
	if c.setMemory {
		applyMemory(log, c)
	}
	return undo
}

func applyCPU(log *slog.Logger, c config) {
	if v, ok := os.LookupEnv("GOMAXPROCS"); ok && v != "" {
		log.Debug("automaxprocs: GOMAXPROCS set in env, leaving as-is", slog.String("value", v))
		return
	}
	quota, ok := cpuQuota(c.cgroupRoot)
	if !ok {
		log.Debug("automaxprocs: no cgroup CPU quota, leaving GOMAXPROCS",
			slog.Int("gomaxprocs", runtime.GOMAXPROCS(0)))
		return
	}
	procs := max(int(quota), c.minProcs) // floor at minProcs
	runtime.GOMAXPROCS(procs)
	log.Info("automaxprocs: set GOMAXPROCS from cgroup CPU quota",
		slog.Int("gomaxprocs", procs), slog.Float64("quota", quota))
}

func applyMemory(log *slog.Logger, c config) {
	if v, ok := os.LookupEnv("GOMEMLIMIT"); ok && v != "" {
		log.Debug("automaxprocs: GOMEMLIMIT set in env, leaving as-is", slog.String("value", v))
		return
	}
	limit, ok := memoryLimit(c.cgroupRoot)
	if !ok {
		log.Debug("automaxprocs: no cgroup memory limit, leaving GOMEMLIMIT")
		return
	}
	target := int64(float64(limit) * c.memHeadroom)
	if target <= 0 {
		return
	}
	debug.SetMemoryLimit(target)
	log.Info("automaxprocs: set GOMEMLIMIT from cgroup memory limit",
		slog.Int64("gomemlimit_bytes", target),
		slog.Int64("cgroup_limit_bytes", limit),
		slog.Float64("headroom", c.memHeadroom))
}
