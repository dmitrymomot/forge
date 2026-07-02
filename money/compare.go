package money

// The comparison helpers below are thin, readable wrappers over Cmp. Each
// requires a matching currency and surfaces ErrCurrencyMismatch otherwise; on
// error they report false so a caller that ignores the error never sees a
// spurious true.

// Equal reports whether m and n are numerically equal (scale-normalized).
func (m Money) Equal(n Money) (bool, error) {
	c, err := m.Cmp(n)
	if err != nil {
		return false, err
	}
	return c == 0, nil
}

// LessThan reports whether m < n.
func (m Money) LessThan(n Money) (bool, error) {
	c, err := m.Cmp(n)
	if err != nil {
		return false, err
	}
	return c < 0, nil
}

// LessThanOrEqual reports whether m <= n.
func (m Money) LessThanOrEqual(n Money) (bool, error) {
	c, err := m.Cmp(n)
	if err != nil {
		return false, err
	}
	return c <= 0, nil
}

// GreaterThan reports whether m > n.
func (m Money) GreaterThan(n Money) (bool, error) {
	c, err := m.Cmp(n)
	if err != nil {
		return false, err
	}
	return c > 0, nil
}

// GreaterThanOrEqual reports whether m >= n.
func (m Money) GreaterThanOrEqual(n Money) (bool, error) {
	c, err := m.Cmp(n)
	if err != nil {
		return false, err
	}
	return c >= 0, nil
}
