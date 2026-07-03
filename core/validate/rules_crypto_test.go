package validate_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/core/validate"
)

func TestBase58Bech32(t *testing.T) {
	assert.True(t, validate.Base58("1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa").IsZero())
	assert.Equal(t, "validation.base58", validate.Base58("0OIl+/").Key) // 0,O,I,l not in alphabet
	assert.Equal(t, "validation.base58", validate.Base58("").Key)

	// BIP-173 canonical example address (valid bech32).
	assert.True(t, validate.Bech32("bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4").IsZero())
	assert.Equal(t, "validation.bech32", validate.Bech32("bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t5").Key) // bad checksum

	// BIP-173 invalid vectors: HRP characters must be ASCII in [33,126].
	assert.Equal(t, "validation.bech32", validate.Bech32(" 1nwldj5").Key)    // HRP char 0x20 (space) < 0x21
	assert.Equal(t, "validation.bech32", validate.Bech32("\x7f1axkwrx").Key) // HRP char 0x7f > 0x7e
}

func TestBech32m(t *testing.T) {
	// BIP-350 valid bech32m vectors (checksum constant 0x2bc830a3) must PASS Bech32.
	assert.True(t, validate.Bech32("A1LQFN3A").IsZero())
	assert.True(t, validate.Bech32("a1lqfn3a").IsZero())
	assert.True(t, validate.Bech32("abcdef1l7aum6echk45nj3s0wdvt2fg8x9yrzpqzd3ryx").IsZero())
	assert.True(t, validate.Bech32("?1v759aa").IsZero())
	// Real taproot (segwit v1) address encoded with bech32m.
	assert.True(t, validate.Bech32("bc1p0xlxvlhemja6c4dqv22uapctqupfhlxm9h8z3k2e72q4k9hcz7vqzk5jj0").IsZero())

	// Valid bech32 (segwit v0, checksum constant 1) still passes alongside bech32m.
	assert.True(t, validate.Bech32("bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4").IsZero())

	// Corrupting a bech32m checksum still fails (last char q -> p).
	assert.Equal(t, "validation.bech32", validate.Bech32("A1LQFN3P").Key)

	// Taproot address is a valid BTC address (BIP-350: witness v1 uses bech32m).
	assert.True(t, validate.BTCAddress("bc1p0xlxvlhemja6c4dqv22uapctqupfhlxm9h8z3k2e72q4k9hcz7vqzk5jj0").IsZero())
}

func TestBech32DecodeBranches(t *testing.T) {
	// Length guards: under 8 and over 90 characters are rejected up front.
	assert.Equal(t, "validation.bech32", validate.Bech32("bc1qqqq").Key)                     // too short (<8)
	assert.Equal(t, "validation.bech32", validate.Bech32("bc1"+strings.Repeat("q", 90)).Key) // too long (>90)

	// No separator '1' at all → LastIndex returns -1 (pos < 1).
	assert.Equal(t, "validation.bech32", validate.Bech32("bcqqqqqqqq").Key)
	// Separator present but empty HRP ('1' at position 0 → pos < 1).
	assert.Equal(t, "validation.bech32", validate.Bech32("1qqqqqqqq").Key)
	// Separator too close to the end: fewer than 6 data + checksum chars (pos+7 > len).
	assert.Equal(t, "validation.bech32", validate.Bech32("bcdefgh1qq").Key)

	// Data part carries a char outside the bech32 charset ('b' and 'i' are excluded).
	assert.Equal(t, "validation.bech32", validate.Bech32("bc1bbbbbbbbbb").Key)

	// Mixed case is rejected (neither all-lower nor all-upper).
	assert.Equal(t, "validation.bech32", validate.Bech32("Bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4").Key)

	// Uppercase form of a valid address is accepted (BIP-173 allows all-upper);
	// the decoder lowercases before verifying the polymod checksum constant 1.
	assert.True(t, validate.Bech32("BC1QW508D6QEJXTDG4Y5R3ZARVARY0C5XW7KV8F3T4").IsZero())
}

func TestBTCAddress(t *testing.T) {
	assert.True(t, validate.BTCAddress("1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa").IsZero())                       // P2PKH (genesis)
	assert.True(t, validate.BTCAddress("3J98t1WpEZ73CNmQviecrnyiWrnqRhWNLy").IsZero())                       // P2SH
	assert.True(t, validate.BTCAddress("bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4").IsZero())               // bech32 segwit v0
	assert.Equal(t, "validation.btc_address", validate.BTCAddress("1A1zP1eP5QGefi2DMPTfTL5SLmv7Divfna").Key) // bad checksum
}

func TestTronSolanaEth(t *testing.T) {
	assert.True(t, validate.TronAddress("TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t").IsZero()) // public TRON address
	assert.Equal(t, "validation.tron_address", validate.TronAddress("TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj60").Key)

	assert.True(t, validate.SolanaAddress("11111111111111111111111111111111").IsZero())            // System Program (32 bytes)
	assert.True(t, validate.SolanaAddress("So11111111111111111111111111111111111111112").IsZero()) // Wrapped SOL mint
	assert.Equal(t, "validation.solana_address", validate.SolanaAddress("tooShort").Key)

	assert.True(t, validate.ETHAddress("0x52908400098527886E0F7030069857D2E4169EE7").IsZero())
	assert.Equal(t, "validation.eth_address", validate.ETHAddress("0x123").Key)
	assert.Equal(t, "validation.eth_address", validate.ETHAddress("52908400098527886E0F7030069857D2E4169EE7").Key) // no 0x
}
