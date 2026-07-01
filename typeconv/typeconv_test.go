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
		{uint(8), "8"},
		{3.5, "3.5"},
		{90 * time.Minute, "1h30m0s"},
		{time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC), "2026-07-01T12:00:00Z"},
	}
	for _, c := range cases {
		if got := typeconv.Format(c.in); got != c.want {
			t.Errorf("Format(%v) = %q, want %q", c.in, got, c.want)
		}
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
