package sanitizer_test

import (
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/pkg/sanitizer"
)

func TestSanitizeStruct_BasicFields(t *testing.T) {
	type TestStruct struct {
		Email    string `sanitize:"trim,lower"`
		Name     string `sanitize:"trim,title"`
		Username string `sanitize:"trim,lower,alphanum"`
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
			input := tt.input
			err := sanitizer.SanitizeStruct(&input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if input.Email != tt.expected.Email {
				t.Errorf("Email: got %q, want %q", input.Email, tt.expected.Email)
			}
			if input.Name != tt.expected.Name {
				t.Errorf("Name: got %q, want %q", input.Name, tt.expected.Name)
			}
			if input.Username != tt.expected.Username {
				t.Errorf("Username: got %q, want %q", input.Username, tt.expected.Username)
			}
			if input.NoTag != tt.expected.NoTag {
				t.Errorf("NoTag: got %q, want %q", input.NoTag, tt.expected.NoTag)
			}
			if input.Skip != tt.expected.Skip {
				t.Errorf("Skip: got %q, want %q", input.Skip, tt.expected.Skip)
			}
		})
	}
}

func TestSanitizeStruct_NestedStructs(t *testing.T) {
	type Address struct {
		Street  string `sanitize:"trim,title"`
		City    string `sanitize:"trim,upper"`
		ZipCode string `sanitize:"trim,digits"`
	}

	type User struct {
		Name    string  `sanitize:"trim,title"`
		Address Address // Nested struct
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
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if input.Name != expected.Name {
		t.Errorf("Name: got %q, want %q", input.Name, expected.Name)
	}
	if input.Address.Street != expected.Address.Street {
		t.Errorf("Address.Street: got %q, want %q", input.Address.Street, expected.Address.Street)
	}
	if input.Address.City != expected.Address.City {
		t.Errorf("Address.City: got %q, want %q", input.Address.City, expected.Address.City)
	}
	if input.Address.ZipCode != expected.Address.ZipCode {
		t.Errorf("Address.ZipCode: got %q, want %q", input.Address.ZipCode, expected.Address.ZipCode)
	}
	if input.NoTag != expected.NoTag {
		t.Errorf("NoTag: got %q, want %q", input.NoTag, expected.NoTag)
	}
}

func TestSanitizeStruct_Pointers(t *testing.T) {
	type TestStruct struct {
		Name     *string `sanitize:"trim,lower"`
		Email    *string `sanitize:"trim,email"`
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
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if *input.Name != "john doe" {
		t.Errorf("Name: got %q, want %q", *input.Name, "john doe")
	}
	if *input.Email != "user@example.com" {
		t.Errorf("Email: got %q, want %q", *input.Email, "user@example.com")
	}
	if input.NilField != nil {
		t.Errorf("NilField: expected nil, got %v", input.NilField)
	}
}

func TestSanitizeStruct_Slices(t *testing.T) {
	type TestStruct struct {
		Tags     []string `sanitize:"trim,lower"`
		Keywords []string `sanitize:"trim,kebab"`
		NoTag    []string
	}

	input := TestStruct{
		Tags:     []string{"  GO  ", "  PROGRAMMING  ", "  WEB  "},
		Keywords: []string{"  Hello World  ", "  Test Case  "},
		NoTag:    []string{"  UNCHANGED  "},
	}

	err := sanitizer.SanitizeStruct(&input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedTags := []string{"go", "programming", "web"}
	for i, tag := range input.Tags {
		if tag != expectedTags[i] {
			t.Errorf("Tags[%d]: got %q, want %q", i, tag, expectedTags[i])
		}
	}

	expectedKeywords := []string{"hello-world", "test-case"}
	for i, keyword := range input.Keywords {
		if keyword != expectedKeywords[i] {
			t.Errorf("Keywords[%d]: got %q, want %q", i, keyword, expectedKeywords[i])
		}
	}

	if input.NoTag[0] != "  UNCHANGED  " {
		t.Errorf("NoTag[0]: got %q, want %q", input.NoTag[0], "  UNCHANGED  ")
	}
}

func TestSanitizeStruct_CompositeSanitizers(t *testing.T) {
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
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if input.Email != "user@example.com" {
		t.Errorf("Email: got %q, want %q", input.Email, "user@example.com")
	}
	if input.Username != "user123" {
		t.Errorf("Username: got %q, want %q", input.Username, "user123")
	}
	if input.Slug != "hello-world-example" {
		t.Errorf("Slug: got %q, want %q", input.Slug, "hello-world-example")
	}
	if input.Name != "JOHN DOE" {
		t.Errorf("Name: got %q, want %q", input.Name, "JOHN DOE")
	}
	if input.Text != "This is a test" {
		t.Errorf("Text: got %q, want %q", input.Text, "This is a test")
	}
	// SafeText should have HTML escaped
	if !strings.Contains(input.SafeText, "&lt;script&gt;") {
		t.Errorf("SafeText: expected HTML to be escaped, got %q", input.SafeText)
	}
}

func TestSanitizeStruct_MaxLength(t *testing.T) {
	type TestStruct struct {
		Short  string `sanitize:"trim,max:5"`
		Medium string `sanitize:"trim,max:10"`
		Long   string `sanitize:"trim,max:20"`
	}

	input := TestStruct{
		Short:  "This is a very long string",
		Medium: "This is another long string",
		Long:   "This is yet another very long string",
	}

	err := sanitizer.SanitizeStruct(&input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len([]rune(input.Short)) > 5 {
		t.Errorf("Short: length %d exceeds max 5", len([]rune(input.Short)))
	}
	if len([]rune(input.Medium)) > 10 {
		t.Errorf("Medium: length %d exceeds max 10", len([]rune(input.Medium)))
	}
	if len([]rune(input.Long)) > 20 {
		t.Errorf("Long: length %d exceeds max 20", len([]rune(input.Long)))
	}
}

func TestSanitizeStruct_CustomSanitizer(t *testing.T) {
	// Register a custom sanitizer
	sanitizer.RegisterSanitizer("reverse", func(s string) string {
		runes := []rune(s)
		for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
			runes[i], runes[j] = runes[j], runes[i]
		}
		return string(runes)
	})

	type TestStruct struct {
		Text string `sanitize:"trim,reverse"`
	}

	input := TestStruct{
		Text: "  hello  ",
	}

	err := sanitizer.SanitizeStruct(&input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if input.Text != "olleh" {
		t.Errorf("Text: got %q, want %q", input.Text, "olleh")
	}
}

func TestSanitizeStruct_MultipleSanitizers(t *testing.T) {
	type TestStruct struct {
		// Apply multiple sanitizers in sequence
		Data string `sanitize:"trim,lower,alphanum,max:10"`
	}

	input := TestStruct{
		Data: "  Hello-World_123!@#  ",
	}

	err := sanitizer.SanitizeStruct(&input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should be: trim -> lower -> alphanum -> max:10
	// "  Hello-World_123!@#  " -> "Hello-World_123!@#" -> "hello-world_123!@#" -> "helloworld123" -> "helloworld"
	if input.Data != "helloworld" {
		t.Errorf("Data: got %q, want %q", input.Data, "helloworld")
	}
}

func TestSanitizeStruct_Errors(t *testing.T) {
	// Test non-pointer
	var s struct{ Name string }
	err := sanitizer.SanitizeStruct(s)
	if err == nil || !strings.Contains(err.Error(), "pointer") {
		t.Errorf("expected pointer error, got: %v", err)
	}

	// Test non-struct pointer
	str := "not a struct"
	err = sanitizer.SanitizeStruct(&str)
	if err == nil || !strings.Contains(err.Error(), "struct") {
		t.Errorf("expected struct error, got: %v", err)
	}

	// Test nil pointer
	var nilPtr *struct{ Name string }
	err = sanitizer.SanitizeStruct(nilPtr)
	if err == nil {
		t.Error("expected error for nil pointer")
	}
}

func TestSanitizeStruct_PointerToStruct(t *testing.T) {
	type Inner struct {
		Value string `sanitize:"trim,upper"`
	}

	type Outer struct {
		Inner *Inner // Will be processed recursively even without tag
		Name  string `sanitize:"trim,lower"`
	}

	inner := &Inner{
		Value: "  hello  ",
	}

	input := Outer{
		Inner: inner,
		Name:  "  WORLD  ",
	}

	err := sanitizer.SanitizeStruct(&input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if input.Inner.Value != "HELLO" {
		t.Errorf("Inner.Value: got %q, want %q", input.Inner.Value, "HELLO")
	}
	if input.Name != "world" {
		t.Errorf("Name: got %q, want %q", input.Name, "world")
	}
}

func TestSanitizeStruct_UnexportedFields(t *testing.T) {
	type TestStruct struct {
		Public  string `sanitize:"trim,lower"`
		private string `sanitize:"trim,upper"` // Should be skipped
	}

	input := TestStruct{
		Public:  "  PUBLIC  ",
		private: "  private  ",
	}

	err := sanitizer.SanitizeStruct(&input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if input.Public != "public" {
		t.Errorf("Public: got %q, want %q", input.Public, "public")
	}
	// private field should remain unchanged
	if input.private != "  private  " {
		t.Errorf("private: should not be modified, got %q", input.private)
	}
}

func TestSanitizeStruct_EmptyTag(t *testing.T) {
	type TestStruct struct {
		Field1 string `sanitize:""`
		Field2 string `sanitize:",,,"`
		Field3 string `sanitize:"trim,,lower"`
	}

	input := TestStruct{
		Field1: "  UNCHANGED  ",
		Field2: "  UNCHANGED  ",
		Field3: "  CHANGED  ",
	}

	err := sanitizer.SanitizeStruct(&input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if input.Field1 != "  UNCHANGED  " {
		t.Errorf("Field1: should not be modified, got %q", input.Field1)
	}
	if input.Field2 != "  UNCHANGED  " {
		t.Errorf("Field2: should not be modified, got %q", input.Field2)
	}
	if input.Field3 != "changed" {
		t.Errorf("Field3: got %q, want %q", input.Field3, "changed")
	}
}

// Benchmark to ensure performance is reasonable
func BenchmarkSanitizeStruct_Tags(b *testing.B) {
	type TestStruct struct {
		Email    string `sanitize:"trim,lower,email"`
		Name     string `sanitize:"trim,title"`
		Username string `sanitize:"trim,lower,alphanum"`
		Bio      string `sanitize:"trim,strip_html,max:500"`
		Website  string `sanitize:"trim,url"`
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
		test := input // Copy struct
		_ = sanitizer.SanitizeStruct(&test)
	}
}
