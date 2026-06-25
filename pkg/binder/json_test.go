package binder_test

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/binder"
)

func TestJSON(t *testing.T) {
	t.Parallel()
	type testStruct struct {
		Name  string `json:"name"`
		Age   int    `json:"age"`
		Email string `json:"email"`
	}

	t.Run("valid JSON binding", func(t *testing.T) {
		t.Parallel()
		jsonData := `{"name":"John Doe","age":30,"email":"john@example.com"}`
		req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(jsonData))
		req.Header.Set("Content-Type", "application/json")

		var result testStruct
		bindFunc := binder.JSON()
		err := bindFunc(req, &result)

		require.NoError(t, err)
		assert.Equal(t, "John Doe", result.Name)
		assert.Equal(t, 30, result.Age)
		assert.Equal(t, "john@example.com", result.Email)
	})

	t.Run("content type with charset", func(t *testing.T) {
		t.Parallel()
		jsonData := `{"name":"Jane","age":25}`
		req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(jsonData))
		req.Header.Set("Content-Type", "application/json; charset=utf-8")

		var result testStruct
		bindFunc := binder.JSON()
		err := bindFunc(req, &result)

		require.NoError(t, err)
		assert.Equal(t, "Jane", result.Name)
		assert.Equal(t, 25, result.Age)
	})

	t.Run("missing content type", func(t *testing.T) {
		t.Parallel()
		jsonData := `{"name":"Test"}`
		req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(jsonData))

		var result testStruct
		bindFunc := binder.JSON()
		err := bindFunc(req, &result)

		require.Error(t, err)
		assert.True(t, errors.Is(err, binder.ErrMissingContentType))
		assert.Contains(t, err.Error(), "expected application/json")
	})

	t.Run("wrong content type", func(t *testing.T) {
		t.Parallel()
		jsonData := `{"name":"Test"}`
		req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(jsonData))
		req.Header.Set("Content-Type", "text/plain")

		var result testStruct
		bindFunc := binder.JSON()
		err := bindFunc(req, &result)

		require.Error(t, err)
		assert.True(t, errors.Is(err, binder.ErrUnsupportedMediaType))
		assert.Contains(t, err.Error(), "got text/plain")
	})

	t.Run("empty body", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(""))
		req.Header.Set("Content-Type", "application/json")

		var result testStruct
		bindFunc := binder.JSON()
		err := bindFunc(req, &result)

		require.Error(t, err)
		assert.True(t, errors.Is(err, binder.ErrFailedToParseJSON))
		assert.Contains(t, err.Error(), "empty body")
	})

	t.Run("invalid JSON syntax", func(t *testing.T) {
		t.Parallel()
		jsonData := `{"name":"Test"`
		req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(jsonData))
		req.Header.Set("Content-Type", "application/json")

		var result testStruct
		bindFunc := binder.JSON()
		err := bindFunc(req, &result)

		require.Error(t, err)
		assert.True(t, errors.Is(err, binder.ErrFailedToParseJSON))
		assert.Contains(t, err.Error(), "unexpected EOF")
	})

	t.Run("invalid character in JSON", func(t *testing.T) {
		t.Parallel()
		jsonData := `{name:"Test"}`
		req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(jsonData))
		req.Header.Set("Content-Type", "application/json")

		var result testStruct
		bindFunc := binder.JSON()
		err := bindFunc(req, &result)

		require.Error(t, err)
		assert.True(t, errors.Is(err, binder.ErrFailedToParseJSON))
		assert.Contains(t, err.Error(), "invalid character")
	})

	t.Run("type mismatch", func(t *testing.T) {
		t.Parallel()
		jsonData := `{"name":"Test","age":"not a number"}`
		req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(jsonData))
		req.Header.Set("Content-Type", "application/json")

		var result testStruct
		bindFunc := binder.JSON()
		err := bindFunc(req, &result)

		require.Error(t, err)
		assert.True(t, errors.Is(err, binder.ErrFailedToParseJSON))
		assert.Contains(t, err.Error(), "cannot unmarshal")
	})

	t.Run("unknown fields rejected", func(t *testing.T) {
		t.Parallel()
		jsonData := `{"name":"Test","age":25,"unknown_field":"value"}`
		req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(jsonData))
		req.Header.Set("Content-Type", "application/json")

		var result testStruct
		bindFunc := binder.JSON()
		err := bindFunc(req, &result)

		require.Error(t, err)
		assert.True(t, errors.Is(err, binder.ErrFailedToParseJSON))
		assert.Contains(t, err.Error(), "unknown")
	})

	t.Run("extra data after valid JSON", func(t *testing.T) {
		t.Parallel()
		jsonData := `{"name":"Test","age":25}{"extra":"data"}`
		req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(jsonData))
		req.Header.Set("Content-Type", "application/json")

		var result testStruct
		bindFunc := binder.JSON()
		err := bindFunc(req, &result)

		require.Error(t, err)
		assert.True(t, errors.Is(err, binder.ErrFailedToParseJSON))
		assert.Contains(t, err.Error(), "unexpected data after JSON object")
	})

	t.Run("null values", func(t *testing.T) {
		t.Parallel()
		jsonData := `{"name":null,"age":null,"email":null}`
		req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(jsonData))
		req.Header.Set("Content-Type", "application/json")

		var result testStruct
		bindFunc := binder.JSON()
		err := bindFunc(req, &result)

		require.NoError(t, err)
		assert.Equal(t, "", result.Name)
		assert.Equal(t, 0, result.Age)
		assert.Equal(t, "", result.Email)
	})

	t.Run("partial data", func(t *testing.T) {
		t.Parallel()
		jsonData := `{"name":"Partial"}`
		req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(jsonData))
		req.Header.Set("Content-Type", "application/json")

		var result testStruct
		bindFunc := binder.JSON()
		err := bindFunc(req, &result)

		require.NoError(t, err)
		assert.Equal(t, "Partial", result.Name)
		assert.Equal(t, 0, result.Age)    // zero value
		assert.Equal(t, "", result.Email) // zero value
	})

	t.Run("nested structs", func(t *testing.T) {
		t.Parallel()
		type Address struct {
			Street string `json:"street"`
			City   string `json:"city"`
		}
		type Person struct {
			Name    string  `json:"name"`
			Address Address `json:"address"`
		}

		jsonData := `{"name":"John","address":{"street":"123 Main St","city":"NYC"}}`
		req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(jsonData))
		req.Header.Set("Content-Type", "application/json")

		var result Person
		bindFunc := binder.JSON()
		err := bindFunc(req, &result)

		require.NoError(t, err)
		assert.Equal(t, "John", result.Name)
		assert.Equal(t, "123 Main St", result.Address.Street)
		assert.Equal(t, "NYC", result.Address.City)
	})

	t.Run("arrays", func(t *testing.T) {
		t.Parallel()
		type Items struct {
			Names []string `json:"names"`
			Nums  []int    `json:"nums"`
		}

		jsonData := `{"names":["Alice","Bob"],"nums":[1,2,3]}`
		req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(jsonData))
		req.Header.Set("Content-Type", "application/json")

		var result Items
		bindFunc := binder.JSON()
		err := bindFunc(req, &result)

		require.NoError(t, err)
		assert.Equal(t, []string{"Alice", "Bob"}, result.Names)
		assert.Equal(t, []int{1, 2, 3}, result.Nums)
	})

	t.Run("pointer fields", func(t *testing.T) {
		t.Parallel()
		type OptionalFields struct {
			Name     *string `json:"name"`
			Age      *int    `json:"age"`
			Required string  `json:"required"`
		}

		jsonData := `{"name":"Test","required":"value"}`
		req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(jsonData))
		req.Header.Set("Content-Type", "application/json")

		var result OptionalFields
		bindFunc := binder.JSON()
		err := bindFunc(req, &result)

		require.NoError(t, err)
		require.NotNil(t, result.Name)
		assert.Equal(t, "Test", *result.Name)
		assert.Nil(t, result.Age) // Not provided, should be nil
		assert.Equal(t, "value", result.Required)
	})

	t.Run("content type with multiple parameters", func(t *testing.T) {
		t.Parallel()
		jsonData := `{"name":"Test"}`
		req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(jsonData))
		req.Header.Set("Content-Type", "application/json; charset=utf-8; boundary=something")

		var result testStruct
		bindFunc := binder.JSON()
		err := bindFunc(req, &result)

		require.NoError(t, err)
		assert.Equal(t, "Test", result.Name)
	})
}

