package storage

// Option configures Put operations.
type Option func(*putOptions)

// putOptions holds configuration for Put operations.
type putOptions struct {
	key             string           // Explicit key (replaces auto-generated)
	prefix          string           // Path prefix (e.g., "avatars/")
	tenant          string           // Tenant ID for multi-tenant isolation
	contentType     string           // Override detected content type
	acl             ACL              // Override default ACL
	validationRules []ValidationRule // Validation rules to apply before upload
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
// Example: WithPrefix("avatars") results in "avatars/{ulid}.{ext}"
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
