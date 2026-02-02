package sanitizer

import (
	"sync"

	"github.com/microcosm-cc/bluemonday"
)

var (
	strictPolicy *bluemonday.Policy
	safePolicy   *bluemonday.Policy
	initOnce     sync.Once
)

func initPolicies() {
	initOnce.Do(func() {
		// StrictPolicy strips ALL HTML, returns plain text
		strictPolicy = bluemonday.StrictPolicy()

		// SafePolicy allows basic formatting for user-generated content
		safePolicy = bluemonday.NewPolicy()
		safePolicy.AllowStandardURLs()
		safePolicy.AllowElements(
			"p", "br",
			"strong", "b", "em", "i",
			"ul", "ol", "li",
			"code", "pre", "blockquote",
		)
		safePolicy.AllowAttrs("href").OnElements("a")
		safePolicy.RequireNoFollowOnLinks(true)
	})
}

// SanitizeHTML allows safe formatting tags (p, a, strong, em, lists, code).
// Use for user-generated content that needs basic HTML formatting.
// Strips all dangerous elements and attributes including scripts, event handlers,
// and javascript: URLs.
func SanitizeHTML(s string) string {
	initPolicies()
	return safePolicy.Sanitize(s)
}

// SanitizeHTMLCustom applies a custom bluemonday policy.
// Returns input unchanged if policy is nil.
func SanitizeHTMLCustom(s string, policy *bluemonday.Policy) string {
	if policy == nil {
		return s
	}
	return policy.Sanitize(s)
}
