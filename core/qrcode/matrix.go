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

// writeFormatInfo writes the 15-bit format information (level + mask, BCH
// (15,5) with generator 0x537, XOR-masked with 0x5412) into both copies.
func writeFormatInfo(g *grid, level Level, mask int) {
	data := level.formatBits()<<3 | mask
	bch := data << 10
	for bch >= 1<<10 { // reduce by generator until degree < 10
		bch ^= 0x537 << (bitLen(bch) - 11)
	}
	bits := (data<<10 | bch) ^ 0x5412
	n := g.size
	// Around the top-left finder.
	for i := 0; i <= 5; i++ {
		g.set(8, i, bit(bits, i))
	}
	g.set(8, 7, bit(bits, 6))
	g.set(8, 8, bit(bits, 7))
	g.set(7, 8, bit(bits, 8))
	for i := 9; i < 15; i++ {
		g.set(14-i, 8, bit(bits, i))
	}
	// Split copy near the other two finders.
	for i := 0; i <= 7; i++ {
		g.set(n-1-i, 8, bit(bits, i))
	}
	for i := 8; i < 15; i++ {
		g.set(8, n-15+i, bit(bits, i))
	}
}

// writeVersionInfo writes the 18-bit version information (6-bit version, BCH
// (18,6) with generator 0x1F25, no XOR mask) into the two 3x6 blocks beside
// the top-right and bottom-left finders. It is a no-op below version 7.
func writeVersionInfo(g *grid, version int) {
	if version < 7 {
		return
	}
	bch := version << 12
	for bitLen(bch) >= 13 {
		bch ^= 0x1F25 << (bitLen(bch) - 13)
	}
	bits := version<<12 | bch
	n := g.size
	for i := range 18 {
		b := bit(bits, i)
		a, c := i/3, i%3
		g.set(a, n-11+c, b)
		g.set(n-11+c, a, b)
	}
}

func bit(v, i int) bool { return (v>>uint(i))&1 == 1 }

func bitLen(v int) int {
	n := 0
	for v > 0 {
		n++
		v >>= 1
	}
	return n
}

// placeData walks the standard upward/downward zigzag from the bottom-right,
// skipping column 6 (timing) and every reserved module, writing stream bits
// MSB-first. Any modules past the stream stay light (remainder bits are 0).
func (g *grid) placeData(stream []byte) {
	n := g.size
	bitIdx := 0
	next := func() bool {
		if bitIdx >= len(stream)*8 {
			return false // remainder bits are 0
		}
		b := (stream[bitIdx/8]>>uint(7-bitIdx%8))&1 == 1
		bitIdx++
		return b
	}
	up := true
	for col := n - 1; col > 0; col -= 2 {
		if col == 6 {
			col-- // skip the vertical timing column
		}
		for i := range n {
			y := i
			if up {
				y = n - 1 - i
			}
			for _, x := range [2]int{col, col - 1} {
				if !g.isReserved(x, y) {
					g.set(x, y, next())
				}
			}
		}
		up = !up
	}
}

// Matrix is a computed QR symbol: a square grid of dark/light modules with no
// quiet zone. Use the accessors to render it however you like, or pass the
// same data + options to PNG/SVG/DataURI for a ready image.
type Matrix struct {
	g       *grid
	version int
	level   Level
}

// Size returns the module count per side (excludes the quiet zone).
func (m *Matrix) Size() int { return m.g.size }

// Module reports whether the module at (x, y) is dark. x and y must be in
// [0, Size()); out-of-range coordinates panic (the Size()-bounded render loop
// is the intended contract).
func (m *Matrix) Module(x, y int) bool { return m.g.at(x, y) }

// Version returns the QR version (1-40).
func (m *Matrix) Version() int { return m.version }

// Level returns the error-correction level the grid was encoded at.
func (m *Matrix) Level() Level { return m.level }

// Encode builds the QR module matrix for data. It honors WithLevel; render
// options (size, colors, logo, shapes) are ignored — pass them to PNG/SVG/
// DataURI instead. Returns ErrTooLarge if data exceeds version-40 capacity.
func Encode(data string, opts ...Option) (*Matrix, error) {
	c, err := newConfig(opts...)
	if err != nil {
		return nil, err
	}
	return encodeMatrix(data, c.level)
}

// encodeMatrix runs the full pipeline at an explicit level. Renderers call it
// with the effective (possibly raised) level.
func encodeMatrix(data string, level Level) (*Matrix, error) {
	raw := []byte(data)
	version, err := pickVersion(len(raw), level)
	if err != nil {
		return nil, err
	}
	g := newGrid(version)
	g.placeFunctionPatterns(version)
	writeVersionInfo(g, version)
	g.placeData(finalCodewords(raw, version, level))
	_, masked := bestMaskForLevel(g, level)
	return &Matrix{g: masked, version: version, level: level}, nil
}
