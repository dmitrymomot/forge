package money

// Neg returns -m in the same currency, preserving the amount's full precision.
func (m Money) Neg() Money {
	return Money{amount: m.amount.Neg(), currency: m.currency}
}

// Abs returns |m| in the same currency, preserving the amount's full precision.
func (m Money) Abs() Money {
	return Money{amount: m.amount.Abs(), currency: m.currency}
}

// IsPositive reports whether the amount is strictly greater than zero.
func (m Money) IsPositive() bool { return m.amount.Sign() > 0 }

// IsNegative reports whether the amount is strictly less than zero.
func (m Money) IsNegative() bool { return m.amount.Sign() < 0 }
