package validate

import "strings"

const bech32Charset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"

func bech32Polymod(values []int) int {
	gen := []int{0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3}
	chk := 1
	for _, v := range values {
		b := chk >> 25
		chk = (chk&0x1ffffff)<<5 ^ v
		for i := 0; i < 5; i++ {
			if (b>>i)&1 == 1 {
				chk ^= gen[i]
			}
		}
	}
	return chk
}

func bech32HrpExpand(hrp string) []int {
	out := make([]int, 0, len(hrp)*2+1)
	for _, c := range hrp {
		out = append(out, int(c)>>5)
	}
	out = append(out, 0)
	for _, c := range hrp {
		out = append(out, int(c)&31)
	}
	return out
}

// bech32Decode validates a BIP-173 bech32 string (checksum constant 1) and returns
// its human-readable part. Rejects mixed case and out-of-range length.
func bech32Decode(s string) (string, bool) {
	if len(s) < 8 || len(s) > 90 {
		return "", false
	}
	if lower, upper := strings.ToLower(s), strings.ToUpper(s); s != lower && s != upper {
		return "", false
	}
	s = strings.ToLower(s)
	pos := strings.LastIndex(s, "1")
	if pos < 1 || pos+7 > len(s) {
		return "", false
	}
	hrp := s[:pos]
	data := make([]int, 0, len(s)-pos-1)
	for _, c := range s[pos+1:] {
		idx := strings.IndexRune(bech32Charset, c)
		if idx < 0 {
			return "", false
		}
		data = append(data, idx)
	}
	if bech32Polymod(append(bech32HrpExpand(hrp), data...)) != 1 {
		return "", false
	}
	return hrp, true
}
