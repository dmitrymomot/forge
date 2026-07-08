package useragent

import (
	"bytes"
	"strings"
)

// input is the shared matcher state: an ASCII-lowercased copy of the raw
// User-Agent used for matching. The original string is threaded separately
// as a raw parameter to the functions that extract substrings from it —
// bundling it here would taint this type's escape analysis (Go's escape
// analysis is field-insensitive) and force the lowercase buffer to heap.
type input struct {
	lower []byte
}

// newInput lowercases raw into buf (heap fallback when raw exceeds cap).
func newInput(raw string, buf []byte) input {
	b := buf
	if cap(b) < len(raw) {
		b = make([]byte, 0, len(raw))
	}
	for i := range len(raw) {
		c := raw[i]
		if c >= 'A' && c <= 'Z' {
			c |= 0x20
		}
		b = append(b, c)
	}
	return input{lower: b}
}

// index returns the offset of the lowercase needle sub in the lowered UA,
// or -1. Alloc-free: string(b) == s comparisons do not allocate.
func (in input) index(sub string) int {
	n := len(sub)
	if n == 0 || n > len(in.lower) {
		return -1
	}
	off := 0
	h := in.lower
	for {
		i := bytes.IndexByte(h, sub[0])
		if i < 0 || i+n > len(h) {
			return -1
		}
		if string(h[i:i+n]) == sub {
			return off + i
		}
		h = h[i+1:]
		off += i + 1
	}
}

func (in input) contains(sub string) bool { return in.index(sub) >= 0 }

// versionAfter reads the dotted (or underscored, iOS-style) version that
// follows the first occurrence of tok in raw: versionAfter(raw, "chrome/")
// on "... Chrome/138.0.0.0 ..." yields 138.0.0.0.
func (in input) versionAfter(raw, tok string) Version {
	i := in.index(tok)
	if i < 0 {
		return Version{}
	}
	return versionAt(raw, i+len(tok))
}

func versionAt(s string, start int) Version {
	end := start
	underscored := false
	for end < len(s) {
		c := s[end]
		switch {
		case c >= '0' && c <= '9' || c == '.':
			end++
		case c == '_':
			underscored = true
			end++
		default:
			goto scanned
		}
	}
scanned:
	for end > start && (s[end-1] == '.' || s[end-1] == '_') {
		end--
	}
	if end == start {
		return Version{}
	}
	full := s[start:end]
	if underscored {
		full = strings.ReplaceAll(full, "_", ".") // iOS "16_6" → "16.6"; allocates only on this path
	}
	return parseVersion(full)
}

// parseVersion fills Major/Minor/Patch from the leading numeric segments;
// segments beyond the third survive only in Full.
func parseVersion(full string) Version {
	v := Version{Full: full}
	part, n := 0, 0
	seen := false
	for i := 0; i <= len(full); i++ {
		if i < len(full) {
			if c := full[i]; c >= '0' && c <= '9' {
				if n < 1<<27 { // clamp: garbage like 40 digits must not overflow a 32-bit int (n*10+digit stays within range)
					n = n*10 + int(c-'0')
				}
				seen = true
				continue
			}
		}
		if seen {
			switch part {
			case 0:
				v.Major = n
			case 1:
				v.Minor = n
			case 2:
				v.Patch = n
			}
		}
		part++
		n = 0
		seen = false
		if part > 2 {
			break
		}
	}
	return v
}
