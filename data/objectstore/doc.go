// Package objectstore stores blobs behind one small Store seam with a
// validating Bucket facade on top. The facade enforces a portable key
// grammar (no traversal, safe on every backend), detects content types from
// magic bytes via core/filetype (the caller's claim is never trusted), caps
// sizes, and optionally confines every operation to a tenant's key prefix.
//
// Two backends ship in this package: Memory for tests and development, and
// Disk — a path-traversal-safe adapter that confines all access to one
// directory via os.Root and writes atomically. Consumers implement Store for
// other backends (an S3-compatible service, for example) and prove them with
// the storetest conformance suite; backends without presigning report
// ErrNotSupported from the signed-URL methods.
//
// Multi-tenant apps opt in with WithScope: keys are transparently stored
// under "<tenant>/", List sees only the tenant's objects, and a missing or
// invalid scope fails closed with ErrScope. Single-tenant apps skip the
// option and pay nothing.
//
// # Usage
//
//	disk, err := objectstore.NewDisk("/var/lib/app/uploads")
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer disk.Close()
//
//	bucket, err := objectstore.New(disk,
//		objectstore.WithMaxSize(10<<20),
//		objectstore.WithAllowedTypes("image/png", "image/jpeg", "image/webp"),
//		objectstore.WithScope(tenantFromContext), // omit in single-tenant apps
//	)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Upload: type is detected from content, size capped, key validated.
//	info, err := bucket.Put(ctx, "avatars/u123.png", file)
//
//	// Download: caller closes the reader.
//	rc, info, err := bucket.Get(ctx, "avatars/u123.png")
//	if err != nil {
//		return err
//	}
//	defer rc.Close()
//
//	// Enumerate a folder-like prefix.
//	for info, err := range bucket.List(ctx, "avatars/") {
//		if err != nil {
//			return err
//		}
//		fmt.Println(info.Key, info.Size)
//	}
package objectstore
