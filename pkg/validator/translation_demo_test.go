package validator_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/validator"
)

// mockTranslator simulates a translation function for use with Translate.
func mockTranslator(key string, values map[string]any) string {
	translations := map[string]string{
		"validation.required":     "The {{field}} field is required.",
		"validation.min_length":   "The {{field}} must be at least {{min}} characters long.",
		"validation.max_length":   "The {{field}} must not exceed {{max}} characters.",
		"validation.exact_length": "The {{field}} must be exactly {{length}} characters long.",
		"validation.min":          "The {{field}} must be at least {{min}}.",
		"validation.max":          "The {{field}} must not exceed {{max}}.",
		"validation.min_items":    "The {{field}} must contain at least {{min}} items.",
		"validation.max_items":    "The {{field}} must not contain more than {{max}} items.",
		"validation.exact_items":  "The {{field}} must contain exactly {{count}} items.",
	}

	tmpl := translations[key]
	if tmpl == "" {
		return key
	}

	result := tmpl
	for placeholder, value := range values {
		token := "{{" + placeholder + "}}"
		result = strings.ReplaceAll(result, token, formatValue(value))
	}
	return result
}

func TestTranslationWorkflow(t *testing.T) {
	t.Parallel()
	t.Run("basic translation workflow with Translate", func(t *testing.T) {
		type LoginForm struct {
			Email    string
			Password string
		}

		form := LoginForm{
			Email:    "",
			Password: "123",
		}

		err := validator.Apply(
			validator.RequiredString("email", form.Email),
			validator.RequiredString("password", form.Password),
			validator.MinLenString("password", form.Password, 8),
		)

		require.Error(t, err)
		require.True(t, validator.IsValidationError(err))

		ve := validator.ExtractValidationErrors(err)
		ve.Translate(mockTranslator)

		emailMsgs := ve.Get("email")
		require.Len(t, emailMsgs, 1)
		assert.Equal(t, "The email field is required.", emailMsgs[0])

		pwdMsgs := ve.Get("password")
		require.Len(t, pwdMsgs, 1)
		assert.Equal(t, "The password must be at least 8 characters long.", pwdMsgs[0])
	})

	t.Run("complex validation with multiple translation values", func(t *testing.T) {
		t.Parallel()
		username := "ab"
		tags := []string{"tag1", "tag2", "tag3", "tag4", "tag5", "tag6"}
		age := 15

		err := validator.Apply(
			validator.MinLenString("username", username, 3),
			validator.MaxLenString("username", username, 20),
			validator.MaxLenSlice("tags", tags, 5),
			validator.MinNum("age", age, 18),
		)

		require.Error(t, err)
		ve := validator.ExtractValidationErrors(err)
		ve.Translate(mockTranslator)

		usernameMsgs := ve.Get("username")
		require.Len(t, usernameMsgs, 1)
		assert.Equal(t, "The username must be at least 3 characters long.", usernameMsgs[0])

		tagsMsgs := ve.Get("tags")
		require.Len(t, tagsMsgs, 1)
		assert.Equal(t, "The tags must not contain more than 5 items.", tagsMsgs[0])

		ageMsgs := ve.Get("age")
		require.Len(t, ageMsgs, 1)
		assert.Equal(t, "The age must be at least 18.", ageMsgs[0])
	})

	t.Run("field-specific translation data preserved after Translate", func(t *testing.T) {
		t.Parallel()
		email := ""
		password := "weak"

		err := validator.Apply(
			validator.RequiredString("email", email),
			validator.MinLenString("password", password, 8),
			validator.MaxLenString("password", password, 128),
		)

		require.Error(t, err)
		ve := validator.ExtractValidationErrors(err)
		ve.Translate(mockTranslator)

		emailErrors := ve.GetErrors("email")
		require.Len(t, emailErrors, 1)
		assert.Equal(t, "validation.required", emailErrors[0].TranslationKey)
		assert.Equal(t, "email", emailErrors[0].TranslationValues["field"])
		assert.Equal(t, "The email field is required.", emailErrors[0].Message)

		passwordErrors := ve.GetErrors("password")
		require.Len(t, passwordErrors, 1)
		assert.Equal(t, "validation.min_length", passwordErrors[0].TranslationKey)
		assert.Equal(t, "password", passwordErrors[0].TranslationValues["field"])
		assert.Equal(t, 8, passwordErrors[0].TranslationValues["min"])
		assert.Equal(t, "The password must be at least 8 characters long.", passwordErrors[0].Message)
	})

	t.Run("translation key consistency across rule types", func(t *testing.T) {
		t.Parallel()
		stringField := ""
		sliceField := []string{}
		mapField := map[string]string{}
		numField := 0

		err := validator.Apply(
			validator.RequiredString("stringField", stringField),
			validator.RequiredSlice("sliceField", sliceField),
			validator.RequiredMap("mapField", mapField),
			validator.RequiredNum("numField", numField),
		)

		require.Error(t, err)
		ve := validator.ExtractValidationErrors(err)
		ve.Translate(mockTranslator)

		for _, e := range ve {
			assert.Equal(t, "validation.required", e.TranslationKey)
			assert.Contains(t, e.TranslationValues, "field")
			assert.Contains(t, e.Message, "field is required")
		}
	})
}

func TestTranslationKeyStandards(t *testing.T) {
	t.Parallel()
	t.Run("validates standard translation keys", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			rule           validator.Rule
			expectedKey    string
			expectedValues map[string]any
		}{
			{
				rule:        validator.RequiredString("email", ""),
				expectedKey: "validation.required",
				expectedValues: map[string]any{
					"field": "email",
				},
			},
			{
				rule:        validator.MinLenString("password", "123", 8),
				expectedKey: "validation.min_length",
				expectedValues: map[string]any{
					"field": "password",
					"min":   8,
				},
			},
			{
				rule:        validator.MaxLenString("username", "verylongusername", 10),
				expectedKey: "validation.max_length",
				expectedValues: map[string]any{
					"field": "username",
					"max":   10,
				},
			},
			{
				rule:        validator.LenString("code", "1234", 6),
				expectedKey: "validation.exact_length",
				expectedValues: map[string]any{
					"field":  "code",
					"length": 6,
				},
			},
			{
				rule:        validator.MinNum("age", 15, 18),
				expectedKey: "validation.min",
				expectedValues: map[string]any{
					"field": "age",
					"min":   18,
				},
			},
			{
				rule:        validator.MaxNum("score", 105, 100),
				expectedKey: "validation.max",
				expectedValues: map[string]any{
					"field": "score",
					"max":   100,
				},
			},
		}

		for _, test := range tests {
			assert.Equal(t, test.expectedKey, test.rule.Error.TranslationKey)
			assert.Equal(t, test.expectedValues, test.rule.Error.TranslationValues)
		}
	})
}
