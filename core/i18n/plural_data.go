package i18n

// Curated CLDR cardinal rules (integer branch only — counts are ints in v1).
// Source: CLDR supplemental plurals; per-language, never family buckets:
// family bucketing is how the old packages broke uk/ru 21 → one.

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// ruleOneOther: en, de, nl, tr.
func ruleOneOther(n int) PluralCategory {
	if absInt(n) == 1 {
		return One
	}
	return Other
}

// ruleOther: ja, zh — no plural distinction.
func ruleOther(int) PluralCategory { return Other }

// ruleFrench: 0..1 → one; whole millions → many.
func ruleFrench(n int) PluralCategory {
	a := absInt(n)
	if a == 0 || a == 1 {
		return One
	}
	if a >= 1000000 && a%1000000 == 0 {
		return Many
	}
	return Other
}

// ruleSpanish: exactly 1 → one; whole millions → many.
func ruleSpanish(n int) PluralCategory {
	a := absInt(n)
	if a == 1 {
		return One
	}
	if a >= 1000000 && a%1000000 == 0 {
		return Many
	}
	return Other
}

// ruleItalianPortuguese shares the Spanish integer branch.
func ruleItalianPortuguese(n int) PluralCategory { return ruleSpanish(n) }

// ruleEastSlavic: uk, ru (and sr/hr/bs when added).
// one:  n%10==1 && n%100!=11
// few:  n%10 in 2..4 && n%100 not in 12..14
// many: everything else (incl. 0)
func ruleEastSlavic(n int) PluralCategory {
	a := absInt(n)
	m10, m100 := a%10, a%100
	switch {
	case m10 == 1 && m100 != 11:
		return One
	case m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14):
		return Few
	default:
		return Many
	}
}

// rulePolish: one only for exactly 1; few for 2..4 windows; many otherwise.
func rulePolish(n int) PluralCategory {
	a := absInt(n)
	if a == 1 {
		return One
	}
	m10, m100 := a%10, a%100
	if m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14) {
		return Few
	}
	return Many
}

// ruleCzech: cs (Slovak shares this shape if a sk row is ever added).
func ruleCzech(n int) PluralCategory {
	a := absInt(n)
	switch {
	case a == 1:
		return One
	case a >= 2 && a <= 4:
		return Few
	default:
		return Other
	}
}

// ruleArabic: full six-category CLDR rule.
func ruleArabic(n int) PluralCategory {
	a := absInt(n)
	switch {
	case a == 0:
		return Zero
	case a == 1:
		return One
	case a == 2:
		return Two
	}
	m100 := a % 100
	switch {
	case m100 >= 3 && m100 <= 10:
		return Few
	case m100 >= 11 && m100 <= 99:
		return Many
	default:
		return Other
	}
}
