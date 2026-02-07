package i18n

import (
	"fmt"
	"strings"
)

// ReplacePlaceholders replaces placeholders in the template string with values
// from the provided map. Placeholders use the format {{name}}.
// If a placeholder is not found in the map, it remains unchanged.
//
// Example:
//
//	template: "Hello, {{name}}! You have {{count}} messages."
//	placeholders: M{"name": "John", "count": 5}
//	returns: "Hello, John! You have 5 messages."
func ReplacePlaceholders(template string, placeholders M) string {
	if len(placeholders) < 1 {
		return template
	}

	result := template
	for key, value := range placeholders {
		placeholder := "{{" + key + "}}"
		replacement := fmt.Sprintf("%v", value)
		result = strings.ReplaceAll(result, placeholder, replacement)
	}

	return result
}
