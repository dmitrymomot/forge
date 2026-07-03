package validate

import (
	"regexp"
	"strings"
)

var reETHAddress = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)

// Base58 requires all characters to be in the Bitcoin Base58 alphabet (no checksum).
func Base58(s string) Violation {
	if s == "" {
		return Violation{Key: "validation.base58"}
	}
	for _, r := range s {
		if !strings.ContainsRune(base58Alphabet, r) {
			return Violation{Key: "validation.base58"}
		}
	}
	return Violation{}
}

// Bech32 requires a valid bech32-family string with a verified checksum: either
// BIP-173 bech32 (segwit v0) or BIP-350 bech32m (segwit v1+/taproot).
func Bech32(s string) Violation {
	if _, enc := bech32Decode(s); enc == bech32None {
		return Violation{Key: "validation.bech32"}
	}
	return Violation{}
}

// BTCAddress accepts Base58Check P2PKH (0x00) / P2SH (0x05), or a segwit address
// under the "bc" HRP. Per BIP-350 the witness version selects the checksum: v0
// (data prefix 'q') uses bech32, v1+ (e.g. taproot "bc1p…") uses bech32m.
func BTCAddress(s string) Violation {
	if p, ok := base58CheckDecode(s); ok && len(p) == 21 && (p[0] == 0x00 || p[0] == 0x05) {
		return Violation{}
	}
	if hrp, enc := bech32Decode(s); enc != bech32None && hrp == "bc" && btcSegwitEncodingOK(s, enc) {
		return Violation{}
	}
	return Violation{Key: "validation.btc_address"}
}

// btcSegwitEncodingOK reports whether a decoded "bc" segwit address uses the
// checksum required for its witness version: witness v0 => bech32, v1+ => bech32m.
// The witness version is the first data character after the "bc1" separator
// ('q' == 0 in the bech32 charset); everything else is treated as v1+.
func btcSegwitEncodingOK(s string, enc bech32Encoding) bool {
	// s is guaranteed len >= 8 with a "1" separator by bech32Decode; the char at
	// index 3 is the first data symbol (HRP "bc" + separator "1").
	if strings.EqualFold(s[3:4], "q") {
		return enc == bech32Bech32
	}
	return enc == bech32Bech32m
}

// TronAddress accepts a Base58Check address with the 0x41 version byte (TRC-20
// token contracts share the TRON account address format).
func TronAddress(s string) Violation {
	if p, ok := base58CheckDecode(s); ok && len(p) == 21 && p[0] == 0x41 {
		return Violation{}
	}
	return Violation{Key: "validation.tron_address"}
}

// SolanaAddress accepts a Base58 string decoding to exactly 32 bytes (Ed25519 key).
func SolanaAddress(s string) Violation {
	if b, ok := base58Decode(s); ok && len(b) == 32 {
		return Violation{}
	}
	return Violation{Key: "validation.solana_address"}
}

// ETHAddress accepts a structural 0x + 40 hex address (EIP-55 checksum out of scope).
func ETHAddress(s string) Violation {
	if !reETHAddress.MatchString(s) {
		return Violation{Key: "validation.eth_address"}
	}
	return Violation{}
}
