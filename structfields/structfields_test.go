package structfields_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/structfields"
)

type walkSample struct {
	Name    string `env:"NAME,required"`
	Age     int    `env:"AGE"`
	Ignored string `env:"-"`
	NoTag   bool
	//nolint:unused // present so Walk's "skip unexported fields" path is exercised
	private string // unexported: never visited
}

func TestWalk_VisitsExportedFieldsWithParsedTags(t *testing.T) {
	var s walkSample
	var got []structfields.Field
	err := structfields.Walk(&s, "env", func(f structfields.Field) error {
		got = append(got, f)
		return nil
	})
	require.NoError(t, err)

	require.Len(t, got, 4, "4 exported fields, unexported private skipped")

	assert.Equal(t, "Name", got[0].Name)
	assert.Equal(t, "NAME", got[0].Tag.Name)
	assert.Equal(t, []string{"required"}, got[0].Tag.Options)
	assert.True(t, got[0].Tag.HasOption("required"))
	assert.Equal(t, "NAME,required", got[0].Tag.Raw)

	assert.Equal(t, "Age", got[1].Name)
	assert.Equal(t, "AGE", got[1].Tag.Name)

	assert.Equal(t, "Ignored", got[2].Name)
	assert.True(t, got[2].Tag.Ignored())

	assert.Equal(t, "NoTag", got[3].Name)
	assert.Equal(t, "", got[3].Tag.Name, "absent tag yields empty Name")
	assert.Nil(t, got[3].Tag.Options)
}

func TestWalk_PointerFieldsAreSettable(t *testing.T) {
	var s walkSample
	err := structfields.Walk(&s, "env", func(f structfields.Field) error {
		switch f.Name {
		case "Name":
			return f.Set("hello")
		case "Age":
			return f.Set(42)
		default:
			return nil
		}
	})
	require.NoError(t, err)
	assert.Equal(t, "hello", s.Name)
	assert.Equal(t, 42, s.Age)
}

func TestWalk_SetConvertsAssignableTypes(t *testing.T) {
	type nums struct {
		N int64
	}
	var s nums
	err := structfields.Walk(&s, "env", func(f structfields.Field) error {
		return f.Set(int(7)) // int convertible to int64
	})
	require.NoError(t, err)
	assert.Equal(t, int64(7), s.N)
}

func TestWalk_SetTypeMismatchReturnsError(t *testing.T) {
	var s walkSample
	err := structfields.Walk(&s, "env", func(f structfields.Field) error {
		if f.Name == "Age" {
			return f.Set("not-a-number")
		}
		return nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "structfields:")
	assert.Contains(t, err.Error(), "Age")
}

func TestWalk_ValueStructIsReadOnly(t *testing.T) {
	s := walkSample{Name: "orig"}
	err := structfields.Walk(s, "env", func(f structfields.Field) error {
		if f.Name == "Name" {
			assert.False(t, f.Value.CanSet(), "value-struct field is not settable")
			setErr := f.Set("mutated")
			assert.True(t, errors.Is(setErr, structfields.ErrNotSettable))
			return setErr
		}
		return nil
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, structfields.ErrNotSettable))
	assert.Equal(t, "orig", s.Name, "value struct not mutated")
}

func TestWalk_ValueReflectsField(t *testing.T) {
	s := walkSample{Age: 9}
	err := structfields.Walk(s, "env", func(f structfields.Field) error {
		if f.Name == "Age" {
			assert.Equal(t, reflect.Int, f.Value.Kind())
			assert.Equal(t, int64(9), f.Value.Int())
		}
		return nil
	})
	require.NoError(t, err)
}

func TestWalk_PropagatesCallbackError(t *testing.T) {
	sentinel := errors.New("stop")
	var visited int
	err := structfields.Walk(&walkSample{}, "env", func(f structfields.Field) error {
		visited++
		return sentinel
	})
	assert.Equal(t, 1, visited, "traversal stops on first callback error")
	assert.True(t, errors.Is(err, sentinel))
}

func TestWalk_RejectsNonStruct(t *testing.T) {
	cases := []struct {
		name string
		in   any
	}{
		{"int", 42},
		{"string", "hello"},
		{"slice", []int{1, 2}},
		{"map", map[string]int{"a": 1}},
		{"nil interface", nil},
		{"pointer to int", new(int)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := structfields.Walk(c.in, "env", func(structfields.Field) error {
				t.Fatal("callback must not run for non-struct input")
				return nil
			})
			assert.True(t, errors.Is(err, structfields.ErrNotStruct), "want ErrNotStruct for %s", c.name)
		})
	}
}

func TestWalk_RejectsNilStructPointer(t *testing.T) {
	var s *walkSample
	err := structfields.Walk(s, "env", func(structfields.Field) error {
		t.Fatal("callback must not run for nil pointer")
		return nil
	})
	assert.True(t, errors.Is(err, structfields.ErrNotStruct))
}

