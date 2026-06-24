package storage

// Option configures Put operations.
type Option func(*putOptions)

// putOptions holds configuration for Put operations.
type putOptions struct {
	key             string           // Explicit S3 key (prevents auto-generation)
	prefix          string           // Path component within the key
	tenant          string           // First path component for isolation
	contentType     string           // Skip auto-detection with explicit type
	filenameExt     string           // Extension hint from a source filename (e.g. ".pdf")
	acl             ACL              // Upload ACL setting
	validationRules []ValidationRule // Applied before upload
	allowPrivateURL bool             // PutFromURL: permit private/loopback/link-local destinations
}

// WithKey sets an explicit storage key, replacing the auto-generated ULID-based key.
// Use this to overwrite an existing file at a specific location.
func WithKey(key string) Option {
	return func(o *putOptions) {
		o.key = key
	}
}

// WithPrefix sets a path prefix for the uploaded file.
// The prefix is added after the tenant (if any) and before the filename.
// Multi-segment prefixes are preserved: each segment is sanitized individually
// and the '/' separators are kept.
// Example: WithPrefix("avatars") results in "avatars/{ulid}.{ext}"
// Example: WithPrefix("users/avatars") results in "users/avatars/{ulid}.{ext}"
func WithPrefix(prefix string) Option {
	return func(o *putOptions) {
		o.prefix = prefix
	}
}

// WithTenant sets a tenant ID for multi-tenant isolation.
// The tenant ID becomes the first path segment.
// Example: WithTenant("tenant123") results in "tenant123/{prefix}/{ulid}.{ext}"
func WithTenant(id string) Option {
	return func(o *putOptions) {
		o.tenant = id
	}
}

// WithContentType overrides the auto-detected content type.
// Use sparingly; auto-detection from magic bytes is preferred.
func WithContentType(ct string) Option {
	return func(o *putOptions) {
		o.contentType = ct
	}
}

// WithACL overrides the default ACL for this upload.
func WithACL(acl ACL) Option {
	return func(o *putOptions) {
		o.acl = acl
	}
}

// WithValidation adds validation rules to be applied before upload.
// If any rule fails, the upload is aborted and a *FileValidationError is returned.
func WithValidation(rules ...ValidationRule) Option {
	return func(o *putOptions) {
		o.validationRules = append(o.validationRules, rules...)
	}
}

// WithAllowPrivateURL disables PutFromURL's SSRF protection, permitting
// downloads from private, loopback, link-local, or unspecified addresses.
//
// PutFromURL blocks these destinations by default so an attacker-supplied URL
// cannot reach internal endpoints. Only use this for trusted, internal source
// URLs (e.g. service-to-service transfers). It has no effect on other upload
// helpers.
func WithAllowPrivateURL() Option {
	return func(o *putOptions) {
		o.allowPrivateURL = true
	}
}

// withFilenameExt records a sanitized extension hint derived from a source
// filename. It is unexported because callers should pass real filenames via the
// helper functions (e.g. PutBytes) rather than set the hint directly.
func withFilenameExt(ext string) Option {
	return func(o *putOptions) {
		o.filenameExt = ext
	}
}
