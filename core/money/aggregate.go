package money

// Sum returns the exact total of first and rest. Every value must share first's
// currency, otherwise it returns ErrCurrencyMismatch. The result keeps full
// precision (no rounding). first fixes the result currency, so the sum of a
// single value is that value; there is no ambiguous zero-currency empty case.
func Sum(first Money, rest ...Money) (Money, error) {
	total := first
	for _, m := range rest {
		var err error
		total, err = total.Add(m)
		if err != nil {
			return Money{}, err
		}
	}
	return total, nil
}
