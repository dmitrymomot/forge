package objectstore

import (
	"context"
	"io"
	"iter"
	"time"
)

// Info describes a stored object. List entries carry Key, Size, and ModTime
// only — ContentType is populated by Get and Stat (backends do not return
// content types when listing).
type Info struct {
	// ModTime is the backend's last-modification stamp. Put returns it zero:
	// the authoritative stamp is assigned by the backend and read via Stat.
	ModTime time.Time
	// Key is the object key as the caller addressed it (tenant prefix
	// stripped on scoped buckets).
	Key string
	// ContentType is the object's MIME type as detected from its leading
	// bytes at Put time (never trusted from the caller).
	ContentType string
	// Size is the object's length in bytes.
	Size int64
}

// Store is the blob-backend seam. Implementations ship in this package
// (Memory, Disk) and in driver subpackages (objectstore/s3). Keys are
// slash-separated relative paths; every implementation rejects keys failing
// ValidateKey with ErrInvalidKey, so stores are safe to use standalone —
// the Bucket facade adds content validation and tenancy on top. The
// storetest subpackage is the executable contract.
//
// Put streams r to the backend under key, recording contentType; it must not
// leave a partial object behind when r fails mid-stream. Get returns the
// object's content and Info; the caller closes the reader. Delete is
// idempotent: removing an absent key is not an error. List yields objects
// whose key starts with prefix (a raw string prefix, not a path boundary) in
// backend-defined order; iteration stops at the first yielded error.
type Store interface {
	Put(ctx context.Context, key, contentType string, r io.Reader) error
	Get(ctx context.Context, key string) (io.ReadCloser, Info, error)
	Stat(ctx context.Context, key string) (Info, error)
	Delete(ctx context.Context, key string) error
	List(ctx context.Context, prefix string) iter.Seq2[Info, error]
}

// URLSigner is the optional presigning capability a Store may implement
// (objectstore/s3 does). Bucket surfaces it via SignedGetURL/SignedPutURL and
// reports ErrNotSupported when the backend lacks it.
type URLSigner interface {
	// SignedGetURL returns a URL that grants a plain HTTP GET of key for ttl.
	SignedGetURL(ctx context.Context, key string, ttl time.Duration) (string, error)
	// SignedPutURL returns a URL that grants a plain HTTP PUT to key for ttl.
	// Uploads through it bypass Bucket's validation — validate after the
	// fact (Stat) or confine such keys to a quarantine prefix.
	SignedPutURL(ctx context.Context, key string, ttl time.Duration) (string, error)
}
