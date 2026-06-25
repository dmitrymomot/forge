package sanitizer

import (
	"html"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// Pre-compiled regex patterns for better performance
var (
	reScriptTags          = regexp.MustCompile(`(?i)<script\b[^>]*>.*?</script>`)
	reJSEvents            = regexp.MustCompile(`(?i)\s*on\w+\s*=\s*("[^"]*"|'[^']*')`)
	reJSProtocol          = regexp.MustCompile(`(?i)javascript\s*:`)
	reSQLIdentifier       = regexp.MustCompile(`[^a-zA-Z0-9_]`)
	rePathTraversal       = regexp.MustCompile(`\.\.[\\/]`)
	reDriveLetter         = regexp.MustCompile(`^[a-zA-Z]:`)
	reShellMetacharacters = regexp.MustCompile(`[|&;$\x60\\<>^!\*\?\[\]\(\)\{\}]`)
	reControlSequences    = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
)

// reDangerousAttrs are pre-compiled patterns for dangerous HTML attributes,
// hoisted to package scope so they are compiled once rather than on every
// SanitizeHTMLAttributes call.
var reDangerousAttrs = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\s*onclick\s*=\s*["'][^"']*["']`),
	regexp.MustCompile(`(?i)\s*onload\s*=\s*["'][^"']*["']`),
	regexp.MustCompile(`(?i)\s*onerror\s*=\s*["'][^"']*["']`),
	regexp.MustCompile(`(?i)\s*onmouseover\s*=\s*["'][^"']*["']`),
	regexp.MustCompile(`(?i)\s*onfocus\s*=\s*["'][^"']*["']`),
	regexp.MustCompile(`(?i)\s*onblur\s*=\s*["'][^"']*["']`),
	regexp.MustCompile(`(?i)\s*style\s*=\s*["'][^"']*expression[^"']*["']`),
	regexp.MustCompile(`(?i)\s*href\s*=\s*["']javascript:[^"']*["']`),
	regexp.MustCompile(`(?i)\s*src\s*=\s*["']javascript:[^"']*["']`),
}

// reSQLKeywords are pre-compiled patterns for common SQL keywords, hoisted to
// package scope so they are compiled once rather than on every
// RemoveSQLKeywords call.
var reSQLKeywords = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bSELECT\b`), regexp.MustCompile(`(?i)\bINSERT\b`),
	regexp.MustCompile(`(?i)\bUPDATE\b`), regexp.MustCompile(`(?i)\bDELETE\b`),
	regexp.MustCompile(`(?i)\bDROP\b`), regexp.MustCompile(`(?i)\bTABLE\b`),
	regexp.MustCompile(`(?i)\bCREATE\b`), regexp.MustCompile(`(?i)\bALTER\b`),
	regexp.MustCompile(`(?i)\bTRUNCATE\b`), regexp.MustCompile(`(?i)\bEXEC\b`),
	regexp.MustCompile(`(?i)\bEXECUTE\b`), regexp.MustCompile(`(?i)\bUNION\b`),
	regexp.MustCompile(`(?i)\bJOIN\b`), regexp.MustCompile(`(?i)\bWHERE\b`),
	regexp.MustCompile(`(?i)\bHAVING\b`), regexp.MustCompile(`(?i)\bORDER\s+BY\b`),
	regexp.MustCompile(`(?i)\bGROUP\s+BY\b`), regexp.MustCompile(`(?i)\bINTO\b`),
	regexp.MustCompile(`(?i)\bVALUES\b`), regexp.MustCompile(`(?i)\bFROM\b`),
	regexp.MustCompile(`(?i)\bSET\b`), regexp.MustCompile(`(?i)\bSCRIPT\b`),
	regexp.MustCompile(`(?i)\bDATA\b`), regexp.MustCompile(`(?i)\bSCHEMA\b`),
}

// EscapeHTML escapes HTML special characters to prevent XSS attacks.
func EscapeHTML(s string) string {
	return html.EscapeString(s)
}

// UnescapeHTML unescapes HTML entities.
func UnescapeHTML(s string) string {
	return html.UnescapeString(s)
}

// StripScriptTags removes all <script> tags and their content.
func StripScriptTags(s string) string {
	return reScriptTags.ReplaceAllString(s, "")
}

// RemoveJavaScriptEvents removes JavaScript event handlers from HTML attributes.
func RemoveJavaScriptEvents(s string) string {
	// Remove on* event handlers (onclick, onload, etc.)
	result := reJSEvents.ReplaceAllString(s, "")

	// Remove javascript: protocols
	return reJSProtocol.ReplaceAllString(result, "")
}

// SanitizeHTMLAttributes removes potentially dangerous HTML attributes.
//
// Deprecated: this regex-based scrubber has known bypasses (e.g. unquoted
// attribute values, newline-separated handlers). It is retained for callers
// that compose it explicitly; for XSS prevention prefer the bluemonday-backed
// StripHTML or SanitizeHTML instead.
func SanitizeHTMLAttributes(s string) string {
	result := s
	for _, re := range reDangerousAttrs {
		result = re.ReplaceAllString(result, "")
	}

	return result
}

// PreventXSS removes all HTML, returning plain text safe for display.
//
// It delegates to the bluemonday-backed StripHTML so the result is hardened
// against the bypasses that affect the home-grown regex scrubbers (e.g.
// malformed tags, unquoted attributes, encoded payloads). Use SanitizeHTML
// instead when you need to preserve safe formatting tags.
func PreventXSS(s string) string {
	return StripHTML(s)
}

// EscapeSQLString escapes single quotes in SQL strings to prevent injection.
func EscapeSQLString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// RemoveSQLKeywords removes common SQL keywords that could be used for injection.
func RemoveSQLKeywords(s string) string {
	result := s
	for _, re := range reSQLKeywords {
		result = re.ReplaceAllString(result, "")
	}

	return result
}

// SanitizeSQLIdentifier ensures SQL identifiers (table names, column names) are safe.
func SanitizeSQLIdentifier(s string) string {
	// Keep only alphanumeric and underscore
	result := reSQLIdentifier.ReplaceAllString(s, "")

	// Ensure it doesn't start with a number
	if len(result) > 0 && unicode.IsDigit(rune(result[0])) {
		result = "_" + result
	}

	// Limit length
	if len(result) > 64 {
		result = result[:64]
	}

	return result
}

// PreventPathTraversal removes path traversal attempts (../ and ..\).
func PreventPathTraversal(path string) string {
	// Remove any ../ or ..\
	result := rePathTraversal.ReplaceAllString(path, "")

	// Remove any remaining .. at the end
	result = strings.ReplaceAll(result, "..", "")

	return result
}

// SanitizePath cleans and normalizes file paths to prevent directory traversal.
func SanitizePath(path string) string {
	// Clean the path
	cleaned := filepath.Clean(path)

	// Remove any path traversal attempts
	cleaned = PreventPathTraversal(cleaned)

	// Remove any drive letters on Windows (C:, D:, etc.)
	cleaned = reDriveLetter.ReplaceAllString(cleaned, "")

	// Ensure it doesn't start with / or \ (after drive letter removal)
	cleaned = strings.TrimPrefix(cleaned, "/")
	cleaned = strings.TrimPrefix(cleaned, "\\")

	// Normalize path separators to forward slashes
	cleaned = filepath.ToSlash(cleaned)

	return cleaned
}

// NormalizePath normalizes a file path and prevents traversal attacks.
func NormalizePath(path string) string {
	// Normalize path separators
	normalized := filepath.ToSlash(path)

	// Apply sanitization
	normalized = SanitizePath(normalized)

	return normalized
}

// SanitizeShellArgument makes a string safe for use as a shell argument.
func SanitizeShellArgument(arg string) string {
	// Remove shell metacharacters
	dangerous := []string{
		"|", "&", ";", "$", "`", "\\", "\"", "'", " ", "\t", "\n", "\r",
		"*", "?", "[", "]", "(", ")", "{", "}", "<", ">", "^", "!",
	}

	result := arg
	for _, char := range dangerous {
		result = strings.ReplaceAll(result, char, "")
	}

	return result
}

