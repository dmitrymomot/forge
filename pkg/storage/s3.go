package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/dmitrymomot/forge/pkg/id"
)

// S3Storage implements Storage using S3-compatible object storage.
type S3Storage struct {
	client    *s3.Client
	presigner *s3.PresignClient
	cfg       Config
}

// New creates a new S3Storage with the given configuration.
func New(cfg Config) (*S3Storage, error) {
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	opts := []func(*s3.Options){
		func(o *s3.Options) {
			o.Region = cfg.Region
			o.Credentials = credentials.NewStaticCredentialsProvider(
				cfg.AccessKey,
				cfg.SecretKey,
				"",
			)
		},
	}

	if cfg.Endpoint != "" {
		opts = append(opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = cfg.PathStyle
		})
	}

	client := s3.New(s3.Options{}, opts...)
	presigner := s3.NewPresignClient(client)

	return &S3Storage{
		client:    client,
		presigner: presigner,
		cfg:       cfg,
	}, nil
}

// Put uploads data from a reader to S3.
func (s *S3Storage) Put(ctx context.Context, r io.Reader, size int64, opts ...Option) (*FileInfo, error) {
	o := &putOptions{
		acl: s.cfg.DefaultACL,
	}
	for _, opt := range opts {
		opt(o)
	}

	var contentType string
	var body io.ReadSeeker
	if o.contentType != "" {
		contentType = o.contentType
		if rs, ok := r.(io.ReadSeeker); ok {
			body = rs
		} else {
			data, err := io.ReadAll(r)
			if err != nil {
				return nil, fmt.Errorf("failed to read input: %w", err)
			}
			body = bytes.NewReader(data)
		}
	} else {
		contentType, body = detectMIMEWithReader(r)
	}

	// Run validation if rules present.
	if len(o.validationRules) > 0 {
		if err := ValidateReader(size, contentType, o.validationRules...); err != nil {
			return nil, err
		}
	}

	key := o.key
	if key == "" {
		key = s.buildKey(o.tenant, o.prefix, contentType)
	}

	var acl types.ObjectCannedACL
	switch o.acl {
	case ACLPublicRead:
		acl = types.ObjectCannedACLPublicRead
	default:
		acl = types.ObjectCannedACLPrivate
	}

	input := &s3.PutObjectInput{
		Bucket:        aws.String(s.cfg.Bucket),
		Key:           aws.String(key),
		Body:          body,
		ContentLength: aws.Int64(size),
		ContentType:   aws.String(contentType),
		ACL:           acl,
	}

	_, err := s.client.PutObject(ctx, input)
	if err != nil {
		return nil, wrapS3Error(err, ErrUploadFailed)
	}

	return &FileInfo{
		Key:         key,
		Size:        size,
		ContentType: contentType,
		ACL:         o.acl,
	}, nil
}

// Get retrieves a file from S3.
func (s *S3Storage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	input := &s3.GetObjectInput{
		Bucket: aws.String(s.cfg.Bucket),
		Key:    aws.String(key),
	}

	output, err := s.client.GetObject(ctx, input)
	if err != nil {
		return nil, wrapS3Error(err, ErrNotFound)
	}

	return output.Body, nil
}

// Delete removes a file from S3.
func (s *S3Storage) Delete(ctx context.Context, key string) error {
	input := &s3.DeleteObjectInput{
		Bucket: aws.String(s.cfg.Bucket),
		Key:    aws.String(key),
	}

	_, err := s.client.DeleteObject(ctx, input)
	if err != nil {
		return wrapS3Error(err, ErrDeleteFailed)
	}

	return nil
}

// URL generates a URL for accessing the file.
// By default, returns a signed URL. Use WithPublic() to get an unsigned public URL.
func (s *S3Storage) URL(ctx context.Context, key string, opts ...URLOption) (string, error) {
	o := &urlOptions{
		expiry: DefaultURLExpiry,
	}
	for _, opt := range opts {
		opt(o)
	}

	if o.forcePublic {
		return s.publicURL(key), nil
	}

	return s.signedURL(ctx, key, o)
}

