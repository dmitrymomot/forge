package qrcode

import "testing"

func TestModuleCount(t *testing.T) {
	if moduleCount(1) != 21 || moduleCount(2) != 25 || moduleCount(40) != 177 {
		t.Errorf("moduleCount wrong: v1=%d v2=%d v40=%d", moduleCount(1), moduleCount(2), moduleCount(40))
	}
}

func TestFinderPatterns(t *testing.T) {
	g := newGrid(1)
	g.placeFunctionPatterns(1)
	// Top-left finder: outer ring dark at (0,0), inner gap light at (1,1),
	// 3x3 core dark at (2,2..4,4). Center of the 3x3 core is (3,3).
	if !g.at(0, 0) {
		t.Error("finder outer corner (0,0) must be dark")
	}
	if g.at(1, 1) {
		t.Error("finder inner ring (1,1) must be light")
	}
	if !g.at(3, 3) {
		t.Error("finder core center (3,3) must be dark")
	}
	// Separator: (7,0) is light (the 1-module light border around the finder).
	if g.at(7, 0) {
		t.Error("separator (7,0) must be light")
	}
}

func TestTimingPatterns(t *testing.T) {
	g := newGrid(1)
	g.placeFunctionPatterns(1)
	// Timing runs along row 6 and column 6, alternating dark/light, dark at
	// even coordinates. Sample column 6 between the finders.
	for x := 8; x < moduleCount(1)-8; x++ {
		want := x%2 == 0
		if g.at(x, 6) != want {
			t.Errorf("timing (%d,6) = %v, want %v", x, g.at(x, 6), want)
		}
	}
}

func TestDarkModule(t *testing.T) {
	g := newGrid(1)
	g.placeFunctionPatterns(1)
	// The always-dark module sits at (8, size-8).
	if !g.at(8, moduleCount(1)-8) {
		t.Error("dark module (8, size-8) must be dark")
	}
}
