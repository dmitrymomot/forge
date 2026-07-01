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

// Bech32 requires a valid BIP-173 bech32 string (checksum verified).
func Bech32(s string) Violation {
	if _, ok := bech32Decode(s); !ok {
		return Violation{Key: "validation.bech32"}
	}
	return Violation{}
}

// BTCAddress accepts Base58Check P2PKH (0x00) / P2SH (0x05) or bech32 (hrp "bc").
func BTCAddress(s string) Violation {
	if p, ok := base58CheckDecode(s); ok && len(p) == 21 && (p[0] == 0x00 || p[0] == 0x05) {
		return Violation{}
	}
	if hrp, ok := bech32Decode(s); ok && hrp == "bc" {
		return Violation{}
	}
	return Violation{Key: "validation.btc_address"}
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