func TestWalk_EmptyStructVisitsNothing(t *testing.T) {
	type empty struct{}
	var count int
	err := structfields.Walk(&empty{}, "env", func(structfields.Field) error {
		count++
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

type EmbeddedInner struct {
	Inner string `env:"INNER"`
}

type embeddedOuter struct {
	EmbeddedInner        // exported anonymous embedded struct
	Outer         string `env:"OUTER"`
}

func TestWalk_ShallowEmbeddedStructNotFlattened(t *testing.T) {
	var s embeddedOuter
	var names []string
	err := structfields.Walk(&s, "env", func(f structfields.Field) error {
		names = append(names, f.Name)
		return nil
	})
	require.NoError(t, err)
	// Shallow: the embedded struct is a single field named after its type,
	// NOT flattened into Inner. Outer is a normal field.
	assert.Equal(t, []string{"EmbeddedInner", "Outer"}, names)
}

func TestWalk_SetSliceToArrayShorterSliceReturnsError(t *testing.T) {
	// reflect reports []int convertible to [4]int, but Convert PANICS when the
	// slice is SHORTER than the array. Set must translate that into an error,
	// never panic.
	t.Run("array", func(t *testing.T) {
		type T struct{ Arr [4]int }
		var s T
		var setErr error
		require.NotPanics(t, func() {
			err := structfields.Walk(&s, "env", func(f structfields.Field) error {
				setErr = f.Set([]int{1, 2})
				return setErr
			})
			require.Error(t, err)
		})
		require.Error(t, setErr)
		assert.Contains(t, setErr.Error(), "structfields:")
		assert.Contains(t, setErr.Error(), "Arr")
		assert.Equal(t, [4]int{}, s.Arr, "array left unset on error")
	})

	t.Run("pointer to array", func(t *testing.T) {
		type T struct{ P *[3]byte }
		var setErr error
		require.NotPanics(t, func() {
			err := structfields.Walk(&T{}, "env", func(f structfields.Field) error {
				setErr = f.Set([]byte{1, 2})
				return setErr
			})
			require.Error(t, err)
		})
		require.Error(t, setErr)
		assert.Contains(t, setErr.Error(), "structfields:")
		assert.Contains(t, setErr.Error(), "P")
	})
}

func TestWalk_SetSliceToArrayExactAndLongerSliceSucceeds(t *testing.T) {
	// A slice equal to or LONGER than the array is a VALID slice->array
	// conversion (Go takes the first N elements). Set must succeed, not reject.
	t.Run("array exact length", func(t *testing.T) {
		type T struct{ Arr [4]int }
		var s T
		require.NotPanics(t, func() {
			err := structfields.Walk(&s, "env", func(f structfields.Field) error {
				return f.Set([]int{1, 2, 3, 4})
			})
			require.NoError(t, err)
		})
		assert.Equal(t, [4]int{1, 2, 3, 4}, s.Arr)
	})

	t.Run("array longer slice", func(t *testing.T) {
		type T struct{ Arr [4]int }
		var s T
		require.NotPanics(t, func() {
			err := structfields.Walk(&s, "env", func(f structfields.Field) error {
				return f.Set([]int{1, 2, 3, 4, 5})
			})
			require.NoError(t, err)
		})
		assert.Equal(t, [4]int{1, 2, 3, 4}, s.Arr, "takes first N elements")
	})

	t.Run("pointer to array exact length", func(t *testing.T) {
		type T struct{ P *[3]byte }
		var s T
		require.NotPanics(t, func() {
			err := structfields.Walk(&s, "env", func(f structfields.Field) error {
				return f.Set([]byte{1, 2, 3})
			})
			require.NoError(t, err)
		})
		require.NotNil(t, s.P)
		assert.Equal(t, [3]byte{1, 2, 3}, *s.P)
	})

	t.Run("pointer to array longer slice", func(t *testing.T) {
		type T struct{ P *[3]byte }
		var s T
		require.NotPanics(t, func() {
			err := structfields.Walk(&s, "env", func(f structfields.Field) error {
				return f.Set([]byte{1, 2, 3, 4})
			})
			require.NoError(t, err)
		})
		require.NotNil(t, s.P)
		assert.Equal(t, [3]byte{1, 2, 3}, *s.P, "takes first N bytes")
	})
}

func TestWalk_EmbeddedFieldReWalkable(t *testing.T) {
	// A caller needing recursion re-Walks the embedded field's value.
	var s embeddedOuter
	var inner []string
	err := structfields.Walk(&s, "env", func(f structfields.Field) error {
		if f.Name == "EmbeddedInner" {
			return structfields.Walk(f.Value.Addr().Interface(), "env", func(g structfields.Field) error {
				inner = append(inner, g.Name)
				return g.Set("nested")
			})
		}
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"Inner"}, inner)
	assert.Equal(t, "nested", s.Inner, "re-Walk on embedded field is settable")
}
