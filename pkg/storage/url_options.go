package storage

import "time"

// URLOption configures URL generation.
type URLOption func(*urlOptions)

// urlOptions holds configuration for URL generation.
type urlOptions struct {
	downloadName string        // Filename for Content-Disposition: attachment
	expiry       time.Duration // Signed URL expiry duration
	forceSigned  bool          // Force signed URL regardless of ACL
	forcePublic  bool          // Force public URL regardless of ACL
}

// DefaultURLExpiry is the default expiry for signed URLs.
const DefaultURLExpiry = 15 * time.Minute

// WithExpiry sets the expiry duration for signed URLs.
// Default is 15 minutes.
func WithExpiry(d time.Duration) URLOption {
	return func(o *urlOptions) {
		o.expiry = d
	}
}

// WithDownload sets the filename for Content-Disposition: attachment header.
// This forces the browser to download the file with the specified name.
// Also implies a signed URL.
func WithDownload(filename string) URLOption {
	return func(o *urlOptions) {
		o.downloadName = filename
		o.forceSigned = true
	}
}

// WithSigned forces a signed URL regardless of the file's ACL.
// Optionally set the expiry duration; if zero, uses default expiry.
func WithSigned(expiry time.Duration) URLOption {
	return func(o *urlOptions) {
		o.forceSigned = true
		if expiry > 0 {
			o.expiry = expiry
		}
	}
}

// WithPublic forces a public URL regardless of the file's ACL.
// Note: This only works if the file was uploaded with ACLPublicRead
// or if the bucket has public access configured.
func WithPublic() URLOption {
	return func(o *urlOptions) {
		o.forcePublic = true
	}
}
