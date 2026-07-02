package validate_test

import (
	"testing"

	"github.com/dmitrymomot/forge/validate"
)

// Composition machinery: Apply tags failures with the field, Check flattens.

func BenchmarkApply(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = validate.Apply("email", "user@example.com", validate.Required[string], validate.Email)
	}
}

func BenchmarkCheck(b *testing.B) {
	name := validate.Apply("name", "Ada", validate.Required[string], validate.MinLen(2))
	email := validate.Apply("email", "ada@example.com", validate.Email)
	b.ReportAllocs()
	for b.Loop() {
		_ = validate.Check(name, email)
	}
}

// Algebra combinators.

func BenchmarkAnd(b *testing.B) {
	r := validate.And(validate.Required[string], validate.MinLen(2), validate.MaxLen(64))
	b.ReportAllocs()
	for b.Loop() {
		_ = r("hello")
	}
}

func BenchmarkOr(b *testing.B) {
	r := validate.Or("validation.contact", validate.Email, validate.MinLen(6))
	b.ReportAllocs()
	for b.Loop() {
		_ = r("ada@example.com")
	}
}

func BenchmarkNot(b *testing.B) {
	r := validate.Not(validate.Contains("@"), "validation.no_at")
	b.ReportAllocs()
	for b.Loop() {
		_ = r("plain-token")
	}
}

func BenchmarkEach(b *testing.B) {
	r := validate.Each(validate.MinLen(2))
	items := []string{"aa", "bb", "cc", "dd", "ee"}
	b.ReportAllocs()
	for b.Loop() {
		_ = r(items)
	}
}

func BenchmarkWhen(b *testing.B) {
	r := validate.When(true, validate.MinLen(2))
	b.ReportAllocs()
	for b.Loop() {
		_ = r("hello")
	}
}

// String / format rules.

func BenchmarkEmail(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = validate.Email("ada.lovelace@example.com")
	}
}

func BenchmarkMinLen(b *testing.B) {
	r := validate.MinLen(8)
	b.ReportAllocs()
	for b.Loop() {
		_ = r("passphrase")
	}
}

// Numeric rule.

func BenchmarkBetween(b *testing.B) {
	r := validate.Between(1, 100)
	b.ReportAllocs()
	for b.Loop() {
		_ = r(42)
	}
}

// Checksum / crypto validators.

func BenchmarkCreditCard(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = validate.CreditCard("4242 4242 4242 4242")
	}
}

func BenchmarkISBN(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = validate.ISBN("9780306406157")
	}
}

func BenchmarkEAN(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = validate.EAN("4006381333931")
	}
}

func BenchmarkBech32(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = validate.Bech32("bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4")
	}
}

func BenchmarkETHAddress(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = validate.ETHAddress("0x52908400098527886E0F7030069857D2E4169EE7")
	}
}

func BenchmarkBTCAddress(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = validate.BTCAddress("1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa")
	}
}
