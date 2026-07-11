package mmdb

import "testing"

func TestCtrlSimpleTypes(t *testing.T) {
	// String "US": type 2 (0b010), size 2 -> control 0x42, then 'U','S'.
	typ, size, dataOff, err := ctrl([]byte{0x42, 'U', 'S'}, 0)
	if err != nil || typ != typeString || size != 2 || dataOff != 1 {
		t.Fatalf("ctrl=(%d,%d,%d,%v)", typ, size, dataOff, err)
	}
}

func TestStringAt(t *testing.T) {
	s, next, err := stringAt([]byte{0x42, 'U', 'S'}, 0)
	if err != nil || s != "US" || next != 3 {
		t.Fatalf("stringAt=(%q,%d,%v)", s, next, err)
	}
}

func TestUintAt(t *testing.T) {
	// uint16 300 = 0x012C: type 5 (0b101), size 2 -> control 0xA2, then 0x01,0x2C.
	v, next, err := uintAt([]byte{0xA2, 0x01, 0x2C}, 0)
	if err != nil || v != 300 || next != 3 {
		t.Fatalf("uintAt=(%d,%d,%v)", v, next, err)
	}
	// uint32 13335 = 0x3417 stored minimal (size 2): type 6 (0b110) -> 0xC2.
	v, _, err = uintAt([]byte{0xC2, 0x34, 0x17}, 0)
	if err != nil || v != 13335 {
		t.Fatalf("uint32 v=%d err=%v", v, err)
	}
}

func TestExtendedSizeString(t *testing.T) {
	// size 29 marker: real size = 29 + next byte. control 0x5D (type 2, size 29),
	// size byte 0x00 -> len 29.
	data := make([]byte, 2+29)
	data[0], data[1] = 0x5D, 0x00
	for i := 2; i < len(data); i++ {
		data[i] = 'x'
	}
	s, next, err := stringAt(data, 0)
	if err != nil || len(s) != 29 || next != len(data) {
		t.Fatalf("len=%d next=%d err=%v", len(s), next, err)
	}
}

func TestSkipValueMap(t *testing.T) {
	// map{ "US": "x" }: control 0xE1 (type 7, size 1), key 0x42 'U' 'S', value 0x41 'x'.
	data := []byte{0xE1, 0x42, 'U', 'S', 0x41, 'x'}
	next, err := skipValue(data, 0, 0)
	if err != nil || next != len(data) {
		t.Fatalf("skip next=%d err=%v", next, err)
	}
}

func TestCtrlBounds(t *testing.T) {
	if _, _, _, err := ctrl([]byte{}, 0); err == nil {
		t.Fatal("ctrl past end should error")
	}
	if _, _, err := stringAt([]byte{0x42, 'U'}, 0); err == nil {
		t.Fatal("truncated string should error")
	}
}
