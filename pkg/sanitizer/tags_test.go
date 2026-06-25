package sanitizer_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/sanitizer"
)

func TestSanitizeStruct_BasicFields(t *testing.T) {
	t.Parallel()

	type TestStruct struct {
		Email    string `sanitize:"trim;lower"`
		Name     string `sanitize:"trim;title"`
		Username string `sanitize:"trim;lower;alphanum"`
		NoTag    string
		Skip     string `sanitize:"-"`
	}

	tests := []struct {
		name     string
		input    TestStruct
		expected TestStruct
	}{
		{
			name: "basic sanitization",
			input: TestStruct{
				Email:    "  USER@EXAMPLE.COM  ",
				Name:     "  john doe  ",
				Username: "  User_123!  ",
				NoTag:    "  not sanitized  ",
				Skip:     "  skip this  ",
			},
			expected: TestStruct{
				Email:    "user@example.com",
				Name:     "JOHN DOE", // strings.ToTitle converts to uppercase
				Username: "user123",
				NoTag:    "  not sanitized  ",
				Skip:     "  skip this  ",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			input := tt.input
			err := sanitizer.SanitizeStruct(&input)
			require.NoError(t, err)

			require.Equal(t, tt.expected.Email, input.Email)
			require.Equal(t, tt.expected.Name, input.Name)
			require.Equal(t, tt.expected.Username, input.Username)
			require.Equal(t, tt.expected.NoTag, input.NoTag)
			require.Equal(t, tt.expected.Skip, input.Skip)
		})
	}
}

func TestSanitizeStruct_NestedStructs(t *testing.T) {
	t.Parallel()

	type Address struct {
		Street  string `sanitize:"trim;title"`
		City    string `sanitize:"trim;upper"`
		ZipCode string `sanitize:"trim;digits"`
	}

	type User struct {
		Name    string `sanitize:"trim;title"`
		Address Address
		NoTag   string
	}

	input := User{
		Name: "  jane smith  ",
		Address: Address{
			Street:  "  123 main street  ",
			City:    "  new york  ",
			ZipCode: "  12345-6789  ",
		},
		NoTag: "  unchanged  ",
	}

	expected := User{
		Name: "JANE SMITH", // strings.ToTitle converts to uppercase
		Address: Address{
			Street:  "123 MAIN STREET", // strings.ToTitle converts to uppercase
			City:    "NEW YORK",
			ZipCode: "123456789",
		},
		NoTag: "  unchanged  ",
	}

	err := sanitizer.SanitizeStruct(&input)
	require.NoError(t, err)

	require.Equal(t, expected.Name, input.Name)
	require.Equal(t, expected.Address.Street, input.Address.Street)
	require.Equal(t, expected.Address.City, input.Address.City)
	require.Equal(t, expected.Address.ZipCode, input.Address.ZipCode)
	require.Equal(t, expected.NoTag, input.NoTag)
}

func TestSanitizeStruct_Pointers(t *testing.T) {
	t.Parallel()

	type TestStruct struct {
		Name     *string `sanitize:"trim;lower"`
		Email    *string `sanitize:"trim;email"`
		NilField *string `sanitize:"trim"`
	}

	name := "  JOHN DOE  "
	email := "  USER@EXAMPLE.COM  "

	input := TestStruct{
		Name:     &name,
		Email:    &email,
		NilField: nil,
	}

	err := sanitizer.SanitizeStruct(&input)
	require.NoError(t, err)

	require.Equal(t, "john doe", *input.Name)
	require.Equal(t, "user@example.com", *input.Email)
	require.Nil(t, input.NilField)
}

func TestSanitizeStruct_Slices(t *testing.T) {
	t.Parallel()

	type TestStruct struct {
		Tags     []string `sanitize:"trim;lower"`
		Keywords []string `sanitize:"trim;kebab"`
		NoTag    []string
	}

	input := TestStruct{
		Tags:     []string{"  GO  ", "  PROGRAMMING  ", "  WEB  "},
		Keywords: []string{"  Hello World  ", "  Test Case  "},
		NoTag:    []string{"  UNCHANGED  "},
	}

	err := sanitizer.SanitizeStruct(&input)
	require.NoError(t, err)

	require.Equal(t, []string{"go", "programming", "web"}, input.Tags)
	require.Equal(t, []string{"hello-world", "test-case"}, input.Keywords)
	require.Equal(t, "  UNCHANGED  ", input.NoTag[0])
}

func TestSanitizeStruct_CompositeSanitizers(t *testing.T) {
	t.Parallel()

	type TestStruct struct {
		Email    string `sanitize:"email"`
		Username string `sanitize:"username"`
		Slug     string `sanitize:"slug"`
		Name     string `sanitize:"name"`
		Text     string `sanitize:"text"`
		SafeText string `sanitize:"safe_text"`
	}

	input := TestStruct{
		Email:    "  USER@EXAMPLE.COM  ",
		Username: "  User_123!@#  ",
		Slug:     "  Hello World Example  ",
		Name:     "  john   doe  ",
		Text:     "  This   is   a   test  ",
		SafeText: "  <script>alert('XSS')</script>  ",
	}

	err := sanitizer.SanitizeStruct(&input)
	require.NoError(t, err)

	require.Equal(t, "user@example.com", input.Email)
	require.Equal(t, "user123", input.Username)
	require.Equal(t, "hello-world-example", input.Slug)
	require.Equal(t, "JOHN DOE", input.Name)
	require.Equal(t, "This is a test", input.Text)
	// SafeText should have HTML escaped.
	require.Contains(t, input.SafeText, "&lt;script&gt;")
}

