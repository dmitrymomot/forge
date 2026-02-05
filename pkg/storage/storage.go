package storage

import (
	"context"
	"io"
)

// Storage defines the interface for file storage operations.
type Storage interface {
	// Put uploads data from a reader to storage.
	// The size parameter is used for content-length header.
	// Options can customize key, prefix, tenant, ACL, and content type.
	Put(ctx context.Context, r io.Reader, size int64, opts ...Option) (*FileInfo, error)

	// Get retrieves a file from storage.
	// The caller is responsible for closing the returned reader.
	Get(ctx context.Context, key string) (io.ReadCloser, error)

	// Delete removes a file from storage.
	Delete(ctx context.Context, key string) error

	// URL generates a URL for accessing the file.
	// For private files, returns a signed URL. For public files, returns the public URL.
	// Use URLOptions to customize expiry, download disposition, or force signed/public.
	URL(ctx context.Context, key string, opts ...URLOption) (string, error)
}

// Config holds S3-compatible storage configuration.
type Config struct {
	// Bucket is the S3 bucket name (required).
	Bucket string

	// AccessKey is the AWS access key ID (required).
	AccessKey string

	// SecretKey is the AWS secret access key (required).
	SecretKey string

	// Endpoint is the custom S3 endpoint URL (optional, for MinIO or other S3-compatible services).
	Endpoint string

	// Region is the AWS region (default: us-east-1).
	Region string

	// PublicURL is the CDN or public URL prefix for public files (optional).
	// If set, public URLs will use this prefix instead of the S3 URL.
	PublicURL string

	// DefaultACL is the default ACL for uploaded files (default: private).
	DefaultACL ACL

	// PathStyle enables path-style URLs (required for MinIO).
	PathStyle bool

	// MaxDownloadSize is the maximum size for URL downloads in bytes (default: 50MB).
	MaxDownloadSize int64
}

// FileInfo contains metadata about an uploaded file.
type FileInfo struct {
	// Key is the storage key (path) for the file.
	Key string

	// ContentType is the detected MIME type.
	ContentType string

	// ACL is the access control setting.
	ACL ACL

	// Size is the file size in bytes.
	Size int64
}

// ACL represents access control levels for stored files.
type ACL string

const (
	// ACLPrivate makes the file accessible only via signed URLs.
	ACLPrivate ACL = "private"

	// ACLPublicRead makes the file publicly readable.
	ACLPublicRead ACL = "public-read"
)

// Default configuration values.
const (
	DefaultRegion          = "us-east-1"
	DefaultMaxDownloadSize = 50 << 20 // 50MB
	DefaultSignedURLExpiry = 15 * 60  // 15 minutes in seconds
)

// applyDefaults fills in default values for empty config fields.
func (c *Config) applyDefaults() {
	if c.Region == "" {
		c.Region = DefaultRegion
	}
	if c.DefaultACL == "" {
		c.DefaultACL = ACLPrivate
	}
	if c.MaxDownloadSize == 0 {
		c.MaxDownloadSize = DefaultMaxDownloadSize
	}
}

// validate checks that required configuration fields are set.
func (c *Config) validate() error {
	if c.Bucket == "" {
		return ErrInvalidConfig
	}
	if c.AccessKey == "" {
		return ErrInvalidConfig
	}
	if c.SecretKey == "" {
		return ErrInvalidConfig
	}
	return nil
}
