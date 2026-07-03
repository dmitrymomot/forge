package id

import "time"

// putMillis writes the low 48 bits of ms as a big-endian value into dst[0:6].
func putMillis(dst []byte, ms uint64) {
	dst[0] = byte(ms >> 40)
	dst[1] = byte(ms >> 32)
	dst[2] = byte(ms >> 24)
	dst[3] = byte(ms >> 16)
	dst[4] = byte(ms >> 8)
	dst[5] = byte(ms)
}

// millis reads a 48-bit big-endian value from src[0:6].
func millis(src []byte) uint64 {
	return uint64(src[0])<<40 | uint64(src[1])<<32 | uint64(src[2])<<24 |
		uint64(src[3])<<16 | uint64(src[4])<<8 | uint64(src[5])
}

// timeFromMillis converts a Unix-millisecond value to a UTC time.Time.
func timeFromMillis(ms uint64) time.Time {
	return time.UnixMilli(int64(ms)).UTC()
}
