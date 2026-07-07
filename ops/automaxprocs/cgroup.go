package automaxprocs

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// unlimited treats cgroup v1's PAGE_COUNTER_MAX-ish sentinels as "no limit".
const unlimited = int64(1) << 62

// cpuQuota returns cores = quota/period from the cgroup under root, or ok=false
// when unlimited or unreadable. cgroup v2 "cpu.max" is "<quota> <period>" or
// "max <period>"; v1 splits across cpu.cfs_quota_us / cpu.cfs_period_us. The
// file reads stay thin so the pure parser (parseCPUMax) carries the logic;
// full-path coverage rides Set(WithCgroupRoot(tmp)) against fixture files.
func cpuQuota(root string) (cores float64, ok bool) {
	if b, err := os.ReadFile(filepath.Join(root, "cpu.max")); err == nil { // v2
		return parseCPUMax(string(b))
	}
	q, e1 := readInt(filepath.Join(root, "cpu", "cpu.cfs_quota_us")) // v1
	p, e2 := readInt(filepath.Join(root, "cpu", "cpu.cfs_period_us"))
	if e1 == nil && e2 == nil && q > 0 && p > 0 {
		return float64(q) / float64(p), true
	}
	return 0, false
}

// parseCPUMax parses a v2 "cpu.max" line into cores. Pure, hence tested directly.
func parseCPUMax(s string) (cores float64, ok bool) {
	f := strings.Fields(strings.TrimSpace(s))
	if len(f) != 2 || f[0] == "max" {
		return 0, false
	}
	q, e1 := strconv.ParseFloat(f[0], 64)
	p, e2 := strconv.ParseFloat(f[1], 64)
	if e1 == nil && e2 == nil && q > 0 && p > 0 {
		return q / p, true
	}
	return 0, false
}

// memoryLimit returns the cgroup memory limit in bytes, or ok=false when
// unlimited or unreadable. v2 "memory.max" is a number or "max"; v1 is
// memory.limit_in_bytes with a huge sentinel meaning unlimited.
func memoryLimit(root string) (bytes int64, ok bool) {
	if b, err := os.ReadFile(filepath.Join(root, "memory.max")); err == nil { // v2
		return parseMemMax(string(b))
	}
	if n, err := readInt(filepath.Join(root, "memory", "memory.limit_in_bytes")); err == nil { // v1
		return validMem(n)
	}
	return 0, false
}

// parseMemMax parses a v2 "memory.max" value ("max" or a byte count). Pure.
func parseMemMax(s string) (bytes int64, ok bool) {
	s = strings.TrimSpace(s)
	if s == "max" {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return validMem(n)
}

// validMem accepts a positive, non-sentinel byte count.
func validMem(n int64) (int64, bool) {
	if n > 0 && n < unlimited {
		return n, true
	}
	return 0, false
}

func readInt(path string) (int64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
}
