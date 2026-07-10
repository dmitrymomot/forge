package qrcode

// maskCondition reports whether module (x, y) is inverted by mask pattern id.
func maskCondition(id, x, y int) bool {
	switch id {
	case 0:
		return (x+y)%2 == 0
	case 1:
		return y%2 == 0
	case 2:
		return x%3 == 0
	case 3:
		return (x+y)%3 == 0
	case 4:
		return (y/2+x/3)%2 == 0
	case 5:
		return (x*y)%2+(x*y)%3 == 0
	case 6:
		return ((x*y)%2+(x*y)%3)%2 == 0
	case 7:
		return ((x+y)%2+(x*y)%3)%2 == 0
	default:
		return false
	}
}

// applyMask returns a copy of g with mask id applied to every non-reserved
// (data) module and format information written for (level, id).
func applyMask(g *grid, id int, level Level) *grid {
	out := &grid{size: g.size, module: append([]bool{}, g.module...), reserved: g.reserved}
	for y := range g.size {
		for x := range g.size {
			if !g.isReserved(x, y) && maskCondition(id, x, y) {
				out.module[out.idx(x, y)] = !out.module[out.idx(x, y)]
			}
		}
	}
	writeFormatInfo(out, level, id)
	return out
}

// bestMask applies all 8 masks at LevelM and returns the id + masked grid with
// the lowest penalty score. It is a convenience wrapper for tests; the encoder
// uses bestMaskForLevel so the format information carries the real level.
func bestMask(g *grid) (int, *grid) {
	return bestMaskForLevel(g, LevelM)
}

// bestMaskForLevel applies all 8 masks and returns the id + masked grid with
// the lowest penalty score, writing format information for level. Ties keep the
// lower mask id.
func bestMaskForLevel(g *grid, level Level) (int, *grid) {
	bestID := 0
	best := applyMask(g, 0, level)
	bestScore := penalty(best)
	for id := 1; id < 8; id++ {
		m := applyMask(g, id, level)
		if s := penalty(m); s < bestScore {
			bestID, bestScore, best = id, s, m
		}
	}
	return bestID, best
}

// ISO/IEC 18004 §8.8.2 demerit weights.
const (
	penaltyN1 = 3
	penaltyN2 = 3
	penaltyN3 = 40
	penaltyN4 = 10
)

// penalty scores a masked grid with the four ISO/IEC 18004 §8.8.2 rules. It
// mirrors the reference qrencode implementation so mask selection matches it
// exactly: rule 3 uses run-length ratio detection (not a fixed-window match,
// which false-positives when an adjacent module extends a run) and rule 4
// rounds the dark ratio rather than flooring it.
func penalty(g *grid) int {
	n := g.size
	demerit := 0

	// Rule 2: each 2x2 block of one color adds N2.
	for y := 1; y < n; y++ {
		for x := 1; x < n; x++ {
			c := g.at(x, y)
			if g.at(x-1, y) == c && g.at(x, y-1) == c && g.at(x-1, y-1) == c {
				demerit += penaltyN2
			}
		}
	}

	// Rules 1 and 3: run-length analysis of every row and column.
	run := make([]int, n+1)
	for y := range n {
		demerit += calcN1N3(run, rowRunLength(g, y, run))
	}
	for x := range n {
		demerit += calcN1N3(run, colRunLength(g, x, run))
	}

	// Rule 4: deviation of the (rounded) dark-module ratio from 50%.
	blacks := 0
	for _, m := range g.module {
		if m {
			blacks++
		}
	}
	w2 := n * n
	bratio := (200*blacks + w2) / w2 / 2 // == round(100*blacks/w2)
	dev := bratio - 50
	if dev < 0 {
		dev = -dev
	}
	demerit += (dev / 5) * penaltyN4

	return demerit
}

// rowRunLength fills run with the alternating same-color run lengths of row y
// and returns the count. A row starting dark gets a -1 sentinel at index 0 so
// that dark runs always land on odd indices (as calcN1N3 expects).
func rowRunLength(g *grid, y int, run []int) int {
	head := 0
	prev := g.at(0, y)
	if prev {
		run[0] = -1
		head = 1
	}
	run[head] = 1
	for x := 1; x < g.size; x++ {
		if c := g.at(x, y); c != prev {
			head++
			run[head] = 1
			prev = c
		} else {
			run[head]++
		}
	}
	return head + 1
}

// colRunLength is rowRunLength for column x.
func colRunLength(g *grid, x int, run []int) int {
	head := 0
	prev := g.at(x, 0)
	if prev {
		run[0] = -1
		head = 1
	}
	run[head] = 1
	for y := 1; y < g.size; y++ {
		if c := g.at(x, y); c != prev {
			head++
			run[head] = 1
			prev = c
		} else {
			run[head]++
		}
	}
	return head + 1
}

// calcN1N3 applies rule 1 (runs of 5+ modules) and rule 3 (1:1:3:1:1
// finder-like patterns with a 4x-module light margin on at least one side) to
// a single row/column's run lengths.
//
// The finder-margin boundary tests below (`i == 3` for the left margin,
// `i+4 >= length` for the right) mirror qrencode 4.1.1's Mask_calcN1N3
// VERBATIM — that release is the reference tool whose output the golden
// testdata is matched against. Do NOT "simplify" them to the ISO-idealized
// combined form (`run[i-3] < 0 || run[i-3] >= 4*fact || i+3 >= length ||
// run[i+3] >= 4*fact`): that form appears in some other QR implementations but
// NOT in qrencode 4.1.1, and it diverges on inputs like a light-start line
// with the finder run at index 3 (e.g. run=[1,1,1,3,1,1,1]: 4.1.1 scores +40,
// the combined form scores 0). Empirically the installed qrencode binary picks
// the mask this form predicts, not the combined one, on every input where the
// two disagree — matching 4.1.1 exactly is what preserves parity.
func calcN1N3(run []int, length int) int {
	demerit := 0
	for i := range length {
		if run[i] >= 5 {
			demerit += penaltyN1 + (run[i] - 5)
		}
		if i&1 == 1 && i >= 3 && i < length-2 && run[i]%3 == 0 {
			fact := run[i] / 3
			if run[i-2] == fact && run[i-1] == fact && run[i+1] == fact && run[i+2] == fact {
				switch {
				case i == 3 || run[i-3] >= 4*fact:
					demerit += penaltyN3
				case i+4 >= length || run[i+3] >= 4*fact:
					demerit += penaltyN3
				}
			}
		}
	}
	return demerit
}