func TestSanitizeStruct_MaxLength(t *testing.T) {
	t.Parallel()

	type TestStruct struct {
		Short  string `sanitize:"trim;max:5"`
		Medium string `sanitize:"trim;max:10"`
		Long   string `sanitize:"trim;max:20"`
	}

	input := TestStruct{
		Short:  "This is a very long string",
		Medium: "This is another long string",
		Long:   "This is yet another very long string",
	}

	err := sanitizer.SanitizeStruct(&input)
	require.NoError(t, err)

	require.LessOrEqual(t, len([]rune(input.Short)), 5)
	require.LessOrEqual(t, len([]rune(input.Medium)), 10)
	require.LessOrEqual(t, len([]rune(input.Long)), 20)
}

func TestSanitizeStruct_CustomSanitizer(t *testing.T) {
	t.Parallel()

	// Register a custom sanitizer. The registry is process-global, so use a
	// unique name and remove it afterwards to avoid leaking state into other
	// tests that run in parallel.
	const customName = "reverse_tags_test"
	sanitizer.RegisterSanitizer(customName, func(s string) string {
		runes := []rune(s)
		for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
			runes[i], runes[j] = runes[j], runes[i]
		}
		return string(runes)
	})
	t.Cleanup(func() {
		sanitizer.UnregisterSanitizer(customName)
	})

	type TestStruct struct {
		Text string `sanitize:"trim;reverse_tags_test"`
	}

	input := TestStruct{
		Text: "  hello  ",
	}

	err := sanitizer.SanitizeStruct(&input)
	require.NoError(t, err)

	require.Equal(t, "olleh", input.Text)
}

func TestSanitizeStruct_MultipleSanitizers(t *testing.T) {
	t.Parallel()

	type TestStruct struct {
		// Apply multiple sanitizers in sequence.
		Data string `sanitize:"trim;lower;alphanum;max:10"`
	}

	input := TestStruct{
		Data: "  Hello-World_123!@#  ",
	}

	err := sanitizer.SanitizeStruct(&input)
	require.NoError(t, err)

	// Should be: trim -> lower -> alphanum -> max:10
	// "  Hello-World_123!@#  " -> "Hello-World_123!@#" -> "hello-world_123!@#" -> "helloworld123" -> "helloworld"
	require.Equal(t, "helloworld", input.Data)
}

func TestSanitizeStruct_Errors(t *testing.T) {
	t.Parallel()

	t.Run("non-pointer", func(t *testing.T) {
		t.Parallel()

		var s struct{ Name string }
		err := sanitizer.SanitizeStruct(s)
		require.Error(t, err)
		require.Contains(t, err.Error(), "pointer")
	})

	t.Run("non-struct pointer", func(t *testing.T) {
		t.Parallel()

		str := "not a struct"
		err := sanitizer.SanitizeStruct(&str)
		require.Error(t, err)
		require.Contains(t, err.Error(), "struct")
	})

	t.Run("nil pointer", func(t *testing.T) {
		t.Parallel()

		var nilPtr *struct{ Name string }
		err := sanitizer.SanitizeStruct(nilPtr)
		require.Error(t, err)
	})
}

func TestSanitizeStruct_PointerToStruct(t *testing.T) {
	t.Parallel()

	type Inner struct {
		Value string `sanitize:"trim;upper"`
	}

	type Outer struct {
		Inner *Inner // Will be processed recursively even without tag.
		Name  string `sanitize:"trim;lower"`
	}

	inner := &Inner{
		Value: "  hello  ",
	}

	input := Outer{
		Inner: inner,
		Name:  "  WORLD  ",
	}

	err := sanitizer.SanitizeStruct(&input)
	require.NoError(t, err)

	require.Equal(t, "HELLO", input.Inner.Value)
	require.Equal(t, "world", input.Name)
}

func TestSanitizeStruct_UnexportedFields(t *testing.T) {
	t.Parallel()

	type TestStruct struct {
		Public  string `sanitize:"trim;lower"`
		private string `sanitize:"trim;upper"` // Should be skipped.
	}

	input := TestStruct{
		Public:  "  PUBLIC  ",
		private: "  private  ",
	}

	err := sanitizer.SanitizeStruct(&input)
	require.NoError(t, err)

	require.Equal(t, "public", input.Public)
	// private field should remain unchanged.
	require.Equal(t, "  private  ", input.private)
}

func TestSanitizeStruct_EmptyTag(t *testing.T) {
	t.Parallel()

	type TestStruct struct {
		Field1 string `sanitize:""`
		Field2 string `sanitize:";;;"`
		Field3 string `sanitize:"trim;;lower"`
	}

	input := TestStruct{
		Field1: "  UNCHANGED  ",
		Field2: "  UNCHANGED  ",
		Field3: "  CHANGED  ",
	}

	err := sanitizer.SanitizeStruct(&input)
	require.NoError(t, err)

	require.Equal(t, "  UNCHANGED  ", input.Field1)
	require.Equal(t, "  UNCHANGED  ", input.Field2)
	require.Equal(t, "changed", input.Field3)
}

// Benchmark to ensure performance is reasonable.
func BenchmarkSanitizeStruct_Tags(b *testing.B) {
	type TestStruct struct {
		Email    string `sanitize:"trim;lower;email"`
		Name     string `sanitize:"trim;title"`
		Username string `sanitize:"trim;lower;alphanum"`
		Bio      string `sanitize:"trim;strip_html;max:500"`
		Website  string `sanitize:"trim;url"`
	}

	input := TestStruct{
		Email:    "  USER@EXAMPLE.COM  ",
		Name:     "  john doe  ",
		Username: "  User_123!  ",
		Bio:      "  <p>This is my bio with <b>HTML</b></p>  ",
		Website:  "  example.com  ",
	}

	b.ResetTimer()
	for b.Loop() {
		test := input // Copy struct.
		_ = sanitizer.SanitizeStruct(&test)
	}
}
