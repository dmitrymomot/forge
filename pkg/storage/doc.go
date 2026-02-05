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
// The Config struct supports environment variables:
//
//	type Config struct {
//		Bucket          string // STORAGE_BUCKET
//		AccessKey       string // STORAGE_ACCESS_KEY
//		SecretKey       string // STORAGE_SECRET_KEY
//		Endpoint        string // STORAGE_ENDPOINT (for MinIO/custom S3)
//		Region          string // STORAGE_REGION (default: us-east-1)
//		PublicURL       string // STORAGE_PUBLIC_URL (CDN URL)
//		DefaultACL      ACL    // STORAGE_DEFAULT_ACL (default: private)
//		PathStyle       bool   // STORAGE_PATH_STYLE (for MinIO)
//		MaxDownloadSize int64  // STORAGE_MAX_DOWNLOAD (default: 50MB)
//	}
package storage
