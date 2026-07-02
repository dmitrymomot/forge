package validate

import "strings"

const bech32Charset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"

// bech32Encoding reports which checksum constant a string matched.
type bech32Encoding int

const (
	bech32None    bech32Encoding = iota // checksum invalid under both constants
	bech32Bech32                        // BIP-173 bech32, polymod constant 1
	bech32Bech32m                       // BIP-350 bech32m, polymod constant 0x2bc830a3
)

// bech32mConst is the BIP-350 bech32m checksum constant (segwit v1+/taproot).
const bech32mConst = 0x2bc830a3

func bech32Polymod(values []int) int {
	gen := []int{0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3}
	chk := 1
	for _, v := range values {
		b := chk >> 25
		chk = (chk&0x1ffffff)<<5 ^ v
		for i := range 5 {
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

// bech32Decode validates a bech32-family string and returns its human-readable
// part along with the encoding that matched: bech32Bech32 (BIP-173, polymod 1),
// bech32Bech32m (BIP-350, polymod 0x2bc830a3), or bech32None when neither matches
// or the string is otherwise malformed. Rejects mixed case and out-of-range length.
// Callers apply their own policy: Bech32 accepts either encoding, while BTCAddress
// enforces the BIP-350 per-witness-version rule (v0 => bech32, v1+ => bech32m).
func bech32Decode(s string) (string, bech32Encoding) {
	if len(s) < 8 || len(s) > 90 {
		return "", bech32None
	}
	if lower, upper := strings.ToLower(s), strings.ToUpper(s); s != lower && s != upper {
		return "", bech32None
	}
	s = strings.ToLower(s)
	pos := strings.LastIndex(s, "1")
	if pos < 1 || pos+7 > len(s) {
		return "", bech32None
	}
	hrp := s[:pos]
	// BIP-173: every HRP character must be ASCII in the range [33,126] (0x21..0x7e).
	for i := range len(hrp) {
		if hrp[i] < 33 || hrp[i] > 126 {
			return "", bech32None
		}
	}
	data := make([]int, 0, len(s)-pos-1)
	for _, c := range s[pos+1:] {
		idx := strings.IndexRune(bech32Charset, c)
		if idx < 0 {
			return "", bech32None
		}
		data = append(data, idx)
	}
	switch bech32Polymod(append(bech32HrpExpand(hrp), data...)) {
	case 1:
		return hrp, bech32Bech32
	case bech32mConst:
		return hrp, bech32Bech32m
	default:
		return "", bech32None
	}
}
