package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/dmitrymomot/forge/data/objectstore"
)

// Store is an objectstore.Store on an S3(-compatible) bucket. It also
// implements objectstore.URLSigner, so a Bucket over it serves presigned
// GET/PUT URLs. The caller owns the client's lifecycle and credentials.
type Store struct {
	client *awss3.Client
	// manager is deprecated in favor of feature/s3/transfermanager, but the
	// successor is still v0.x — migrate once it reaches a stable v1.
	uploader *manager.Uploader //nolint:staticcheck
	presign  *awss3.PresignClient
	bucket   string
}

// New returns a Store writing to bucket through client. It panics on a nil
// client or empty bucket — both are wiring bugs, not runtime conditions.
func New(client *awss3.Client, bucket string) *Store {
	if client == nil {
		panic("objectstore/s3: nil client")
	}
	if bucket == "" {
		panic("objectstore/s3: empty bucket")
	}
	return &Store{
		client:   client,
		uploader: manager.NewUploader(client), //nolint:staticcheck // see Store.uploader

		presign: awss3.NewPresignClient(client),
		bucket:  bucket,
	}
}

// Put streams r to the bucket under key. Streams larger than the SDK's part
// size upload as multipart; a failed upload is aborted, leaving no object.
func (s *Store) Put(ctx context.Context, key, contentType string, r io.Reader) error {
	if err := objectstore.ValidateKey(key); err != nil {
		return err
	}
	_, err := s.uploader.Upload(ctx, &awss3.PutObjectInput{ //nolint:staticcheck // see Store.uploader
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        r,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("objectstore/s3: upload: %w", err)
	}
	return nil
}

// Get returns the object's content and Info; the caller closes the reader.
func (s *Store) Get(ctx context.Context, key string) (io.ReadCloser, objectstore.Info, error) {
	if err := objectstore.ValidateKey(key); err != nil {
		return nil, objectstore.Info{}, err
	}
	out, err := s.client.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, objectstore.Info{}, fmt.Errorf("%w: %q", objectstore.ErrNotFound, key)
		}
		return nil, objectstore.Info{}, fmt.Errorf("objectstore/s3: get: %w", err)
	}
	info := objectstore.Info{
		Key:         key,
		ContentType: aws.ToString(out.ContentType),
		Size:        aws.ToInt64(out.ContentLength),
		ModTime:     aws.ToTime(out.LastModified),
	}
	return out.Body, info, nil
}

// Stat returns the object's Info via a HEAD request.
func (s *Store) Stat(ctx context.Context, key string) (objectstore.Info, error) {
	if err := objectstore.ValidateKey(key); err != nil {
		return objectstore.Info{}, err
	}
	out, err := s.client.HeadObject(ctx, &awss3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNotFound(err) {
			return objectstore.Info{}, fmt.Errorf("%w: %q", objectstore.ErrNotFound, key)
		}
		return objectstore.Info{}, fmt.Errorf("objectstore/s3: head: %w", err)
	}
	return objectstore.Info{
		Key:         key,
		ContentType: aws.ToString(out.ContentType),
		Size:        aws.ToInt64(out.ContentLength),
		ModTime:     aws.ToTime(out.LastModified),
	}, nil
}

// Delete removes the object; S3 deletes are idempotent, so an absent key is
// not an error.
func (s *Store) Delete(ctx context.Context, key string) error {
	if err := objectstore.ValidateKey(key); err != nil {
		return err
	}
	_, err := s.client.DeleteObject(ctx, &awss3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("objectstore/s3: delete: %w", err)
	}
	return nil
}

// List yields objects whose key starts with prefix in lexicographic order,
// paging through the bucket as the consumer advances.
func (s *Store) List(ctx context.Context, prefix string) iter.Seq2[objectstore.Info, error] {
	return func(yield func(objectstore.Info, error) bool) {
		if err := objectstore.ValidatePrefix(prefix); err != nil {
			yield(objectstore.Info{}, err)
			return
		}
		input := &awss3.ListObjectsV2Input{Bucket: aws.String(s.bucket)}
		if prefix != "" {
			input.Prefix = aws.String(prefix)
		}
		pager := awss3.NewListObjectsV2Paginator(s.client, input)
		for pager.HasMorePages() {
			page, err := pager.NextPage(ctx)
			if err != nil {
				yield(objectstore.Info{}, fmt.Errorf("objectstore/s3: list: %w", err))
				return
			}
			for _, obj := range page.Contents {
				info := objectstore.Info{
					Key:     aws.ToString(obj.Key),
					Size:    aws.ToInt64(obj.Size),
					ModTime: aws.ToTime(obj.LastModified),
				}
				if !yield(info, nil) {
					return
				}
			}
		}
	}
}

// SignedGetURL returns a presigned URL granting a plain HTTP GET of key for
// ttl.
func (s *Store) SignedGetURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if err := objectstore.ValidateKey(key); err != nil {
		return "", err
	}
	req, err := s.presign.PresignGetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, awss3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("objectstore/s3: presign get: %w", err)
	}
	return req.URL, nil
}

// SignedPutURL returns a presigned URL granting a plain HTTP PUT to key for
// ttl. Uploads through it bypass Bucket validation — see
// objectstore.URLSigner.
func (s *Store) SignedPutURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if err := objectstore.ValidateKey(key); err != nil {
		return "", err
	}
	req, err := s.presign.PresignPutObject(ctx, &awss3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, awss3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("objectstore/s3: presign put: %w", err)
	}
	return req.URL, nil
}

// isNotFound reports whether err is S3's missing-object answer: NoSuchKey
// from GetObject or the bare 404 NotFound HeadObject returns.
func isNotFound(err error) bool {
	var noKey *types.NoSuchKey
	var notFound *types.NotFound
	return errors.As(err, &noKey) || errors.As(err, &notFound)
}
