package money

import "sort"

// Allocate splits the amount proportionally to the integer ratios, penny-perfect
// at the currency's MinorUnits using the largest-remainder method so that the
// returned parts always sum back to m exactly. It returns ErrInvalidAllocation
// when no ratios are given, any ratio is negative, or the ratios sum to zero or
// less.
func (m Money) Allocate(ratios ...int) ([]Money, error) {
	if len(ratios) == 0 {
		return nil, ErrInvalidAllocation
	}
	sum := 0
	for _, r := range ratios {
		if r < 0 {
			return nil, ErrInvalidAllocation
		}
		sum += r
	}
	if sum <= 0 {
		return nil, ErrInvalidAllocation
	}

	total := m.Minor()

	// Signed floor toward zero plus remainder tracking, largest-remainder
	// distribution of the leftover units.
	type part struct {
		index int
		base  int64
		rem   int64 // |remainder|, for ranking
	}
	parts := make([]part, len(ratios))
	var allocated int64
	for i, r := range ratios {
		prod := total * int64(r)
		base := prod / int64(sum) // truncates toward zero in Go
		rem := prod - base*int64(sum)
		if rem < 0 {
			rem = -rem
		}
		parts[i] = part{index: i, base: base, rem: rem}
		allocated += base
	}

	leftover := total - allocated // carries the sign of total
	step := int64(1)
	if leftover < 0 {
		step = -1
		leftover = -leftover
	}

	// Rank by descending remainder, ties by ascending index (stable, deterministic).
	order := make([]int, len(parts))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		pa, pb := parts[order[a]], parts[order[b]]
		if pa.rem != pb.rem {
			return pa.rem > pb.rem
		}
		return pa.index < pb.index
	})

	for k := int64(0); k < leftover; k++ {
		parts[order[k%int64(len(order))]].base += step
	}

	out := make([]Money, len(ratios))
	for i := range parts {
		out[i] = FromMinor(parts[i].base, m.currency)
	}
	return out, nil
}

// Split divides the amount into n equal parts, distributing any remainder minor
// units to the first parts. It returns ErrInvalidAllocation for n <= 0.
func (m Money) Split(n int) ([]Money, error) {
	if n <= 0 {
		return nil, ErrInvalidAllocation
	}
	ratios := make([]int, n)
	for i := range ratios {
		ratios[i] = 1
	}
	return m.Allocate(ratios...)
}
