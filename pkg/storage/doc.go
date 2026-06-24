// Package storage provides S3-compatible file storage operations.
//
// It offers a simple interface for uploading, retrieving, and managing files
// with automatic MIME detection, validation, and multi-tenant support.
//
// # Basic Usage
//
// Create a storage client and upload files:
//
//	cfg := storage.Config{
//		Bucket:    "my-bucket",
//		Region:    "us-east-1",
//		AccessKey: os.Getenv("AWS_ACCESS_KEY_ID"),
//		SecretKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
//	}
//
//	store, err := storage.New(cfg)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Upload from form
//	fh, _ := c.FormFile("avatar")
//	info, err := storage.PutFile(ctx, store, fh,
//		storage.WithPrefix("avatars"),
//		storage.WithACL(storage.ACLPublicRead),
//	)
//
// # Validation
//
// Use WithValidation for validated uploads:
//
//	info, err := storage.PutFile(ctx, store, fh,
//		storage.WithValidation(
//			storage.MaxSize(5 << 20),  // 5MB
//			storage.ImageOnly(),
//		),
//		storage.WithTenant(tenantID),
//		storage.WithPrefix("avatars"),
//	)
//	if err != nil {
//		var verr *storage.FileValidationError
//		if errors.As(err, &verr) {
//			// Handle validation error
//		}
//	}
//
// # URL Generation
//
// Generate URLs for stored files:
//
//	// Auto-detect based on ACL (public vs signed)
//	url, err := store.URL(ctx, info.Key)
//
//	// Force signed URL with custom expiry
//	url, err := store.URL(ctx, info.Key,
//		storage.WithSigned(time.Hour),
//	)
//
//	// Signed URL with download disposition
//	url, err := store.URL(ctx, info.Key,
//		storage.WithDownload("document.pdf"),
//	)
//
// # Multi-Tenant Support
//
// Use WithTenant for tenant isolation:
//
//	info, err := storage.PutFile(ctx, store, fh,
//		storage.WithTenant(tenantID),
//		storage.WithPrefix("documents"),
//	)
//	// Key: {tenant}/{prefix}/{ulid}.{ext}
//
// # Configuration
//
// The Config struct is populated from environment variables via its `env` tags.
// The names below are the exact tag values; the application typically loads them
// under a prefix (e.g. STORAGE_), so the effective variable is the prefix plus
// the tag name (STORAGE_BUCKET, STORAGE_MAX_DOWNLOAD_SIZE, ...).
//
//	type Config struct {
//		Bucket          string `env:"BUCKET,required"`
//		AccessKey       string `env:"ACCESS_KEY,required"`
//		SecretKey       string `env:"SECRET_KEY,required"`
//		Endpoint        string `env:"ENDPOINT"`                          // for MinIO/custom S3
//		Region          string `env:"REGION" envDefault:"us-east-1"`
//		PublicURL       string `env:"PUBLIC_URL"`                        // CDN URL
//		DefaultACL      string `env:"DEFAULT_ACL" envDefault:"private"`
//		PathStyle       bool   `env:"PATH_STYLE" envDefault:"false"`    // for MinIO
//		MaxDownloadSize int64  `env:"MAX_DOWNLOAD_SIZE" envDefault:"52428800"` // 50MB
//	}
//
// # Fetching from URLs (SSRF)
//
// PutFromURL treats its source URL as untrusted and, by default, refuses to
// connect to private, loopback, link-local, or unspecified addresses (enforced
// on the resolved IP at dial time, which also blocks DNS-rebinding). Use
// WithAllowPrivateURL to opt out for trusted internal source URLs.
package storage