// buildKey constructs a storage key from tenant, prefix, and content type.
// Format: {tenant}/{prefix}/{ulid}.{ext}
func (s *S3Storage) buildKey(tenant, prefix, contentType string) string {
	var parts []string

	if tenant != "" {
		parts = append(parts, sanitizePathSegment(tenant))
	}
	if prefix != "" {
		parts = append(parts, sanitizePathSegment(prefix))
	}

	ext := ExtFromMIME(contentType)
	if ext == "" {
		ext = ".bin"
	}
	filename := id.NewULID() + ext

	parts = append(parts, filename)

	return strings.Join(parts, "/")
}

// publicURL generates a public URL for the file.
func (s *S3Storage) publicURL(key string) string {
	if s.cfg.PublicURL != "" {
		return strings.TrimSuffix(s.cfg.PublicURL, "/") + "/" + key
	}

	// Default S3 URL format.
	if s.cfg.Endpoint != "" {
		endpoint := strings.TrimSuffix(s.cfg.Endpoint, "/")
		if s.cfg.PathStyle {
			return fmt.Sprintf("%s/%s/%s", endpoint, s.cfg.Bucket, key)
		}
		return fmt.Sprintf("%s/%s", endpoint, key)
	}

	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", s.cfg.Bucket, s.cfg.Region, key)
}

// signedURL generates a pre-signed URL for the file.
func (s *S3Storage) signedURL(ctx context.Context, key string, opts *urlOptions) (string, error) {
	input := &s3.GetObjectInput{
		Bucket: aws.String(s.cfg.Bucket),
		Key:    aws.String(key),
	}

	// Add Content-Disposition for downloads.
	if opts.downloadName != "" {
		disposition := fmt.Sprintf("attachment; filename=%q", opts.downloadName)
		input.ResponseContentDisposition = aws.String(disposition)
	}

	presignOpts := func(po *s3.PresignOptions) {
		po.Expires = opts.expiry
	}

	result, err := s.presigner.PresignGetObject(ctx, input, presignOpts)
	if err != nil {
		return "", wrapS3Error(err, ErrPresignFailed)
	}

	return result.URL, nil
}

// pathSegmentRegex matches characters that are not safe for path segments.
var pathSegmentRegex = regexp.MustCompile(`[^a-zA-Z0-9\-_.]`)

// sanitizePathSegment removes potentially dangerous characters from path segments.
// This prevents path traversal attacks and ensures safe S3 keys.
func sanitizePathSegment(segment string) string {
	// Remove leading/trailing whitespace and slashes.
	segment = strings.Trim(segment, " /\\")

	// Remove path traversal attempts.
	segment = strings.ReplaceAll(segment, "..", "")

	// Replace unsafe characters.
	segment = pathSegmentRegex.ReplaceAllString(segment, "_")

	// URL-encode the result for extra safety.
	return url.PathEscape(segment)
}

// HeadObject checks if a file exists and returns its metadata without downloading it.
func (s *S3Storage) HeadObject(ctx context.Context, key string) (*FileInfo, error) {
	input := &s3.HeadObjectInput{
		Bucket: aws.String(s.cfg.Bucket),
		Key:    aws.String(key),
	}

	output, err := s.client.HeadObject(ctx, input)
	if err != nil {
		return nil, wrapS3Error(err, ErrNotFound)
	}

	contentType := ""
	if output.ContentType != nil {
		contentType = *output.ContentType
	}

	size := int64(0)
	if output.ContentLength != nil {
		size = *output.ContentLength
	}

	return &FileInfo{
		Key:         key,
		Size:        size,
		ContentType: contentType,
		ACL:         s.cfg.DefaultACL,
	}, nil
}

// Copy copies a file from one key to another within the same bucket.
// S3 CopyObject preserves ACL by default.
func (s *S3Storage) Copy(ctx context.Context, srcKey, dstKey string) error {
	input := &s3.CopyObjectInput{
		Bucket:     aws.String(s.cfg.Bucket),
		Key:        aws.String(dstKey),
		CopySource: aws.String(s.cfg.Bucket + "/" + srcKey),
	}

	_, err := s.client.CopyObject(ctx, input)
	if err != nil {
		return wrapS3Error(err, ErrUploadFailed)
	}

	return nil
}

// Ensure S3Storage implements Storage.
var _ Storage = (*S3Storage)(nil)
