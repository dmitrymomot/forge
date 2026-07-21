package decimal_test

import (
	"testing"

	"github.com/dmitrymomot/forge/core/decimal"
)

// Small operands stay on the int64 fast path; the big literals below overflow
// int64 so the *big.Int slow path is measured too.
const (
	bigLit  = "123456789012345678901234567890.123456789"
	bigLit2 = "987654321098765432109876543210.987654321"
)

func BenchmarkParse(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_, _ = decimal.Parse("1234567.89")
	}
}

func BenchmarkParseBig(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_, _ = decimal.Parse(bigLit)
	}
}

func BenchmarkString(b *testing.B) {
	d := decimal.MustParse("1234567.89")
	b.ReportAllocs()
	for b.Loop() {
		_ = d.String()
	}
}

func BenchmarkStringBig(b *testing.B) {
	d := decimal.MustParse(bigLit)
	b.ReportAllocs()
	for b.Loop() {
		_ = d.String()
	}
}

func BenchmarkAdd(b *testing.B) {
	x := decimal.MustParse("1234.56")
	y := decimal.MustParse("78.9012")
	b.ReportAllocs()
	for b.Loop() {
		_ = x.Add(y)
	}
}

func BenchmarkAddBig(b *testing.B) {
	x := decimal.MustParse(bigLit)
	y := decimal.MustParse(bigLit2)
	b.ReportAllocs()
	for b.Loop() {
		_ = x.Add(y)
	}
}

func BenchmarkSub(b *testing.B) {
	x := decimal.MustParse("1234.56")
	y := decimal.MustParse("78.9012")
	b.ReportAllocs()
	for b.Loop() {
		_ = x.Sub(y)
	}
}

func BenchmarkSubBig(b *testing.B) {
	x := decimal.MustParse(bigLit)
	y := decimal.MustParse(bigLit2)
	b.ReportAllocs()
	for b.Loop() {
		_ = x.Sub(y)
	}
}

func BenchmarkMul(b *testing.B) {
	x := decimal.MustParse("1234.56")
	y := decimal.MustParse("78.9012")
	b.ReportAllocs()
	for b.Loop() {
		_ = x.Mul(y)
	}
}

func BenchmarkMulBig(b *testing.B) {
	x := decimal.MustParse(bigLit)
	y := decimal.MustParse(bigLit2)
	b.ReportAllocs()
	for b.Loop() {
		_ = x.Mul(y)
	}
}

func BenchmarkDiv(b *testing.B) {
	x := decimal.MustParse("100")
	y := decimal.MustParse("3")
	b.ReportAllocs()
	for b.Loop() {
		_, _ = x.Div(y, 10, decimal.HalfEven)
	}
}

func BenchmarkDivBig(b *testing.B) {
	x := decimal.MustParse(bigLit)
	y := decimal.MustParse("7")
	b.ReportAllocs()
	for b.Loop() {
		_, _ = x.Div(y, 20, decimal.HalfEven)
	}
}

func BenchmarkRound(b *testing.B) {
	d := decimal.MustParse("1234.56789")
	b.ReportAllocs()
	for b.Loop() {
		_ = d.Round(2, decimal.HalfEven)
	}
}

func BenchmarkRoundBig(b *testing.B) {
	d := decimal.MustParse(bigLit)
	b.ReportAllocs()
	for b.Loop() {
		_ = d.Round(2, decimal.HalfEven)
	}
}

func BenchmarkRescaleUp(b *testing.B) {
	d := decimal.MustParse("1234.56")
	b.ReportAllocs()
	for b.Loop() {
		_ = d.Rescale(8, decimal.HalfEven)
	}
}

func BenchmarkRescaleDown(b *testing.B) {
	d := decimal.MustParse("1234.56789012")
	b.ReportAllocs()
	for b.Loop() {
		_ = d.Rescale(2, decimal.HalfEven)
	}
}

func BenchmarkRescaleBig(b *testing.B) {
	d := decimal.MustParse(bigLit)
	b.ReportAllocs()
	for b.Loop() {
		_ = d.Rescale(30, decimal.HalfEven)
	}
}

func BenchmarkCmp(b *testing.B) {
	x := decimal.MustParse("1234.56")
	y := decimal.MustParse("1234.57")
	b.ReportAllocs()
	for b.Loop() {
		_ = x.Cmp(y)
	}
}

func BenchmarkCmpBig(b *testing.B) {
	x := decimal.MustParse(bigLit)
	y := decimal.MustParse(bigLit2)
	b.ReportAllocs()
	for b.Loop() {
		_ = x.Cmp(y)
	}
}

func BenchmarkQuoRem(b *testing.B) {
	x := decimal.MustParse("100.5")
	y := decimal.MustParse("7")
	b.ReportAllocs()
	for b.Loop() {
		_, _, _ = x.QuoRem(y)
	}
}

func BenchmarkTruncate(b *testing.B) {
	d := decimal.MustParse("1234.56789")
	b.ReportAllocs()
	for b.Loop() {
		_ = d.Truncate(2)
	}
}

func BenchmarkIsInteger(b *testing.B) {
	d := decimal.MustParse("1234.56789")
	b.ReportAllocs()
	for b.Loop() {
		_ = d.IsInteger()
	}
}

func BenchmarkMarshalJSON(b *testing.B) {
	d := decimal.MustParse("1234.56")
	b.ReportAllocs()
	for b.Loop() {
		_, _ = d.MarshalJSON()
	}
}

func BenchmarkUnmarshalJSON(b *testing.B) {
	p := []byte(`"1234.56"`)
	b.ReportAllocs()
	for b.Loop() {
		var d decimal.Decimal
		_ = d.UnmarshalJSON(p)
	}
}

func BenchmarkValue(b *testing.B) {
	d := decimal.MustParse("1234.56")
	b.ReportAllocs()
	for b.Loop() {
		_, _ = d.Value()
	}
}

func BenchmarkScan(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		var d decimal.Decimal
		_ = d.Scan("1234.56")
	}
}
