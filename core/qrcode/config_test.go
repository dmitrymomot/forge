package qrcode

import (
	"errors"
	"image/color"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	c, err := newConfig()
	if err != nil {
		t.Fatalf("newConfig: %v", err)
	}
	if c.level != LevelM {
		t.Errorf("level = %v, want LevelM", c.level)
	}
	if c.scale != 8 {
		t.Errorf("scale = %d, want 8", c.scale)
	}
	if c.border != 4 {
		t.Errorf("border = %d, want 4", c.border)
	}
	if c.fg != (color.Black) || c.bg != (color.White) {
		t.Errorf("fg/bg = %v/%v, want black/white", c.fg, c.bg)
	}
	if c.moduleShape != ShapeSquare || c.eyeShape != EyeSquare {
		t.Errorf("shapes = %v/%v, want square/square", c.moduleShape, c.eyeShape)
	}
	if c.logoSize != 0.2 {
		t.Errorf("logoSize = %v, want 0.2", c.logoSize)
	}
}

func TestOptionsApply(t *testing.T) {
	c, err := newConfig(
		WithLevel(LevelH), WithScale(10), WithSize(300), WithBorder(2),
		WithForeground(color.RGBA{R: 1}), WithBackground(color.RGBA{B: 1}),
		WithModuleShape(ShapeDots), WithEyeShape(EyeRounded), WithLogoSize(0.25),
	)
	if err != nil {
		t.Fatalf("newConfig: %v", err)
	}
	if c.level != LevelH || c.scale != 10 || c.targetSize != 300 || c.border != 2 {
		t.Errorf("options not applied: %+v", c)
	}
	if c.moduleShape != ShapeDots || c.eyeShape != EyeRounded || c.logoSize != 0.25 {
		t.Errorf("shape/logo options not applied: %+v", c)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name string
		opts []Option
		want error
	}{
		{"scale zero", []Option{WithScale(0)}, ErrInvalidScale},
		{"scale negative", []Option{WithScale(-1)}, ErrInvalidScale},
		{"border negative", []Option{WithBorder(-1)}, ErrInvalidBorder},
		{"logo too big", []Option{WithLogoSize(0.4)}, ErrInvalidLogoSize},
		{"logo zero", []Option{WithLogoSize(0)}, ErrInvalidLogoSize},
		{"nil foreground", []Option{WithForeground(nil)}, ErrInvalidColor},
		{"nil background", []Option{WithBackground(nil)}, ErrInvalidColor},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := newConfig(tt.opts...); !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestLevelString(t *testing.T) {
	if LevelL.String() != "L" || LevelM.String() != "M" || LevelQ.String() != "Q" || LevelH.String() != "H" {
		t.Error("Level.String mismatch")
	}
}
