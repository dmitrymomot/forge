// Package sanitizer provides comprehensive input sanitization and data cleaning
// utilities for web applications. It offers protection against XSS attacks,
// SQL injection, and other security vulnerabilities through string manipulation,
// HTML sanitization, collection processing, and structured data cleaning.
//
// The package provides four main categories of functionality:
//   - String manipulation and normalization
//   - Format-specific sanitization (email, phone, URL, etc.)
//   - Security-focused protection (XSS, SQL injection, path traversal)
//   - Collection and structured data processing
//
// # Basic String Sanitization
//
// Core string cleaning functions for common transformations:
//
//	import "github.com/dmitrymomot/forge/pkg/sanitizer"
//
//	// Basic trimming and case conversion
//	cleaned := sanitizer.Trim("  hello world  ")          // "hello world"
//	lower := sanitizer.ToLower("HELLO")                    // "hello"
//	upper := sanitizer.ToUpper("hello")                    // "HELLO"
//	title := sanitizer.ToTitle("hello world")             // "HELLO WORLD"
//
//	// Combined operations
//	clean := sanitizer.TrimToLower("  HELLO  ")           // "hello"
//	clean = sanitizer.TrimToUpper("  hello  ")            // "HELLO"
//
//	// Length and character filtering
//	limited := sanitizer.MaxLength("very long text", 10)  // "very long "
//	alphanum := sanitizer.KeepAlphanumeric("hello@123!")  // "hello123 "
//	alpha := sanitizer.KeepAlpha("hello123!")             // "hello "
//	digits := sanitizer.KeepDigits("abc123def")           // "123"
//
// # Case Conversion Functions
//
// Transform strings between different naming conventions:
//
//	// Convert to kebab-case (URL-friendly)
//	slug := sanitizer.ToKebabCase("Hello World!")  // "hello-world"
//
//	// Convert to snake_case (database-friendly)
//	field := sanitizer.ToSnakeCase("Hello World")  // "hello_world"
//
//	// Convert to camelCase (JavaScript-friendly)
//	prop := sanitizer.ToCamelCase("hello world")   // "helloWorld"
//
// # Whitespace and Control Character Handling
//
// Clean up problematic whitespace and control characters:
//
//	// Normalize multiple whitespace to single spaces
//	normalized := sanitizer.RemoveExtraWhitespace("hello   world")  // "hello world"
//	normalized = sanitizer.NormalizeWhitespace("hello\t\nworld")    // "hello world"
//
//	// Remove control characters but preserve basic whitespace
//	safe := sanitizer.RemoveControlChars("hello\x00world")          // "helloworld"
//
//	// Convert to single line
//	oneLine := sanitizer.SingleLine("line1\nline2\rline3")         // "line1 line2 line3"
//
// # HTML and Security Sanitization
//
// Protect against XSS attacks and clean HTML content:
//
//	// Strip all HTML tags
//	plainText := sanitizer.StripHTML("<p>Hello <script>alert('xss')</script>world</p>")
//	// Result: "Hello world"
//
//	// Escape HTML for safe display
//	escaped := sanitizer.EscapeHTML("<script>alert('xss')</script>")
//	// Result: "&lt;script&gt;alert('xss')&lt;/script&gt;"
//
//	// Comprehensive XSS prevention
//	safe := sanitizer.PreventXSS(`<p onclick="evil()">Content</p>`)
//
//	// Remove specific dangerous elements
//	cleaned := sanitizer.StripScriptTags(`<p>Safe</p><script>alert('xss')</script>`)
//	cleaned = sanitizer.RemoveJavaScriptEvents(`<div onclick="evil()">Content</div>`)
//
// # SQL Injection Prevention
//
// Sanitize inputs for database operations:
//
//	// Escape SQL strings (but prefer parameterized queries)
//	escaped := sanitizer.EscapeSQLString("O'Connor")  // "O''Connor"
//
//	// Clean SQL identifiers (table/column names)
//	safeTable := sanitizer.SanitizeSQLIdentifier("users-table!")  // "users_table"
//
//	// Remove dangerous SQL keywords
//	cleaned := sanitizer.RemoveSQLKeywords("SELECT * FROM users")  // " *  users"
//
//	// Example with parameterized queries (recommended approach)
//	tableName := sanitizer.SanitizeSQLIdentifier(userTableName)
//	query := fmt.Sprintf("SELECT * FROM %s WHERE id = ?", tableName)
//	// Then use query with parameters
//
// # Format-Specific Sanitization
//
// Clean and normalize common data formats:
//
//	// Email processing
//	email := sanitizer.NormalizeEmail("  JOHN.DOE@EXAMPLE.COM  ")  // "john.doe@example.com"
//	domain := sanitizer.ExtractEmailDomain("user@domain.com")      // "domain.com"
//	masked := sanitizer.MaskEmail("john.doe@example.com")          // "j*******@example.com"
//	secure := sanitizer.SanitizeEmail("user<script>@example.com")  // "userscript@example.com"
//
//	// Phone number processing
//	digits := sanitizer.NormalizePhone("(555) 123-4567")          // "5551234567"
//	formatted := sanitizer.FormatPhoneUS("5551234567")            // "(555) 123-4567"
//	masked := sanitizer.MaskPhone("5551234567")                   // "******4567"
//
//	// URL processing
//	normalized := sanitizer.NormalizeURL("example.com/path/")     // "https://example.com/path"
//	domain := sanitizer.ExtractDomain("https://example.com/path") // "example.com"
//	cleaned := sanitizer.SanitizeURL("javascript:alert('xss')")   // "" (removes dangerous URLs)
//	noQuery := sanitizer.RemoveQueryParams("https://example.com?tracking=123")
//
// # File Path Sanitization
//
// Secure file path and filename handling:
//
//	// Clean filenames for safe storage
//	safe := sanitizer.SanitizeFilename("my file<>:name.txt")       // "my file___name.txt"
//	secure := sanitizer.SanitizeSecureFilename("../../../etc/passwd")  // ".._.._..._etc_passwd"
//
//	// Prevent directory traversal attacks
//	safePath := sanitizer.SanitizePath("../../../etc/passwd")     // "etc/passwd"
//	normalized := sanitizer.NormalizePath("folder\\..\\file.txt") // "file.txt"
//	cleaned := sanitizer.PreventPathTraversal("folder/../file")   // "folder/file"
//
// # Numeric Value Processing
//
// Type-safe numeric sanitization using Go generics:
//
//	// Clamp values within ranges
//	clamped := sanitizer.Clamp(15, 0, 10)                         // 10
//	minClamped := sanitizer.ClampMin(-5, 0)                       // 0
//	maxClamped := sanitizer.ClampMax(15, 10)                      // 10
//
//	// Absolute values and sign handling
//	abs := sanitizer.Abs(-42)                                     // 42
//	positive := sanitizer.ZeroIfNegative(-5)                      // 0
//	nonZero := sanitizer.NonZero(0)                               // 1
//
//	// Floating-point operations
//	rounded := sanitizer.Round(3.7)                               // 4.0
//	precise := sanitizer.RoundToDecimalPlaces(3.14159, 2)         // 3.14
//	percentage := sanitizer.Percentage(25, 100)                  // 25.0
//
//	// Safe division
//	result := sanitizer.SafeDivide(10, 0, -1)                     // -1 (fallback)
//
// # Collection Processing
//
// Clean and manipulate slices and maps:
//
//	// String slice operations
//	trimmed := sanitizer.TrimStringSlice([]string{"  hello ", " world  "})
//	lower := sanitizer.ToLowerStringSlice([]string{"HELLO", "WORLD"})
//	clean := sanitizer.CleanStringSlice([]string{"  hello ", "", " world "})
//	// clean result: ["hello", "world"] (trimmed, filtered, deduplicated)
//
//	// Slice utilities
//	filtered := sanitizer.FilterEmpty([]string{"hello", "", "world"})    // ["hello", "world"]
//	unique := sanitizer.DeduplicateStrings([]string{"a", "b", "a"})      // ["a", "b"]
//	limited := sanitizer.LimitSliceLength([]string{"a", "b", "c"}, 2)    // ["a", "b"]
//	sorted := sanitizer.SortStrings([]string{"c", "a", "b"})            // ["a", "b", "c"]
//
//	// Map operations
//	data := map[string]string{"  NAME ": "  John ", "email": "john@example.com"}
//	cleaned := sanitizer.CleanStringMap(data)
//	// Result: {"name": "John", "email": "john@example.com"}
//
//	// Map utilities
//	keys := sanitizer.ExtractMapKeys(data)
//	values := sanitizer.ExtractMapValues(data)
//	merged := sanitizer.MergeStringMaps(map1, map2, map3)
//
// # Struct Field Sanitization with Tags
//
// Automatically sanitize struct fields using reflection and tags:
//
//	type User struct {
//		Name     string `sanitize:"trim,title"`
//		Email    string `sanitize:"email"`
//		Username string `sanitize:"trim,lower,alphanum,max:20"`
//		Bio      string `sanitize:"trim,safe_html"`
//		Website  string `sanitize:"url"`
//		Tags     []string `sanitize:"trim,lower"`
//		// Use "-" to skip sanitization
//		Password string `sanitize:"-"`
//	}
//
//	user := &User{
//		Name:     "  john doe  ",
//		Email:    "  JOHN@EXAMPLE.COM  ",
//		Username: "  John123!@#  ",
//		Bio:      `<p>Developer</p><script>alert('xss')</script>`,
//		Website:  "example.com/profile",
//		Tags:     []string{"  GO  ", "  WEB  "},
//	}
//
//	err := sanitizer.SanitizeStruct(user)
//	// user.Name becomes "JOHN DOE"
//	// user.Email becomes "john@example.com"
//	// user.Username becomes "john123" (limited to 20 chars)
//	// user.Bio becomes safe HTML
//	// user.Website becomes "https://example.com/profile"
//	// user.Tags becomes ["go", "web"]
//
// # Available Sanitizer Tags
//
// The following tags can be used with SanitizeStruct:
//
//	// String operations
//	"trim", "lower", "upper", "title", "trim_lower", "trim_upper"
//	"kebab", "snake", "camel", "single_line", "no_spaces"
//	"alphanum", "alpha", "digits", "max:N"
//
//	// Format sanitizers
//	"email", "phone", "url", "domain", "filename", "whitespace"
//	"credit_card", "ssn", "postal_code"
//
//	// Security sanitizers
//	"strip_html", "escape_html", "xss", "sql_string", "sql_identifier"
//	"path", "user_input", "secure_filename", "no_control", "no_null"
//
//	// Composite sanitizers
//	"username" (alphanum + lower + trim)
//	"slug" (kebab + trim)
//	"name" (title + no_spaces + trim)
//	"text" (no_spaces + trim)
//	"safe_text" (escape_html + no_spaces + trim)
//	"safe_html" (xss + trim)
//
// # Custom Sanitizer Registration
//
// Register your own sanitization functions:
//
//	// Register a custom sanitizer
//	sanitizer.RegisterSanitizer("remove_emoji", func(s string) string {
//		// Implementation to remove emoji characters
//		return removeEmoji(s)
//	})
//
//	// Use in struct tags
//	type Post struct {
//		Title string `sanitize:"trim,remove_emoji"`
//	}
//
// # Functional Composition
//
// Build sanitization pipelines using functional composition:
//
//	// Create a reusable sanitization pipeline
//	cleanName := sanitizer.Compose(
//		sanitizer.Trim,
//		sanitizer.ToTitle,
//		sanitizer.RemoveExtraWhitespace,
//	)
//
//	// Apply pipeline to values
//	result := cleanName("  john   doe  ")  // "JOHN DOE"
//
//	// Or apply transformations in sequence
//	result = sanitizer.Apply("  HELLO WORLD  ",
//		sanitizer.Trim,
//		sanitizer.ToLower,
//		sanitizer.ToKebabCase,
//	)  // "hello-world"
//
// # Security Best Practices
//
// When using this package for security:
//
//   - Always sanitize user input at application boundaries
//   - Use parameterized queries as primary SQL injection defense
//   - Apply context-appropriate sanitization (HTML vs SQL vs filename)
//   - Validate input lengths to prevent DoS attacks
//   - Log suspicious input patterns for monitoring
//   - Test sanitization with malicious inputs
//   - Keep security sanitization rules updated
//   - Use HTTPS when normalizing URLs
//   - Sanitize filenames before filesystem operations
//   - Remove control characters from user content
package sanitizer
