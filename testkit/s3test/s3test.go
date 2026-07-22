//go:build integration

package s3test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/testcontainers/testcontainers-go/modules/minio"

	"github.com/dmitrymomot/forge/core/id"
)

var (
	sharedOnce     sync.Once
	sharedEndpoint string
	sharedAccess   string
	sharedSecret   string
)

// Client returns an S3 client wired to a server to test against.
//
// If FORGE_TEST_S3_ENDPOINT is set it points the suite at an existing
// S3-compatible server (with FORGE_TEST_S3_ACCESS_KEY and
// FORGE_TEST_S3_SECRET_KEY as credentials). Otherwise a throwaway
// minio container is started once per test process, shared across every test
// in the package, and removed by the testcontainers Ryuk reaper when the
// process exits. The client uses path-style addressing, so presigned URLs it
// mints resolve without virtual-host DNS.
func Client(tb testing.TB) *awss3.Client {
	tb.Helper()
	endpoint, access, secret := target()
	return awss3.New(awss3.Options{
		BaseEndpoint: aws.String(endpoint),
		Region:       "us-east-1",
		Credentials:  credentials.NewStaticCredentialsProvider(access, secret, ""),
		UsePathStyle: true,
	})
}

// Bucket creates a uniquely named bucket through client and returns its
// name. The bucket and its contents are removed on test cleanup
// (best-effort — the throwaway container is discarded anyway; the cleanup
// matters when targeting a live server via env).
func Bucket(tb testing.TB, client *awss3.Client) string {
	tb.Helper()
	ctx := context.Background()
	name := "test-" + strings.ToLower(id.NewShort().String())
	if _, err := client.CreateBucket(ctx, &awss3.CreateBucketInput{Bucket: aws.String(name)}); err != nil {
		tb.Fatalf("s3test: create bucket: %v", err)
	}
	tb.Cleanup(func() {
		pager := awss3.NewListObjectsV2Paginator(client, &awss3.ListObjectsV2Input{Bucket: aws.String(name)})
		for pager.HasMorePages() {
			page, err := pager.NextPage(ctx)
			if err != nil {
				return
			}
			for _, obj := range page.Contents {
				_, _ = client.DeleteObject(ctx, &awss3.DeleteObjectInput{Bucket: aws.String(name), Key: obj.Key})
			}
		}
		_, _ = client.DeleteBucket(ctx, &awss3.DeleteBucketInput{Bucket: aws.String(name)})
	})
	return name
}

// target resolves the endpoint and credentials, preferring the env override.
func target() (endpoint, access, secret string) {
	if ep := os.Getenv("FORGE_TEST_S3_ENDPOINT"); ep != "" {
		if !strings.Contains(ep, "://") {
			ep = "http://" + ep
		}
		return ep, os.Getenv("FORGE_TEST_S3_ACCESS_KEY"), os.Getenv("FORGE_TEST_S3_SECRET_KEY")
	}
	sharedOnce.Do(startShared)
	return sharedEndpoint, sharedAccess, sharedSecret
}

// startShared boots the shared container. It panics rather than failing a
// single test: a Goexit inside sync.Once still marks it done, which would
// leave the shared address empty and make every later caller dial "".
func startShared() {
	ctx := context.Background()
	c, err := minio.Run(ctx, "minio/minio:RELEASE.2024-01-16T16-07-38Z")
	if err != nil {
		panic(fmt.Sprintf("s3test: start container: %v", err))
	}
	conn, err := c.ConnectionString(ctx)
	if err != nil {
		panic(fmt.Sprintf("s3test: connection string: %v", err))
	}
	sharedEndpoint = "http://" + conn
	sharedAccess = c.Username
	sharedSecret = c.Password
}
