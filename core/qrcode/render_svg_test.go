package qrcode_test

import (
	"image/color"
	"strconv"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/core/qrcode"
)

func TestSVGStructure(t *testing.T) {
	out, err := qrcode.SVG("hello", qrcode.WithBackground(color.White), qrcode.WithForeground(color.Black))
	if err != nil {
		t.Fatalf("SVG: %v", err)
	}
	s := string(out)
	if !strings.HasPrefix(s, "<svg") || !strings.Contains(s, "</svg>") {
		t.Errorf("not an svg document: %.40q", s)
	}
	m, err := qrcode.Encode("hello")
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	full := m.Size() + 2*4
	if !strings.Contains(s, `viewBox="0 0 `+strconv.Itoa(full)+" "+strconv.Itoa(full)+`"`) {
		t.Errorf("viewBox not in module units: %.80q", s)
	}
}
