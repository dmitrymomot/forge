package bytesize_test

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/core/bytesize"
)

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		want bytesize.ByteSize
	}{
		{"512", 512},
		{"512B", 512},
		{"10MB", 10 * bytesize.MB},
		{"10 MB", 10 * bytesize.MB},
		{"10mb", 10 * bytesize.MB},
		{"10MiB", 10 * bytesize.MiB},
		{"1.5GiB", bytesize.ByteSize(1.5 * float64(bytesize.GiB))},
		{"2GB", 2 * bytesize.GB},
	}
	for _, c := range cases {
		got, err := bytesize.Parse(c.in)
		if err != nil {
			t.Errorf("Parse(%q) error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("Parse(%q) = %d, want %d", c.in, int64(got), int64(c.want))
		}
	}
}

func TestParseInvalid(t *testing.T) {
	for _, in := range []string{"", "abc", "10XB", "MB", "1.2.3KB"} {
		if _, err := bytesize.Parse(in); !errors.Is(err, bytesize.ErrInvalidSize) {
			t.Errorf("Parse(%q): want ErrInvalidSize, got %v", in, err)
		}
	}
}

func TestStringRoundTrip(t *testing.T) {
	vals := []bytesize.ByteSize{
		0, 512, 1023, 1024, 1536, 1537,
		10 * bytesize.MiB, 2 * bytesize.GiB, 10 * bytesize.MB,
	}
	for _, v := range vals {
		s := v.String()
		got, err := bytesize.Parse(s)
		if err != nil {
			t.Errorf("Parse(String(%d)=%q) error: %v", int64(v), s, err)
			continue
		}
		if got != v {
			t.Errorf("round-trip %d -> %q -> %d", int64(v), s, int64(got))
		}
	}
}

func TestFormatFamilies(t *testing.T) {
	if got := bytesize.FormatIEC(1536); got != "1.5KiB" {
		t.Errorf("FormatIEC(1536) = %q", got)
	}
	if got := bytesize.FormatSI(1_500_000); got != "1.5MB" {
		t.Errorf("FormatSI(1_500_000) = %q", got)
	}
	if got := bytesize.FormatIEC(1537); got != "1537B" {
		t.Errorf("FormatIEC(1537) = %q", got)
	}
	if got := bytesize.FormatIEC(1024); got != "1KiB" {
		t.Errorf("FormatIEC(1024) = %q", got)
	}
}

func TestParseOverflow(t *testing.T) {
	for _, in := range []string{"9999999PiB", "9999999999999999999", "1e30MB", "NaNMB", "InfB", "-InfKB", "8192.0PiB"} {
		if _, err := bytesize.Parse(in); !errors.Is(err, bytesize.ErrInvalidSize) {
			t.Errorf("Parse(%q): want ErrInvalidSize, got %v", in, err)
		}
	}
}

func TestParseFloatLowerBoundary(t *testing.T) {
	// -8192.0PiB == math.MinInt64 exactly (valid lower boundary; must NOT be rejected).
	got, err := bytesize.Parse("-8192.0PiB")
	if err != nil {
		t.Fatalf("Parse(-8192.0PiB) unexpected err: %v", err)
	}
	if int64(got) != math.MinInt64 {
		t.Fatalf("Parse(-8192.0PiB) = %d, want math.MinInt64", int64(got))
	}
}

func TestNegativeRoundTrip(t *testing.T) {
	vals := []bytesize.ByteSize{-1536, -1 * bytesize.MiB, math.MinInt64}
	for _, v := range vals {
		s := v.String()
		if strings.HasPrefix(s, "--") {
			t.Errorf("String(%d) has doubled sign: %q", int64(v), s)
		}
		got, err := bytesize.Parse(s)
		if err != nil {
			t.Errorf("Parse(String(%d)=%q) error: %v", int64(v), s, err)
			continue
		}
		if got != v {
			t.Errorf("negative round-trip %d -> %q -> %d", int64(v), s, int64(got))
		}
	}
}

func TestTextMarshaling(t *testing.T) {
	type cfg struct {
		Max bytesize.ByteSize `json:"max"`
	}
	var c cfg
	if err := json.Unmarshal([]byte(`{"max":"10MiB"}`), &c); err != nil {
		t.Fatal(err)
	}
	if c.Max != 10*bytesize.MiB {
		t.Fatalf("got %d", int64(c.Max))
	}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"max":"10MiB"}` {
		t.Fatalf("got %s", b)
	}
}
