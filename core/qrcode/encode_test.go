package qrcode

import (
	"errors"
	"testing"
)

func TestCharCountBits(t *testing.T) {
	if charCountBits(1) != 8 || charCountBits(9) != 8 {
		t.Error("v1-9 byte char-count must be 8 bits")
	}
	if charCountBits(10) != 16 || charCountBits(40) != 16 {
		t.Error("v10-40 byte char-count must be 16 bits")
	}
}

func TestVersionSpecKnown(t *testing.T) {
	// ISO/IEC 18004 Table 9 anchors.
	// v1-M: 1 block, 16 data codewords, 10 EC codewords.
	s := versionSpec(1, LevelM)
	if s.ecPerBlock != 10 || s.group1Blocks != 1 || s.group1Words != 16 || s.group2Blocks != 0 {
		t.Errorf("v1-M spec wrong: %+v", s)
	}
	// v5-Q: 2 blocks of 15 + 2 blocks of 16, 18 EC codewords each.
	s = versionSpec(5, LevelQ)
	if s.ecPerBlock != 18 || s.group1Blocks != 2 || s.group1Words != 15 ||
		s.group2Blocks != 2 || s.group2Words != 16 {
		t.Errorf("v5-Q spec wrong: %+v", s)
	}
}

func TestPickVersion(t *testing.T) {
	// v1-M holds 16 data codewords = 16 bytes total, minus mode(4b)+count(8b)+
	// terminator overhead: ~14 bytes of payload. A 10-byte payload fits v1.
	v, err := pickVersion(10, LevelM)
	if err != nil {
		t.Fatalf("pickVersion: %v", err)
	}
	if v != 1 {
		t.Errorf("version = %d, want 1", v)
	}
	// Way past the v40 ceiling must error.
	if _, err := pickVersion(1<<20, LevelM); !errors.Is(err, ErrTooLarge) {
		t.Errorf("err = %v, want ErrTooLarge", err)
	}
}

func TestEncodeDataStructure(t *testing.T) {
	v, _ := pickVersion(4, LevelM)
	cw := encodeData([]byte("test"), v, LevelM)
	total := versionSpec(v, LevelM)
	want := (total.group1Blocks*total.group1Words + total.group2Blocks*total.group2Words)
	if len(cw) != want {
		t.Fatalf("len(codewords) = %d, want %d", len(cw), want)
	}
	// First byte: mode nibble 0100 then high nibble of length (4). "test" len=4,
	// v1 count is 8 bits -> first byte = 0100_0000 = 0x40, second = 0100_XXXX.
	if cw[0] != 0x40 {
		t.Errorf("cw[0] = %#x, want 0x40 (byte mode + high count nibble)", cw[0])
	}
	// Tail must be pad codewords 0xEC / 0x11 alternating once padding starts.
	if cw[len(cw)-1] != 0x11 && cw[len(cw)-1] != 0xEC {
		t.Errorf("last codeword = %#x, want pad 0xEC/0x11", cw[len(cw)-1])
	}
}

// levels enumerates every EC level in Level-iota order for whole-table sweeps.
var levels = []Level{LevelL, LevelM, LevelQ, LevelH}

// TestTotalCodewordsConstantPerVersion is a transcription safety net: the EC
// level only shifts the data/EC split, so the TOTAL codeword count (data + EC)
// for a version is invariant across L, M, Q, H. A mis-transcribed row in any
// single level breaks this equality and localizes the error immediately.
func TestTotalCodewordsConstantPerVersion(t *testing.T) {
	for v := 1; v <= 40; v++ {
		var totals [4]int
		for i, lvl := range levels {
			s := versionSpec(v, lvl)
			totals[i] = dataCodewords(v, lvl) + (s.group1Blocks+s.group2Blocks)*s.ecPerBlock
		}
		if totals[0] != totals[1] || totals[1] != totals[2] || totals[2] != totals[3] {
			t.Errorf("v%d total codewords differ across levels: L=%d M=%d Q=%d H=%d",
				v, totals[0], totals[1], totals[2], totals[3])
		}
	}
}

// TestECBlocksPopulated catches an empty or missing EC-block row for any
// version/level.
func TestECBlocksPopulated(t *testing.T) {
	for v := 1; v <= 40; v++ {
		for _, lvl := range levels {
			s := versionSpec(v, lvl)
			if s.ecPerBlock <= 0 || s.group1Blocks < 1 || s.group1Words <= 0 {
				t.Errorf("v%d level %s incomplete: %+v", v, lvl, s)
			}
		}
	}
}

// TestAlignmentCentersShape asserts the Annex E structural invariants for every
// version: v1 has none; otherwise the count is v/7+2, the first center is 6, and
// the last is the bottom-right center at moduleCount(v)-7 == 4*v+10.
func TestAlignmentCentersShape(t *testing.T) {
	if got := alignmentCenters(1); len(got) != 0 {
		t.Errorf("v1 alignment centers = %v, want empty", got)
	}
	for v := 2; v <= 40; v++ {
		c := alignmentCenters(v)
		if wantLen := v/7 + 2; len(c) != wantLen {
			t.Errorf("v%d: len(centers) = %d, want %d", v, len(c), wantLen)
			continue
		}
		if c[0] != 6 {
			t.Errorf("v%d: centers[0] = %d, want 6", v, c[0])
		}
		if last, want := c[len(c)-1], 4*v+10; last != want {
			t.Errorf("v%d: last center = %d, want %d (moduleCount-7)", v, last, want)
		}
	}
}
