package qrcode

import "testing"

func TestFormatInfoRoundTrip(t *testing.T) {
	// Format bits map: L=01, M=00, Q=11, H=10 (spec order).
	if LevelM.formatBits() != 0b00 || LevelL.formatBits() != 0b01 ||
		LevelQ.formatBits() != 0b11 || LevelH.formatBits() != 0b10 {
		t.Fatal("formatBits mapping wrong")
	}
}

func TestBestMaskDeterministic(t *testing.T) {
	g := newGrid(1)
	g.placeFunctionPatterns(1)
	stream := finalCodewords([]byte("mask"), 1, LevelM)
	g.placeData(stream)
	mask, masked := bestMask(g)
	if mask < 0 || mask > 7 {
		t.Fatalf("mask = %d out of range", mask)
	}
	if masked.size != g.size {
		t.Fatal("masked grid wrong size")
	}
}
