package validator_test

import (
	"reflect"
	"testing"

	"github.com/dmitrymomot/forge/pkg/validator"
)

func TestValidateStruct_BasicFields(t *testing.T) {
	type TestStruct struct {
		Email    string `validate:"required;email"`
		Username string `validate:"required;min:3;max:20;alphanum"`
		Age      int    `validate:"required;min:18;max:150"`
		NoTag    string
		Skip     string `validate:"-"`
	}

	tests := []struct {
		name      string
		input     TestStruct
		wantError bool
		errCount  int
		errFields []string
	}{
		{
			name: "valid data",
			input: TestStruct{
				Email:    "user@example.com",
				Username: "john123",
				Age:      25,
				NoTag:    "",
				Skip:     "",
			},
			wantError: false,
		},
		{
			name: "missing required fields",
			input: TestStruct{
				Email:    "",
				Username: "",
				Age:      0,
				NoTag:    "",
				Skip:     "skip this",
			},
			wantError: true,
			errCount:  -1, // Don't check exact count, username has multiple rules that fail
			errFields: []string{"Email", "Username", "Age"},
		},
		{
			name: "invalid email",
			input: TestStruct{
				Email:    "not-an-email",
				Username: "john123",
				Age:      25,
			},
			wantError: true,
			errCount:  1,
			errFields: []string{"Email"},
		},
		{
			name: "username too short",
			input: TestStruct{
				Email:    "user@example.com",
				Username: "ab",
				Age:      25,
			},
			wantError: true,
			errCount:  1,
			errFields: []string{"Username"},
		},
		{
			name: "username too long",
			input: TestStruct{
				Email:    "user@example.com",
				Username: "verylongusernamethatexceedstwentycharacters",
				Age:      25,
			},
			wantError: true,
			errCount:  1,
			errFields: []string{"Username"},
		},
		{
			name: "username not alphanumeric",
			input: TestStruct{
				Email:    "user@example.com",
				Username: "john_123",
				Age:      25,
			},
			wantError: true,
			errCount:  1,
			errFields: []string{"Username"},
		},
		{
			name: "age below minimum",
			input: TestStruct{
				Email:    "user@example.com",
				Username: "john123",
				Age:      17,
			},
			wantError: true,
			errCount:  1,
			errFields: []string{"Age"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateStruct(&tt.input)

			if tt.wantError {
				if err == nil {
					t.Error("expected error but got none")
					return
				}

				validationErrors := validator.ExtractValidationErrors(err)
				if validationErrors == nil {
					t.Error("expected ValidationErrors but got different error type")
					return
				}

				if tt.errCount > 0 && len(validationErrors) != tt.errCount {
					t.Errorf("expected %d errors, got %d", tt.errCount, len(validationErrors))
				}

				for _, field := range tt.errFields {
					if !validationErrors.Has(field) {
						t.Errorf("expected error for field %s", field)
					}
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestValidateStruct_NestedStructs(t *testing.T) {
	type Address struct {
		Street  string `validate:"required;min:5"`
		City    string `validate:"required"`
		ZipCode string `validate:"required;len:5;numeric"`
	}

	type User struct {
		Name    string  `validate:"required"`
		Address Address // Nested struct, always validated
		NoTag   string
	}

	tests := []struct {
		name      string
		input     User
		wantError bool
		errFields []string
	}{
		{
			name: "valid nested data",
			input: User{
				Name: "John Doe",
				Address: Address{
					Street:  "123 Main Street",
					City:    "New York",
					ZipCode: "12345",
				},
				NoTag: "unchanged",
			},
			wantError: false,
		},
		{
			name: "invalid nested fields",
			input: User{
				Name: "John Doe",
				Address: Address{
					Street:  "123", // Too short
					City:    "",    // Required
					ZipCode: "abc", // Not numeric, wrong length
				},
			},
			wantError: true,
			errFields: []string{"Address.Street", "Address.City", "Address.ZipCode"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateStruct(&tt.input)

			if tt.wantError {
				if err == nil {
					t.Error("expected error but got none")
					return
				}

				validationErrors := validator.ExtractValidationErrors(err)
				for _, field := range tt.errFields {
					if !validationErrors.Has(field) {
						t.Errorf("expected error for field %s", field)
					}
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestValidateStruct_Delimiters(t *testing.T) {
	type TestStruct struct {
		Price    float64  `validate:"required;between:0.01,999.99"`
		Status   string   `validate:"required;in:active,pending,disabled"`
		Tags     []string `validate:"min:1;max:5"`
		Username string   `validate:"required;min:3;max:20;alphanum"`
	}

	tests := []struct {
		name      string
		input     TestStruct
		wantError bool
		errFields []string
	}{
		{
			name: "valid with complex delimiters",
			input: TestStruct{
				Price:    99.99,
				Status:   "active",
				Tags:     []string{"tag1", "tag2"},
				Username: "user123",
			},
			wantError: false,
		},
		{
			name: "price out of range",
			input: TestStruct{
				Price:    1000.00,
				Status:   "active",
				Tags:     []string{"tag1"},
				Username: "user123",
			},
			wantError: true,
			errFields: []string{"Price"},
		},
		{
			name: "status not in allowed values",
			input: TestStruct{
				Price:    50.00,
				Status:   "invalid",
				Tags:     []string{"tag1"},
				Username: "user123",
			},
			wantError: true,
			errFields: []string{"Status"},
		},
		{
			name: "too many tags",
			input: TestStruct{
				Price:    50.00,
				Status:   "active",
				Tags:     []string{"tag1", "tag2", "tag3", "tag4", "tag5", "tag6"},
				Username: "user123",
			},
			wantError: true,
			errFields: []string{"Tags"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateStruct(&tt.input)

			if tt.wantError {
				if err == nil {
					t.Error("expected error but got none")
					return
				}

				validationErrors := validator.ExtractValidationErrors(err)
				for _, field := range tt.errFields {
					if !validationErrors.Has(field) {
						t.Errorf("expected error for field %s", field)
					}
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestValidateStruct_NumericValidation(t *testing.T) {
	type TestStruct struct {
		Positive int     `validate:"positive"`
		Negative int     `validate:"negative"`
		Zero     int     `validate:"zero"`
		NonZero  int     `validate:"nonzero"`
		Float    float64 `validate:"between:1.5,2.5"`
	}

	tests := []struct {
		name      string
		input     TestStruct
		wantError bool
		errFields []string
	}{
		{
			name: "valid numeric values",
			input: TestStruct{
				Positive: 10,
				Negative: -5,
				Zero:     0,
				NonZero:  1,
				Float:    2.0,
			},
			wantError: false,
		},
		{
			name: "invalid positive",
			input: TestStruct{
				Positive: -1,
				Negative: -5,
				Zero:     0,
				NonZero:  1,
				Float:    2.0,
			},
			wantError: true,
			errFields: []string{"Positive"},
		},
		{
			name: "invalid negative",
			input: TestStruct{
				Positive: 10,
				Negative: 5,
				Zero:     0,
				NonZero:  1,
				Float:    2.0,
			},
			wantError: true,
			errFields: []string{"Negative"},
		},
		{
			name: "invalid zero",
			input: TestStruct{
				Positive: 10,
				Negative: -5,
				Zero:     1,
				NonZero:  1,
				Float:    2.0,
			},
			wantError: true,
			errFields: []string{"Zero"},
		},
		{
			name: "invalid nonzero",
			input: TestStruct{
				Positive: 10,
				Negative: -5,
				Zero:     0,
				NonZero:  0,
				Float:    2.0,
			},
			wantError: true,
			errFields: []string{"NonZero"},
		},
		{
			name: "float out of range",
			input: TestStruct{
				Positive: 10,
				Negative: -5,
				Zero:     0,
				NonZero:  1,
				Float:    3.0,
			},
			wantError: true,
			errFields: []string{"Float"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateStruct(&tt.input)

			if tt.wantError {
				if err == nil {
					t.Error("expected error but got none")
					return
				}

				validationErrors := validator.ExtractValidationErrors(err)
				for _, field := range tt.errFields {
					if !validationErrors.Has(field) {
						t.Errorf("expected error for field %s", field)
					}
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestValidateStruct_StringValidation(t *testing.T) {
	type TestStruct struct {
		URL      string `validate:"url"`
		Phone    string `validate:"phone"`
		UUID     string `validate:"uuid"`
		Contains string `validate:"contains:test"`
		Prefix   string `validate:"prefix:hello"`
		Suffix   string `validate:"suffix:world"`
	}

	tests := []struct {
		name      string
		input     TestStruct
		wantError bool
		errFields []string
	}{
		{
			name: "valid string formats",
			input: TestStruct{
				URL:      "https://example.com",
				Phone:    "+12345678901",
				UUID:     "550e8400-e29b-41d4-a716-446655440000",
				Contains: "this is a test string",
				Prefix:   "hello world",
				Suffix:   "hello world",
			},
			wantError: false,
		},
		{
			name: "invalid URL",
			input: TestStruct{
				URL:      "not a url",
				Phone:    "+12345678901",
				UUID:     "550e8400-e29b-41d4-a716-446655440000",
				Contains: "this is a test string",
				Prefix:   "hello world",
				Suffix:   "hello world",
			},
			wantError: true,
			errFields: []string{"URL"},
		},
		{
			name: "invalid phone",
			input: TestStruct{
				URL:      "https://example.com",
				Phone:    "123",
				UUID:     "550e8400-e29b-41d4-a716-446655440000",
				Contains: "this is a test string",
				Prefix:   "hello world",
				Suffix:   "hello world",
			},
			wantError: true,
			errFields: []string{"Phone"},
		},
		{
			name: "invalid UUID",
			input: TestStruct{
				URL:      "https://example.com",
				Phone:    "+12345678901",
				UUID:     "not-a-uuid",
				Contains: "this is a test string",
				Prefix:   "hello world",
				Suffix:   "hello world",
			},
			wantError: true,
			errFields: []string{"UUID"},
		},
		{
			name: "missing contains",
			input: TestStruct{
				URL:      "https://example.com",
				Phone:    "+12345678901",
				UUID:     "550e8400-e29b-41d4-a716-446655440000",
				Contains: "this is a string",
				Prefix:   "hello world",
				Suffix:   "hello world",
			},
			wantError: true,
			errFields: []string{"Contains"},
		},
		{
			name: "wrong prefix",
			input: TestStruct{
				URL:      "https://example.com",
				Phone:    "+12345678901",
				UUID:     "550e8400-e29b-41d4-a716-446655440000",
				Contains: "this is a test string",
				Prefix:   "goodbye world",
				Suffix:   "hello world",
			},
			wantError: true,
			errFields: []string{"Prefix"},
		},
		{
			name: "wrong suffix",
			input: TestStruct{
				URL:      "https://example.com",
				Phone:    "+12345678901",
				UUID:     "550e8400-e29b-41d4-a716-446655440000",
				Contains: "this is a test string",
				Prefix:   "hello world",
				Suffix:   "hello earth",
			},
			wantError: true,
			errFields: []string{"Suffix"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateStruct(&tt.input)

			if tt.wantError {
				if err == nil {
					t.Error("expected error but got none")
					return
				}

				validationErrors := validator.ExtractValidationErrors(err)
				for _, field := range tt.errFields {
					if !validationErrors.Has(field) {
						t.Errorf("expected error for field %s", field)
					}
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestValidateStruct_Pointers(t *testing.T) {
	type TestStruct struct {
		Required *string `validate:"required"`
		Optional *string `validate:"email"`
		NilField *string `validate:"min:5"`
	}

	validEmail := "test@example.com"
	invalidEmail := "not-an-email"
	requiredValue := "value"

	tests := []struct {
		name      string
		input     TestStruct
		wantError bool
		errFields []string
	}{
		{
			name: "valid pointers",
			input: TestStruct{
				Required: &requiredValue,
				Optional: &validEmail,
				NilField: nil,
			},
			wantError: false,
		},
		{
			name: "nil required pointer",
			input: TestStruct{
				Required: nil,
				Optional: &validEmail,
				NilField: nil,
			},
			wantError: true,
			errFields: []string{"Required"},
		},
		{
			name: "invalid optional pointer",
			input: TestStruct{
				Required: &requiredValue,
				Optional: &invalidEmail,
				NilField: nil,
			},
			wantError: true,
			errFields: []string{"Optional"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateStruct(&tt.input)

			if tt.wantError {
				if err == nil {
					t.Error("expected error but got none")
					return
				}

				validationErrors := validator.ExtractValidationErrors(err)
				for _, field := range tt.errFields {
					if !validationErrors.Has(field) {
						t.Errorf("expected error for field %s", field)
					}
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestValidateStruct_CustomValidator(t *testing.T) {
	// Register a custom validator
	validator.RegisterValidator("even", func(field string, value reflect.Value, params []string) validator.Rule {
		return validator.Rule{
			Check: func() bool {
				if value.Kind() == reflect.Int || value.Kind() == reflect.Int64 {
					return value.Int()%2 == 0
				}
				return true
			},
			Error: validator.ValidationError{
				Field:   field,
				Message: "must be an even number",
			},
		}
	})

	type TestStruct struct {
		Number int `validate:"even"`
	}

	tests := []struct {
		name      string
		input     TestStruct
		wantError bool
	}{
		{
			name:      "even number",
			input:     TestStruct{Number: 4},
			wantError: false,
		},
		{
			name:      "odd number",
			input:     TestStruct{Number: 5},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateStruct(&tt.input)

			if tt.wantError {
				if err == nil {
					t.Error("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestValidateStruct_EmptyTag(t *testing.T) {
	type TestStruct struct {
		Field1 string `validate:""`
		Field2 string `validate:";;;"`
		Field3 string `validate:"required;;min:5"`
	}

	input := TestStruct{
		Field1: "unchanged",
		Field2: "unchanged",
		Field3: "test",
	}

	err := validator.ValidateStruct(&input)
	if err == nil {
		t.Error("expected error for Field3 (too short)")
		return
	}

	validationErrors := validator.ExtractValidationErrors(err)
	if !validationErrors.Has("Field3") {
		t.Error("expected error for Field3")
	}
	if validationErrors.Has("Field1") || validationErrors.Has("Field2") {
		t.Error("should not validate fields with empty rules")
	}
}

func TestValidateStruct_Errors(t *testing.T) {
	// Test non-pointer
	var s struct{ Name string }
	err := validator.ValidateStruct(s)
	if err == nil || err.Error() != "validator: must pass a pointer to struct" {
		t.Errorf("expected pointer error, got: %v", err)
	}

	// Test non-struct pointer
	str := "not a struct"
	err = validator.ValidateStruct(&str)
	if err == nil || err.Error() != "validator: must pass a pointer to struct" {
		t.Errorf("expected struct error, got: %v", err)
	}
}

func TestValidateStruct_MultipleErrors(t *testing.T) {
	type TestStruct struct {
		Email    string `validate:"required;email"`
		Username string `validate:"required;min:3;max:20;alphanum"`
		Age      int    `validate:"required;min:18;max:150"`
	}

	input := TestStruct{
		Email:    "invalid",
		Username: "a!", // Too short and not alphanum
		Age:      200,  // Too high
	}

	err := validator.ValidateStruct(&input)
	if err == nil {
		t.Error("expected validation errors")
		return
	}

	validationErrors := validator.ExtractValidationErrors(err)
	if validationErrors == nil {
		t.Error("expected ValidationErrors type")
		return
	}

	// Should collect all errors, not stop at first
	if !validationErrors.Has("Email") {
		t.Error("expected error for Email")
	}
	if !validationErrors.Has("Username") {
		t.Error("expected error for Username")
	}
	if !validationErrors.Has("Age") {
		t.Error("expected error for Age")
	}

	// Username should have multiple errors
	usernameErrors := validationErrors.GetErrors("Username")
	if len(usernameErrors) < 2 {
		t.Errorf("expected multiple errors for Username, got %d", len(usernameErrors))
	}
}

// Benchmark to ensure performance is reasonable
func BenchmarkValidateStruct(b *testing.B) {
	type TestStruct struct {
		Email    string  `validate:"required;email"`
		Username string  `validate:"required;min:3;max:20;alphanum"`
		Age      int     `validate:"required;min:18;max:150"`
		Price    float64 `validate:"required;between:0.01,999.99"`
		Website  string  `validate:"url"`
	}

	input := TestStruct{
		Email:    "user@example.com",
		Username: "john123",
		Age:      25,
		Price:    99.99,
		Website:  "https://example.com",
	}

	b.ResetTimer()
	for b.Loop() {
		_ = validator.ValidateStruct(&input)
	}
}
