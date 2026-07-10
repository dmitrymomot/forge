package qrcode

// Level is the QR error-correction level: higher levels recover more of a
// damaged or obscured symbol at the cost of a denser code.
type Level int

const (
	LevelL Level = iota // ~7% recovery
	LevelM              // ~15% recovery (default)
	LevelQ              // ~25% recovery
	LevelH              // ~30% recovery
)

// formatBits returns the 2-bit error-correction indicator used in the QR
// format information (spec order: L=01, M=00, Q=11, H=10).
func (l Level) formatBits() int {
	switch l {
	case LevelL:
		return 0b01
	case LevelM:
		return 0b00
	case LevelQ:
		return 0b11
	case LevelH:
		return 0b10
	default:
		return 0b00
	}
}

// String returns the single-letter level name ("L", "M", "Q", "H").
func (l Level) String() string {
	switch l {
	case LevelL:
		return "L"
	case LevelM:
		return "M"
	case LevelQ:
		return "Q"
	case LevelH:
		return "H"
	default:
		return "?"
	}
}