// RemoveShellMetacharacters removes shell metacharacters that could be used for injection.
func RemoveShellMetacharacters(s string) string {
	// Remove characters that have special meaning in shells
	return reShellMetacharacters.ReplaceAllString(s, "")
}

// RemoveNullBytes removes null bytes that could cause issues in C-based systems.
func RemoveNullBytes(s string) string {
	return strings.ReplaceAll(s, "\x00", "")
}

// RemoveControlSequences removes ANSI escape sequences and other control characters.
func RemoveControlSequences(s string) string {
	// Remove ANSI escape sequences
	result := reControlSequences.ReplaceAllString(s, "")

	// Remove other control characters except common ones
	result = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return -1
		}
		return r
	}, result)

	return result
}

// LimitLength truncates input to prevent DoS attacks through large inputs.
func LimitLength(s string, maxLength int) string {
	if maxLength <= 0 {
		return ""
	}

	runes := []rune(s)
	if len(runes) <= maxLength {
		return s
	}

	return string(runes[:maxLength])
}

// SanitizeUserInput applies comprehensive sanitization for user input.
func SanitizeUserInput(s string) string {
	result := s
	result = RemoveNullBytes(result)
	result = RemoveControlSequences(result)
	result = strings.TrimSpace(result)
	result = LimitLength(result, 10000) // Reasonable default limit
	return result
}

