package encoding

// crockfordAlphabet is Crockford base32: digits 0-9 then A-Z excluding I, L, O,
// U. Index == 5-bit value.
const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// Encode32 encodes b in Crockford base32, MSB-first with leading (most
// significant) zero-bit padding. This is the ULID-canonical layout: 16 bytes ->
// 26 chars, 10 bytes -> 16 chars.
func Encode32(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	nbits := len(b) * 8
	nchars := (nbits + 4) / 5 // ceil(nbits/5)
	pad := nchars*5 - nbits   // padding bits sit at the MSB (front)
	bit := func(pos int) int {
		dpos := pos - pad
		if dpos < 0 {
			return 0
		}
		return int((b[dpos/8] >> uint(7-dpos%8)) & 1)
	}
	out := make([]byte, nchars)
	for i := range out {
		v := 0
		for k := range 5 {
			v = v<<1 | bit(i*5+k)
		}
		out[i] = crockfordAlphabet[v]
	}
	return string(out)
}

// Decode32 reverses Encode32. It is case-insensitive and applies Crockford
// decode aliases (I,i,L,l -> 1; O,o -> 0). Invalid characters (including U)
// return ErrInvalidEncoding.
func Decode32(s string) ([]byte, error) {
	if s == "" {
		return []byte{}, nil
	}
	nbits := len(s) * 5
	nbytes := nbits / 8
	pad := nbits - nbytes*8 // leading pad bits to drop
	out := make([]byte, nbytes)
	pos := 0
	for i := 0; i < len(s); i++ {
		v, ok := decodeCrockfordChar(s[i])
		if !ok {
			return nil, ErrInvalidEncoding
		}
		for k := 4; k >= 0; k-- {
			p := pos
			pos++
			if p < pad {
				continue // drop MSB padding bit
			}
			if (v>>uint(k))&1 == 1 {
				dpos := p - pad
				out[dpos/8] |= 1 << uint(7-dpos%8)
			}
		}
	}
	return out, nil
}

func decodeCrockfordChar(c byte) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case c == 'O' || c == 'o':
		return 0, true
	case c == 'I' || c == 'i' || c == 'L' || c == 'l':
		return 1, true
	}
	uc := c
	if uc >= 'a' && uc <= 'z' {
		uc -= 'a' - 'A'
	}
	for i := range len(crockfordAlphabet) {
		if crockfordAlphabet[i] == uc {
			return i, true
		}
	}
	return 0, false
}
