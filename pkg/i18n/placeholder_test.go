package i18n_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/i18n"
)

func TestReplacePlaceholders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		template     string
		placeholders i18n.M
		expected     string
	}{
		{
			name:         "no placeholders",
			template:     "Hello, World!",
			placeholders: nil,
			expected:     "Hello, World!",
		},
		{
			name:         "single placeholder",
			template:     "Hello, {{name}}!",
			placeholders: i18n.M{"name": "John"},
			expected:     "Hello, John!",
		},
		{
			name:         "multiple placeholders",
			template:     "Welcome, {{name}}! You have {{count}} messages.",
			placeholders: i18n.M{"name": "Alice", "count": 5},
			expected:     "Welcome, Alice! You have 5 messages.",
		},
		{
			name:         "missing placeholder remains unchanged",
			template:     "Hello, {{name}}! Your ID is {{id}}.",
			placeholders: i18n.M{"name": "Bob"},
			expected:     "Hello, Bob! Your ID is {{id}}.",
		},
		{
			name:         "integer values",
			template:     "You have {{count}} items in your cart.",
			placeholders: i18n.M{"count": 42},
			expected:     "You have 42 items in your cart.",
		},
		{
			name:         "float values",
			template:     "Your balance is ${{amount}}.",
			placeholders: i18n.M{"amount": 123.45},
			expected:     "Your balance is $123.45.",
		},
		{
			name:         "boolean values",
			template:     "Feature enabled: {{enabled}}",
			placeholders: i18n.M{"enabled": true},
			expected:     "Feature enabled: true",
		},
		{
			name:         "repeated placeholders",
			template:     "{{name}} is here. Hello, {{name}}!",
			placeholders: i18n.M{"name": "Charlie"},
			expected:     "Charlie is here. Hello, Charlie!",
		},
		{
			name:         "empty placeholder map",
			template:     "Hello, {{name}}!",
			placeholders: i18n.M{},
			expected:     "Hello, {{name}}!",
		},
		{
			name:         "placeholder with special characters",
			template:     "Path: {{path}}",
			placeholders: i18n.M{"path": "/usr/local/bin"},
			expected:     "Path: /usr/local/bin",
		},
		{
			name:         "nil value",
			template:     "Value: {{val}}",
			placeholders: i18n.M{"val": nil},
			expected:     "Value: <nil>",
		},
		{
			name:         "placeholder names with underscores",
			template:     "User {{user_name}} has {{item_count}} items",
			placeholders: i18n.M{"user_name": "Dave", "item_count": 10},
			expected:     "User Dave has 10 items",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := i18n.ReplacePlaceholders(tt.template, tt.placeholders)
			require.Equal(t, tt.expected, result)
		})
	}
}