// PreventLDAPInjection removes LDAP injection characters.
func PreventLDAPInjection(s string) string {
	// Remove LDAP special characters
	dangerous := []string{"(", ")", "*", "\\", "/", "\x00"}

	result := s
	for _, char := range dangerous {
		result = strings.ReplaceAll(result, char, "")
	}

	return result
}

// SanitizeEmail removes dangerous characters from email addresses while preserving valid format.
func SanitizeEmail(email string) string {
	// Remove null bytes and control characters
	result := RemoveNullBytes(email)
	result = RemoveControlSequences(result)

	// Remove potential XSS attempts
	result = strings.ReplaceAll(result, "<", "")
	result = strings.ReplaceAll(result, ">", "")
	result = strings.ReplaceAll(result, "\"", "")
	result = strings.ReplaceAll(result, "'", "")

	return strings.TrimSpace(result)
}

// SanitizeURL removes dangerous elements from URLs while preserving valid structure.
func SanitizeURL(url string) string {
	result := url

	// Remove dangerous protocols
	dangerous := []string{
		"javascript:", "data:", "vbscript:", "file:", "ftp:",
	}

	lower := strings.ToLower(result)
	for _, protocol := range dangerous {
		if strings.HasPrefix(lower, protocol) {
			return ""
		}
	}

	// Remove potential XSS
	result = RemoveJavaScriptEvents(result)
	result = strings.ReplaceAll(result, "<", "")
	result = strings.ReplaceAll(result, ">", "")

	return strings.TrimSpace(result)
}

// PreventHeaderInjection removes characters that could be used for HTTP header injection.
func PreventHeaderInjection(s string) string {
	// Remove line breaks that could split headers
	result := strings.ReplaceAll(s, "\r", "")
	result = strings.ReplaceAll(result, "\n", "")

	// Remove null bytes
	result = RemoveNullBytes(result)

	return result
}

// SanitizeSecureFilename makes a filename safe by removing dangerous characters.
func SanitizeSecureFilename(filename string) string {
	// Remove path separators and dangerous characters
	dangerous := []string{
		"/", "\\", ":", "*", "?", "\"", "<", ">", "|",
		"\x00", "\r", "\n", "\t",
	}

	result := filename
	for _, char := range dangerous {
		result = strings.ReplaceAll(result, char, "_")
	}

	// Remove leading/trailing spaces and dots
	result = strings.Trim(result, " .")

	// Limit length
	result = LimitLength(result, 255)

	// Ensure it's not empty
	if result == "" {
		result = "file"
	}

	return result
}
