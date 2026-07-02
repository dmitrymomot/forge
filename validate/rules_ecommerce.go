package validate

import (
	"strings"
	"time"
)

func digitsOnly(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// luhn reports whether an all-digit string passes the Luhn checksum.
func luhn(s string) bool {
	sum, alt := 0, false
	for i := len(s) - 1; i >= 0; i-- {
		d := int(s[i] - '0')
		if alt {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		alt = !alt
	}
	return sum%10 == 0
}

// CreditCard requires a 13–19 digit, Luhn-valid PAN (spaces/dashes ignored).
func CreditCard(s string) Violation {
	s = strings.NewReplacer(" ", "", "-", "").Replace(s)
	if !digitsOnly(s) || len(s) < 13 || len(s) > 19 || !luhn(s) {
		return Violation{Key: "validation.credit_card"}
	}
	return Violation{}
}

// CVV requires a 3–4 digit code.
func CVV(s string) Violation {
	if !digitsOnly(s) || (len(s) != 3 && len(s) != 4) {
		return Violation{Key: "validation.cvv"}
	}
	return Violation{}
}

// CardExpiry requires "MM/YY" with a valid month, not before the current month.
func CardExpiry(s string) Violation {
	fail := Violation{Key: "validation.card_expiry"}
	parts := strings.Split(s, "/")
	if len(parts) != 2 || len(parts[0]) != 2 || len(parts[1]) != 2 || !digitsOnly(parts[0]) || !digitsOnly(parts[1]) {
		return fail
	}
	mm := int(parts[0][0]-'0')*10 + int(parts[0][1]-'0')
	yy := int(parts[1][0]-'0')*10 + int(parts[1][1]-'0')
	if mm < 1 || mm > 12 {
		return fail
	}
	exp := time.Date(2000+yy, time.Month(mm)+1, 1, 0, 0, 0, 0, time.UTC) // first of month after expiry
	if !time.Now().Before(exp) {
		return fail
	}
	return Violation{}
}

// CurrencyCode requires an ISO-4217 alpha-3 code (uppercase).
func CurrencyCode(s string) Violation {
	if _, ok := currencyCodes[s]; !ok {
		return Violation{Key: "validation.currency_code"}
	}
	return Violation{}
}

// CountryCode requires an ISO-3166-1 alpha-2 code (uppercase).
func CountryCode(s string) Violation {
	if _, ok := countryCodes[s]; !ok {
		return Violation{Key: "validation.country_code"}
	}
	return Violation{}
}

// eanChecksum validates an EAN/UPC/ISBN-13 style mod-10 check digit.
func eanChecksum(s string) bool {
	sum := 0
	for i := 0; i < len(s)-1; i++ {
		d := int(s[i] - '0')
		if (len(s)-i)%2 == 0 {
			d *= 3
		}
		sum += d
	}
	check := (10 - sum%10) % 10
	return check == int(s[len(s)-1]-'0')
}

// EAN requires an 8- or 13-digit barcode with a valid check digit.
func EAN(s string) Violation {
	if !digitsOnly(s) || (len(s) != 8 && len(s) != 13) || !eanChecksum(s) {
		return Violation{Key: "validation.ean"}
	}
	return Violation{}
}

// ISBN requires a valid ISBN-10 (mod 11, trailing X allowed) or ISBN-13 (mod 10).
func ISBN(s string) Violation {
	s = strings.NewReplacer("-", "", " ", "").Replace(s)
	switch len(s) {
	case 10:
		sum := 0
		for i := range 9 {
			if s[i] < '0' || s[i] > '9' {
				return Violation{Key: "validation.isbn"}
			}
			sum += (10 - i) * int(s[i]-'0')
		}
		last := s[9]
		switch {
		case last == 'X' || last == 'x':
			sum += 10
		case last >= '0' && last <= '9':
			sum += int(last - '0')
		default:
			return Violation{Key: "validation.isbn"}
		}
		if sum%11 == 0 {
			return Violation{}
		}
	case 13:
		if digitsOnly(s) && eanChecksum(s) {
			return Violation{}
		}
	}
	return Violation{Key: "validation.isbn"}
}
