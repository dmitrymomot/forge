package id

const (
	crockford      = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	crockfordLower = "0123456789abcdefghjkmnpqrstvwxyz"
)

// crockfordDec maps an input byte to its 5-bit value, or -1 when invalid. Both
// letter cases are accepted, and Crockford's ambiguous characters are aliased:
// I/i/L/l -> 1 and O/o -> 0.
var crockfordDec = buildCrockfordDec()

func buildCrockfordDec() [256]int8 {
	var t [256]int8
	for i := range t {
		t[i] = -1
	}
	for i := range len(crockford) {
		t[crockford[i]] = int8(i)
		t[crockfordLower[i]] = int8(i)
	}
	t['O'], t['o'] = 0, 0
	t['I'], t['i'] = 1, 1
	t['L'], t['l'] = 1, 1
	return t
}

// encodeBase32 writes the big-endian, MSB-first (left-padded) Crockford base32 of
// src into dst using the alpha alphabet. len(dst) must equal ceil(len(src)*8/5).
func encodeBase32(dst, src []byte, alpha string) {
	pad := uint((5 - (len(src)*8)%5) % 5) // zero pad bits on the MSB side
	var buf uint
	nb := pad
	si := 0
	for di := range dst {
		for nb < 5 {
			buf = buf<<8 | uint(src[si])
			si++
			nb += 8
		}
		nb -= 5
		dst[di] = alpha[(buf>>nb)&0x1f]
	}
}

// decodeBase32 fills dst from the Crockford base32 string s, inverting
// encodeBase32. It returns false if s contains an invalid character or if the
// leading padding bits are non-zero (an out-of-range value). len(s) must equal
// ceil(len(dst)*8/5); callers validate the length first.
func decodeBase32(dst []byte, s string) bool {
	pad := uint((5 - (len(dst)*8)%5) % 5)
	var buf uint
	nb := 0
	di := 0
	for i := range len(s) {
		v := crockfordDec[s[i]]
		if v < 0 {
			return false
		}
		if i == 0 && pad > 0 {
			if uint(v)>>(5-pad) != 0 {
				return false // top pad bits must be zero; otherwise it overflows dst
			}
			buf = uint(v)
			nb = int(5 - pad)
		} else {
			buf = buf<<5 | uint(v)
			nb += 5
		}
		for nb >= 8 {
			nb -= 8
			dst[di] = byte(buf >> nb)
			di++
		}
	}
	return true
}
