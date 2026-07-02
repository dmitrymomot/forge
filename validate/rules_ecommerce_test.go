package validate_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/validate"
)

func TestCreditCardCVVExpiry(t *testing.T) {
	// 4242 4242 4242 4242 is a well-known Luhn-valid test PAN.
	assert.True(t, validate.CreditCard("4242 4242 4242 4242").IsZero())
	// 5555-5555-5555-4444 (Mastercard test PAN): the doubled 5s exceed 9, exercising
	// the Luhn digit-sum "d -= 9" overflow branch.
	assert.True(t, validate.CreditCard("5555-5555-5555-4444").IsZero())
	assert.Equal(t, "validation.credit_card", validate.CreditCard("4242 4242 4242 4241").Key) // bad checksum
	assert.Equal(t, "validation.credit_card", validate.CreditCard("12345").Key)               // too short

	assert.True(t, validate.CVV("123").IsZero())
	assert.True(t, validate.CVV("1234").IsZero())
	assert.Equal(t, "validation.cvv", validate.CVV("12").Key)
	assert.Equal(t, "validation.cvv", validate.CVV("12a").Key)
	assert.Equal(t, "validation.cvv", validate.CVV("").Key)                // empty → digitsOnly false
	assert.Equal(t, "validation.credit_card", validate.CreditCard("").Key) // empty → digitsOnly false

	assert.True(t, validate.CardExpiry("12/99").IsZero())                       // Dec 2099, future
	assert.Equal(t, "validation.card_expiry", validate.CardExpiry("01/00").Key) // Jan 2000, past
	assert.Equal(t, "validation.card_expiry", validate.CardExpiry("13/30").Key) // bad month
	assert.Equal(t, "validation.card_expiry", validate.CardExpiry("2030-01").Key)
}

func TestCurrencyCountryCode(t *testing.T) {
	assert.True(t, validate.CurrencyCode("USD").IsZero())
	assert.True(t, validate.CurrencyCode("EUR").IsZero())
	assert.Equal(t, "validation.currency_code", validate.CurrencyCode("usd").Key) // wrong case
	assert.Equal(t, "validation.currency_code", validate.CurrencyCode("XYZ").Key)

	assert.True(t, validate.CountryCode("US").IsZero())
	assert.True(t, validate.CountryCode("GB").IsZero())
	assert.Equal(t, "validation.country_code", validate.CountryCode("ZZ").Key)
}

func TestEANISBN(t *testing.T) {
	assert.True(t, validate.EAN("4006381333931").IsZero()) // valid EAN-13
	assert.Equal(t, "validation.ean", validate.EAN("4006381333932").Key)
	assert.True(t, validate.EAN("40170725").IsZero())                    // valid EAN-8
	assert.Equal(t, "validation.ean", validate.EAN("40170724").Key)      // bad EAN-8 check
	assert.Equal(t, "validation.ean", validate.EAN("12345").Key)         // wrong length
	assert.Equal(t, "validation.ean", validate.EAN("400638133393a").Key) // non-digit

	assert.True(t, validate.ISBN("0306406152").IsZero())    // valid ISBN-10
	assert.True(t, validate.ISBN("9780306406157").IsZero()) // valid ISBN-13
	assert.True(t, validate.ISBN("080442957X").IsZero())    // ISBN-10 with uppercase X check digit
	assert.True(t, validate.ISBN("080442957x").IsZero())    // lowercase x check digit also accepted
	// Hyphen/space separators are stripped before validation.
	assert.True(t, validate.ISBN("0-306-40615-2").IsZero())
	assert.True(t, validate.ISBN("978 0 306 40615 7").IsZero())

	assert.Equal(t, "validation.isbn", validate.ISBN("0306406153").Key)    // bad ISBN-10 checksum
	assert.Equal(t, "validation.isbn", validate.ISBN("03064061a2").Key)    // non-digit within the first 9 chars
	assert.Equal(t, "validation.isbn", validate.ISBN("030640615Y").Key)    // invalid check-digit char (not 0-9/X)
	assert.Equal(t, "validation.isbn", validate.ISBN("9780306406158").Key) // bad ISBN-13 checksum
	assert.Equal(t, "validation.isbn", validate.ISBN("123").Key)           // wrong length entirely
}
