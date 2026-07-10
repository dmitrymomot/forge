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
