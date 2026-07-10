package qrcode

import (
	"fmt"
	"image"
	"image/color"
)

const (
	defaultScale    = 8
	defaultBorder   = 4
	defaultLogoSize = 0.2
	maxLogoSize     = 0.3
)

type config struct {
	fg, bg      color.Color
	logo        image.Image // nil = no logo
	level       Level
	scale       int
	targetSize  int // WithSize; 0 = unset. Wins over scale when > 0.
	border      int
	logoSize    float64
	moduleShape Shape
	eyeShape    EyeShape
}

func defaultConfig() config {
	return config{
		level:       LevelM,
		scale:       defaultScale,
		border:      defaultBorder,
		fg:          color.Black,
		bg:          color.White,
		logoSize:    defaultLogoSize,
		moduleShape: ShapeSquare,
		eyeShape:    EyeSquare,
	}
}

func newConfig(opts ...Option) (config, error) {
	c := defaultConfig()
	for _, opt := range opts {
		opt(&c)
	}
	if err := c.validate(); err != nil {
		return config{}, err
	}
	return c, nil
}

func (c config) validate() error {
	if c.scale <= 0 {
		return fmt.Errorf("%w: %d", ErrInvalidScale, c.scale)
	}
	if c.border < 0 {
		return fmt.Errorf("%w: %d", ErrInvalidBorder, c.border)
	}
	if c.logoSize <= 0 || c.logoSize > maxLogoSize {
		return fmt.Errorf("%w: %v", ErrInvalidLogoSize, c.logoSize)
	}
	if c.fg == nil || c.bg == nil {
		return fmt.Errorf("%w", ErrInvalidColor)
	}
	return nil
}

// effectiveLevel raises the encoding level to the minimum a render decoration
// needs to stay scannable. Called by the renderers; Encode uses c.level as-is.
func (c config) effectiveLevel() Level {
	l := c.level
	if (c.logo != nil || c.moduleShape == ShapeDots) && l < LevelQ {
		l = LevelQ
	}
	return l
}
