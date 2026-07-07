// Package automaxprocs right-sizes the Go runtime to its cgroup CPU and
// memory limits: GOMAXPROCS from the cgroup CPU quota, GOMEMLIMIT from the
// cgroup memory limit (scaled by a headroom fraction, default 0.9).
//
// It assumes a containerized host: the default cgroup root is
// "/sys/fs/cgroup" (cgroup v2, with a v1 fallback), overridable via
// WithCgroupRoot for non-standard mounts or tests. Outside a container, or
// wherever the cgroup files are missing or unparseable, the corresponding leg
// is simply left alone.
//
// Set is fail-open: any read or parse failure — or an explicit GOMAXPROCS /
// GOMEMLIMIT environment variable, which always wins — is a logged no-op,
// never a startup error. A maxprocs adjustment must never be the reason a
// process fails to start.
package automaxprocs
