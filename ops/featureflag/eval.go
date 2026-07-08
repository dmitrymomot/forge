package featureflag

import "slices"

// matches reports whether any token in list equals the subject id (when
// non-empty) or any extra identity token. No allocations.
func matches(list []string, id string, extra []string) bool {
	for _, t := range list {
		if id != "" && t == id {
			return true
		}
		if slices.Contains(extra, t) {
			return true
		}
	}
	return false
}

// bucket maps (flag key, subject id) to [0,100) via FNV-1a 64 with a NUL
// separator, inlined to stay allocation-free on the hot path. Deterministic
// across processes; including the key decorrelates buckets across flags.
func bucket(key, id string) int {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for i := range len(key) {
		h ^= uint64(key[i])
		h *= prime64
	}
	h *= prime64 // NUL separator: h ^= 0 is a no-op, the multiply still mixes
	for i := range len(id) {
		h ^= uint64(id[i])
		h *= prime64
	}
	return int(h % 100)
}

// eval runs the data pipeline for an enabled flag:
// deny → allow → rollout. Returns the value or a miss.
func eval(f Flag, key, id string, extra []string) (string, bool) {
	if matches(f.Deny, id, extra) {
		return "", false
	}
	if matches(f.Allow, id, extra) {
		return f.Value, true
	}
	if f.Rollout >= 100 {
		return f.Value, true
	}
	if f.Rollout <= 0 || id == "" {
		return "", false
	}
	if bucket(key, id) < f.Rollout {
		return f.Value, true
	}
	return "", false
}
