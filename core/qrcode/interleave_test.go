package qrcode

import "testing"

func TestFinalCodewordsLength(t *testing.T) {
	// Total codewords = data codewords + EC codewords across all blocks.
	v, level := 5, LevelQ
	s := versionSpec(v, level)
	dataCW := dataCodewords(v, level)
	blocks := s.group1Blocks + s.group2Blocks
	wantTotal := dataCW + blocks*s.ecPerBlock

	got := finalCodewords([]byte("interleave me across blocks!!"), v, level)
	if len(got) != wantTotal {
		t.Fatalf("len(final) = %d, want %d", len(got), wantTotal)
	}
}

func TestFinalCodewordsSingleBlock(t *testing.T) {
	// v1-M is a single block: interleaving is identity — data codewords then
	// EC codewords, in order.
	v, level := 1, LevelM
	payload := []byte("hello")
	data := encodeData(payload, v, level)
	ec := ecCodewords(data, versionSpec(v, level).ecPerBlock)

	got := finalCodewords(payload, v, level)
	want := append(append([]byte{}, data...), ec...)
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("byte %d = %d, want %d", i, got[i], want[i])
		}
	}
}
