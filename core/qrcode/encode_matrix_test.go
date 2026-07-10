package qrcode_test

import (
	"os"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/core/qrcode"
)

// loremBase is repeated and truncated to build the higher-version golden
// inputs. It is byte-identical to the string fed to qrencode when the golden
// testdata was generated (see mid_h.txt / long_q.txt).
const loremBase = "The quick brown fox jumps over the lazy dog. Pack my box with five dozen liquor jugs. Sphinx of black quartz judge my vow. Waltz nymph for quick jigs vex bud. "

// parseGolden turns a `qrencode -t ASCII -m 0` dump into a boolean matrix.
// Dark modules render as non-space characters; light modules as spaces. Each
// module is two characters wide.
func parseGolden(t *testing.T, path string) [][]bool {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	var m [][]bool
	for _, ln := range lines {
		if ln == "" {
			continue
		}
		row := make([]bool, 0, len(ln)/2)
		for i := 0; i+1 < len(ln); i += 2 {
			row = append(row, ln[i] != ' ')
		}
		m = append(m, row)
	}
	return m
}

// TestEncodeMatchesGolden is the correctness gate: it proves the hand-rolled
// encoder (Reed-Solomon, interleaving, placement, masking, format/version
// info) matches the reference qrencode tool module-for-module across several
// versions, levels, and both character-count widths.
func TestEncodeMatchesGolden(t *testing.T) {
	cases := []struct {
		name  string
		input string
		level qrcode.Level
		file  string
	}{
		{"url_v3_M", "https://forge.example/r/abc123", qrcode.LevelM, "testdata/url_m.txt"},
		{"hello_v1_L", "HELLO WORLD", qrcode.LevelL, "testdata/hello_l.txt"},
		{"hello_v2_H", "HELLO WORLD", qrcode.LevelH, "testdata/hello_h.txt"},
		// mid_h: 60 bytes at level H -> version 7 (exercises version info).
		{"mid_v7_H", strings.Repeat(loremBase, 10)[:60], qrcode.LevelH, "testdata/mid_h.txt"},
		// long_q: 250 bytes at level Q -> version 14 (16-bit char count,
		// many alignment patterns, heavy multi-block interleaving).
		{"long_v14_Q", strings.Repeat(loremBase, 10)[:250], qrcode.LevelQ, "testdata/long_q.txt"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			golden := parseGolden(t, tc.file)
			wantVersion := (len(golden) - 17) / 4

			m, err := qrcode.Encode(tc.input, qrcode.WithLevel(tc.level))
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			if m.Size() != len(golden) {
				t.Fatalf("size = %d (version %d), want %d (version %d) — version/capacity mismatch",
					m.Size(), m.Version(), len(golden), wantVersion)
			}
			if m.Version() != wantVersion {
				t.Fatalf("version = %d, want %d", m.Version(), wantVersion)
			}
			for y := range m.Size() {
				for x := range m.Size() {
					if m.Module(x, y) != golden[y][x] {
						t.Fatalf("module (%d,%d) = %v, want %v", x, y, m.Module(x, y), golden[y][x])
					}
				}
			}
		})
	}
}

func TestEncodeAccessors(t *testing.T) {
	m, err := qrcode.Encode("test", qrcode.WithLevel(qrcode.LevelH))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if m.Version() < 1 || m.Version() > 40 {
		t.Errorf("version = %d out of range", m.Version())
	}
	if m.Level() != qrcode.LevelH {
		t.Errorf("level = %v, want H", m.Level())
	}
	if m.Size() != 17+4*m.Version() {
		t.Errorf("size %d inconsistent with version %d", m.Size(), m.Version())
	}
}

func TestEncodeEmptyInput(t *testing.T) {
	m, err := qrcode.Encode("", qrcode.WithLevel(qrcode.LevelM))
	if err != nil {
		t.Fatalf("empty input must be legal: %v", err)
	}
	if m.Size() != 17+4*m.Version() {
		t.Errorf("size %d inconsistent with version %d", m.Size(), m.Version())
	}
}

func TestEncodeTooLarge(t *testing.T) {
	_, err := qrcode.Encode(strings.Repeat("x", 5000), qrcode.WithLevel(qrcode.LevelH))
	if err == nil {
		t.Fatal("expected ErrTooLarge for oversized input")
	}
}
