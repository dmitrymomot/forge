package qrcode

import "testing"

func TestIsEyeModule(t *testing.T) {
	const size = 21 // version 1
	// One coordinate inside each of the three finder regions.
	inside := []struct{ x, y int }{
		{0, 0},        // top-left corner
		{6, 6},        // top-left, far edge
		{size - 7, 0}, // top-right corner
		{size - 1, 6}, // top-right, far edge
		{0, size - 7}, // bottom-left corner
		{6, size - 1}, // bottom-left, far edge
	}
	for _, p := range inside {
		if !isEyeModule(size, p.x, p.y) {
			t.Errorf("isEyeModule(%d,%d) = false, want true (inside a finder)", p.x, p.y)
		}
	}
	// Center/data coordinates outside every finder region.
	outside := []struct{ x, y int }{
		{size / 2, size / 2}, // dead center
		{7, 7},               // just past the top-left finder
		{size - 8, size - 8}, // no finder at the bottom-right
		{10, 0},              // top edge, between the two top finders
	}
	for _, p := range outside {
		if isEyeModule(size, p.x, p.y) {
			t.Errorf("isEyeModule(%d,%d) = true, want false (data module)", p.x, p.y)
		}
	}
}

func TestSupersampleFactor(t *testing.T) {
	cases := []struct {
		name   string
		module Shape
		eye    EyeShape
		want   int
	}{
		{"all square", ShapeSquare, EyeSquare, 1},
		{"dots data", ShapeDots, EyeSquare, 4},
		{"rounded data", ShapeRounded, EyeSquare, 4},
		{"rounded eyes only", ShapeSquare, EyeRounded, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := config{moduleShape: tc.module, eyeShape: tc.eye}
			if got := c.supersampleFactor(); got != tc.want {
				t.Errorf("supersampleFactor() = %d, want %d", got, tc.want)
			}
		})
	}
}