func TestJSONSanitization(t *testing.T) {
	t.Parallel()

	t.Run("map string values are sanitized", func(t *testing.T) {
		t.Parallel()

		// A control character (DEL, 0x7f) and a CRLF are embedded in a map value.
		// Map values are not directly settable via reflection, so this exercises the
		// SetMapIndex write-back path that previously made map sanitization a no-op.
		jsonData := "{\"meta\":{\"note\":\"clean\\u007fvalue\",\"line\":\"a\\r\\nb\"}}"
		req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(jsonData))
		req.Header.Set("Content-Type", "application/json")

		var result struct {
			Meta map[string]string `json:"meta"`
		}
		err := binder.JSON()(req, &result)

		require.NoError(t, err)
		require.Len(t, result.Meta, 2)
		assert.Equal(t, "cleanvalue", result.Meta["note"], "DEL byte must be stripped from map value")
		assert.Equal(t, "ab", result.Meta["line"], "CRLF must be stripped from map value")
	})

	t.Run("map[string]any string values are sanitized", func(t *testing.T) {
		t.Parallel()

		// map[string]any decodes string values into interface{}, exercising the
		// interface write-back path on top of the map write-back path.
		jsonData := "{\"meta\":{\"note\":\"bad\\u0000null\\u009ctail\"}}"
		req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(jsonData))
		req.Header.Set("Content-Type", "application/json")

		var result struct {
			Meta map[string]any `json:"meta"`
		}
		err := binder.JSON()(req, &result)

		require.NoError(t, err)
		note, ok := result.Meta["note"].(string)
		require.True(t, ok, "note should remain a string")
		assert.Equal(t, "badnulltail", note, "NUL and C1 control char must be stripped from interface map value")
	})

	t.Run("DEL and C1 control characters are stripped from string fields", func(t *testing.T) {
		t.Parallel()

		// DEL (U+007F) and several C1 control characters (U+0080-U+009F) must be
		// removed; a graphic character above the C1 block (é, U+00E9) and a tab must
		// be preserved.
		jsonData := "{\"field\":\"a\\u007fb\\u0080c\\u009fd\\te\\u00e9\"}"
		req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(jsonData))
		req.Header.Set("Content-Type", "application/json")

		var result struct {
			Field string `json:"field"`
		}
		err := binder.JSON()(req, &result)

		require.NoError(t, err)
		assert.Equal(t, "abcd\teé", result.Field)
		assert.NotContains(t, result.Field, "\x7f", "DEL must be stripped")
		assert.NotContains(t, result.Field, "\x80", "C1 control (0x80) must be stripped")
		assert.NotContains(t, result.Field, "\x9f", "C1 control (0x9f) must be stripped")
	})

	t.Run("nested struct string values inside maps are sanitized", func(t *testing.T) {
		t.Parallel()

		type inner struct {
			Note string `json:"note"`
		}
		jsonData := "{\"items\":{\"a\":{\"note\":\"keep\\u007fme\"}}}"
		req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(jsonData))
		req.Header.Set("Content-Type", "application/json")

		var result struct {
			Items map[string]inner `json:"items"`
		}
		err := binder.JSON()(req, &result)

		require.NoError(t, err)
		assert.Equal(t, "keepme", result.Items["a"].Note, "DEL inside a struct value of a map must be stripped")
	})
}
