package typeconv_test

import (
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/typeconv"
)

func TestParse(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		v, err := typeconv.Parse[string]("hello")
		if err != nil || v != "hello" {
			t.Fatalf("got %q, %v", v, err)
		}
	})
	t.Run("bool", func(t *testing.T) {
		v, err := typeconv.Parse[bool]("true")
		if err != nil || !v {
			t.Fatalf("got %v, %v", v, err)
		}
	})
	t.Run("int", func(t *testing.T) {
		v, err := typeconv.Parse[int]("42")
		if err != nil || v != 42 {
			t.Fatalf("got %d, %v", v, err)
		}
	})
	t.Run("int8 out of range", func(t *testing.T) {
		if _, err := typeconv.Parse[int8]("999"); !errors.Is(err, typeconv.ErrSyntax) {
			t.Fatalf("want ErrSyntax, got %v", err)
		}
	})
	t.Run("float64", func(t *testing.T) {
		v, err := typeconv.Parse[float64]("3.14")
		if err != nil || v != 3.14 {
			t.Fatalf("got %v, %v", v, err)
		}
	})
	t.Run("duration", func(t *testing.T) {
		v, err := typeconv.Parse[time.Duration]("1h30m")
		if err != nil || v != 90*time.Minute {
			t.Fatalf("got %v, %v", v, err)
		}
	})
	t.Run("time RFC3339", func(t *testing.T) {
		v, err := typeconv.Parse[time.Time]("2026-07-01T12:00:00Z")
		if err != nil || !v.Equal(time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)) {
			t.Fatalf("got %v, %v", v, err)
		}
	})
	t.Run("syntax error wraps ErrSyntax", func(t *testing.T) {
		if _, err := typeconv.Parse[int]("nope"); !errors.Is(err, typeconv.ErrSyntax) {
			t.Fatalf("want ErrSyntax, got %v", err)
		}
	})
	t.Run("unsupported type", func(t *testing.T) {
		if _, err := typeconv.Parse[[]byte]("x"); !errors.Is(err, typeconv.ErrUnsupportedType) {
			t.Fatalf("want ErrUnsupportedType, got %v", err)
		}
	})
	t.Run("int16", func(t *testing.T) {
		v, err := typeconv.Parse[int16]("100")
		if err != nil || v != 100 {
			t.Fatalf("got %d, %v", v, err)
		}
	})
	t.Run("int32", func(t *testing.T) {
		v, err := typeconv.Parse[int32]("100")
		if err != nil || v != 100 {
			t.Fatalf("got %d, %v", v, err)
		}
	})
	t.Run("uint", func(t *testing.T) {
		v, err := typeconv.Parse[uint]("7")
		if err != nil || v != 7 {
			t.Fatalf("got %d, %v", v, err)
		}
	})
	t.Run("uint8", func(t *testing.T) {
		v, err := typeconv.Parse[uint8]("8")
		if err != nil || v != 8 {
			t.Fatalf("got %d, %v", v, err)
		}
	})
	t.Run("uint16", func(t *testing.T) {
		v, err := typeconv.Parse[uint16]("9")
		if err != nil || v != 9 {
			t.Fatalf("got %d, %v", v, err)
		}
	})
	t.Run("uint32", func(t *testing.T) {
		v, err := typeconv.Parse[uint32]("10")
		if err != nil || v != 10 {
			t.Fatalf("got %d, %v", v, err)
		}
	})
	t.Run("uint64", func(t *testing.T) {
		v, err := typeconv.Parse[uint64]("11")
		if err != nil || v != 11 {
			t.Fatalf("got %d, %v", v, err)
		}
	})
	t.Run("float32", func(t *testing.T) {
		v, err := typeconv.Parse[float32]("1.5")
		if err != nil || v != 1.5 {
			t.Fatalf("got %v, %v", v, err)
		}
	})
}

func TestParseIntHelperOverflow(t *testing.T) {
	if _, err := typeconv.ParseInt[int8]("128"); !errors.Is(err, typeconv.ErrSyntax) {
		t.Fatalf("want overflow ErrSyntax, got %v", err)
	}
	if v, err := typeconv.ParseInt[int8]("127"); err != nil || v != 127 {
		t.Fatalf("got %d, %v", v, err)
	}
	// Defined types are handled by the constraint helpers.
	type Port uint16
	if _, err := typeconv.ParseUint[Port]("70000"); !errors.Is(err, typeconv.ErrSyntax) {
		t.Fatalf("want overflow ErrSyntax, got %v", err)
	}
	if v, err := typeconv.ParseUint[Port]("8080"); err != nil || v != 8080 {
		t.Fatalf("got %d, %v", v, err)
	}
}

