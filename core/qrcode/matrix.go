package qrcode

func moduleCount(version int) int { return 17 + 4*version }

type grid struct {
	module   []bool // true = dark
	reserved []bool // true = function pattern / reserved, not for data
	size     int
}

func newGrid(version int) *grid {
	n := moduleCount(version)
	return &grid{size: n, module: make([]bool, n*n), reserved: make([]bool, n*n)}
}

func (g *grid) idx(x, y int) int { return y*g.size + x }

func (g *grid) at(x, y int) bool         { return g.module[g.idx(x, y)] }
func (g *grid) set(x, y int, dark bool)  { g.module[g.idx(x, y)] = dark }
func (g *grid) reserve(x, y int)         { g.reserved[g.idx(x, y)] = true }
func (g *grid) isReserved(x, y int) bool { return g.reserved[g.idx(x, y)] }

// setFn sets a function-pattern module: colors it and marks it reserved.
func (g *grid) setFn(x, y int, dark bool) {
	g.set(x, y, dark)
	g.reserve(x, y)
}

func (g *grid) placeFunctionPatterns(version int) {
	n := g.size

	// Three finder patterns + their separators, at the three corners.
	g.placeFinder(0, 0)
	g.placeFinder(n-7, 0)
	g.placeFinder(0, n-7)

	// Timing patterns along row 6 and column 6.
	for i := 8; i < n-8; i++ {
		dark := i%2 == 0
		g.setFn(i, 6, dark)
		g.setFn(6, i, dark)
	}

	// Alignment patterns at every center pair, skipping ones that collide with
	// finder patterns.
	centers := alignmentCenters(version)
	for _, cy := range centers {
		for _, cx := range centers {
			if (cx <= 8 && cy <= 8) || (cx >= n-9 && cy <= 8) || (cx <= 8 && cy >= n-9) {
				continue
			}
			g.placeAlignment(cx, cy)
		}
	}

	// Dark module.
	g.setFn(8, n-8, true)

	// Reserve format-info areas (written after masking).
	for i := range 9 {
		g.reserveIfFree(i, 8)
		g.reserveIfFree(8, i)
	}
	for i := range 8 {
		g.reserveIfFree(n-1-i, 8)
		g.reserveIfFree(8, n-1-i)
	}
	// Reserve version-info areas for v >= 7.
	if version >= 7 {
		for y := range 6 {
			for x := n - 11; x < n-8; x++ {
				g.reserveIfFree(x, y)
				g.reserveIfFree(y, x)
			}
		}
	}
}

func (g *grid) reserveIfFree(x, y int) {
	if !g.isReserved(x, y) {
		g.reserve(x, y)
	}
}

func (g *grid) placeFinder(ox, oy int) {
	for dy := -1; dy <= 7; dy++ {
		for dx := -1; dx <= 7; dx++ {
			x, y := ox+dx, oy+dy
			if x < 0 || y < 0 || x >= g.size || y >= g.size {
				continue
			}
			// Separator ring (dx or dy == -1 or 7) is light; the rest is the
			// 7x7 finder: dark outer ring, light gap, 3x3 dark core.
			inFinder := dx >= 0 && dx <= 6 && dy >= 0 && dy <= 6
			var dark bool
			if inFinder {
				dark = dx == 0 || dx == 6 || dy == 0 || dy == 6 ||
					(dx >= 2 && dx <= 4 && dy >= 2 && dy <= 4)
			}
			g.setFn(x, y, dark)
		}
	}
}

func (g *grid) placeAlignment(cx, cy int) {
	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			// Dark outer ring + dark center; light in between.
			dark := dx == -2 || dx == 2 || dy == -2 || dy == 2 || (dx == 0 && dy == 0)
			g.setFn(cx+dx, cy+dy, dark)
		}
	}
}
