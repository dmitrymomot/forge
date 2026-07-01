package validate_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/validate"
)

func TestBase58Bech32(t *testing.T) {
	assert.True(t, validate.Base58("1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa").IsZero())
	assert.Equal(t, "validation.base58", validate.Base58("0OIl+/").Key) // 0,O,I,l not in alphabet
	assert.Equal(t, "validation.base58", validate.Base58("").Key)

	// BIP-173 canonical example address (valid bech32).
	assert.True(t, validate.Bech32("bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4").IsZero())
	assert.Equal(t, "validation.bech32", validate.Bech32("bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t5").Key) // bad checksum
}

func TestBTCAddress(t *testing.T) {
	assert.True(t, validate.BTCAddress("1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa").IsZero())         // P2PKH (genesis)
	assert.True(t, validate.BTCAddress("3J98t1WpEZ73CNmQviecrnyiWrnqRhWNLy").IsZero())         // P2SH
	assert.True(t, validate.BTCAddress("bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4").IsZero()) // bech32 segwit v0
	assert.Equal(t, "validation.btc_address", validate.BTCAddress("1A1zP1eP5QGefi2DMPTfTL5SLmv7Divfna").Key) // bad checksum
}

func TestTronSolanaEth(t *testing.T) {
	assert.True(t, validate.TronAddress("TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t").IsZero()) // public TRON address
	assert.Equal(t, "validation.tron_address", validate.TronAddress("TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj60").Key)

	assert.True(t, validate.SolanaAddress("11111111111111111111111111111111").IsZero())             // System Program (32 bytes)
	assert.True(t, validate.SolanaAddress("So11111111111111111111111111111111111111112").IsZero()) // Wrapped SOL mint
	assert.Equal(t, "validation.solana_address", validate.SolanaAddress("tooShort").Key)

	assert.True(t, validate.ETHAddress("0x52908400098527886E0F7030069857D2E4169EE7").IsZero())
	assert.Equal(t, "validation.eth_address", validate.ETHAddress("0x123").Key)
	assert.Equal(t, "validation.eth_address", validate.ETHAddress("52908400098527886E0F7030069857D2E4169EE7").Key) // no 0x
}