func TestFormat(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{"hi", "hi"},
		{true, "true"},
		{42, "42"},
		{int64(-7), "-7"},
		{int16(-3), "-3"},
		{int32(5), "5"},
		{uint(8), "8"},
		{uint8(9), "9"},
		{uint16(10), "10"},
		{uint32(11), "11"},
		{uint64(12), "12"},
		{3.5, "3.5"},
		{float32(1.5), "1.5"},
		{90 * time.Minute, "1h30m0s"},
		{time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC), "2026-07-01T12:00:00Z"},
	}
	for _, c := range cases {
		if got := typeconv.Format(c.in); got != c.want {
			t.Errorf("Format(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseBool(t *testing.T) {
	t.Run("true", func(t *testing.T) {
		v, err := typeconv.ParseBool("true")
		if err != nil || !v {
			t.Fatalf("got %v, %v", v, err)
		}
	})
	t.Run("0 is false", func(t *testing.T) {
		v, err := typeconv.ParseBool("0")
		if err != nil || v {
			t.Fatalf("got %v, %v", v, err)
		}
	})
	t.Run("invalid syntax", func(t *testing.T) {
		if _, err := typeconv.ParseBool("notbool"); !errors.Is(err, typeconv.ErrSyntax) {
			t.Fatalf("want ErrSyntax, got %v", err)
		}
	})
}

func TestParseFloat(t *testing.T) {
	t.Run("float64", func(t *testing.T) {
		v, err := typeconv.ParseFloat[float64]("2.5")
		if err != nil || v != 2.5 {
			t.Fatalf("got %v, %v", v, err)
		}
	})
	t.Run("float32", func(t *testing.T) {
		v, err := typeconv.ParseFloat[float32]("1.5")
		if err != nil || v != 1.5 {
			t.Fatalf("got %v, %v", v, err)
		}
	})
	t.Run("invalid syntax", func(t *testing.T) {
		if _, err := typeconv.ParseFloat[float64]("x"); !errors.Is(err, typeconv.ErrSyntax) {
			t.Fatalf("want ErrSyntax, got %v", err)
		}
	})
}

func TestParseDuration(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		v, err := typeconv.ParseDuration("500ms")
		if err != nil || v != 500*time.Millisecond {
			t.Fatalf("got %v, %v", v, err)
		}
	})
	t.Run("invalid syntax", func(t *testing.T) {
		if _, err := typeconv.ParseDuration("abc"); !errors.Is(err, typeconv.ErrSyntax) {
			t.Fatalf("want ErrSyntax, got %v", err)
		}
	})
}

func TestParseTime(t *testing.T) {
	t.Run("valid RFC3339", func(t *testing.T) {
		v, err := typeconv.ParseTime("2026-07-02T00:00:00Z")
		if err != nil || !v.Equal(time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("got %v, %v", v, err)
		}
	})
	t.Run("date-only is not RFC3339", func(t *testing.T) {
		if _, err := typeconv.ParseTime("2026-07-02"); !errors.Is(err, typeconv.ErrSyntax) {
			t.Fatalf("want ErrSyntax, got %v", err)
		}
	})
}

func TestParseFormatUintptr(t *testing.T) {
	// Parse and Format must cover the same scalar set (Format's "lossless
	// inverse of Parse" contract): uintptr is handled by both.
	v, err := typeconv.Parse[uintptr]("42")
	if err != nil || v != 42 {
		t.Fatalf("Parse[uintptr] = %d, %v", v, err)
	}
	if got := typeconv.Format(uintptr(42)); got != "42" {
		t.Fatalf("Format(uintptr) = %q", got)
	}
	round, err := typeconv.Parse[uintptr](typeconv.Format(uintptr(65535)))
	if err != nil || round != 65535 {
		t.Fatalf("round-trip = %d, %v", round, err)
	}
}

func TestParseSlice(t *testing.T) {
	t.Run("ints", func(t *testing.T) {
		v, err := typeconv.ParseSlice[int]("1, 2, 3", ",")
		if err != nil {
			t.Fatal(err)
		}
		if len(v) != 3 || v[0] != 1 || v[2] != 3 {
			t.Fatalf("got %v", v)
		}
	})
	t.Run("trailing sep and blanks dropped", func(t *testing.T) {
		v, err := typeconv.ParseSlice[string]("a, ,b,", ",")
		if err != nil {
			t.Fatal(err)
		}
		if len(v) != 2 || v[0] != "a" || v[1] != "b" {
			t.Fatalf("got %v", v)
		}
	})
	t.Run("empty input yields nil", func(t *testing.T) {
		v, err := typeconv.ParseSlice[int]("  ", ",")
		if err != nil || v != nil {
			t.Fatalf("got %v, %v", v, err)
		}
	})
	t.Run("bad element errors", func(t *testing.T) {
		if _, err := typeconv.ParseSlice[int]("1,x,3", ","); !errors.Is(err, typeconv.ErrSyntax) {
			t.Fatalf("want ErrSyntax, got %v", err)
		}
	})
}
