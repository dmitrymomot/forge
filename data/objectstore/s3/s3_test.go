//go:build integration

package s3_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/data/objectstore"
	objs3 "github.com/dmitrymomot/forge/data/objectstore/s3"
	"github.com/dmitrymomot/forge/data/objectstore/storetest"
	"github.com/dmitrymomot/forge/testkit/s3test"
)

// pngHead is a valid PNG signature followed by filler — detected as image/png.
var pngHead = append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, bytes.Repeat([]byte{0xAB}, 64)...)

func newStore(t *testing.T) *objs3.Store {
	t.Helper()
	client := s3test.Client(t)
	return objs3.New(client, s3test.Bucket(t, client))
}

func TestS3Conformance(t *testing.T) {
	storetest.Run(t, func(t *testing.T) objectstore.Store {
		t.Helper()
		return newStore(t)
	})
}

func TestS3NewPanics(t *testing.T) {
	t.Parallel()
	assertPanics := func(name string, fn func()) {
		defer func() {
			if recover() == nil {
				t.Errorf("%s did not panic", name)
			}
		}()
		fn()
	}
	assertPanics("nil client", func() { objs3.New(nil, "bucket") })
	assertPanics("empty bucket", func() { objs3.New(s3test.Client(t), "") })
}

func TestS3PresignedURLs(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	if err := store.Put(ctx, "signed/pic.png", "image/png", bytes.NewReader(pngHead)); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Presigned GET serves the object over plain HTTP.
	getURL, err := store.SignedGetURL(ctx, "signed/pic.png", time.Minute)
	if err != nil {
		t.Fatalf("SignedGetURL: %v", err)
	}
	resp, err := http.Get(getURL)
	if err != nil {
		t.Fatalf("GET presigned: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET presigned status = %d", resp.StatusCode)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read presigned body: %v", err)
	}
	if !bytes.Equal(got, pngHead) {
		t.Fatal("presigned GET content mismatch")
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}

	// Presigned PUT uploads the object over plain HTTP.
	putURL, err := store.SignedPutURL(ctx, "signed/uploaded.bin", time.Minute)
	if err != nil {
		t.Fatalf("SignedPutURL: %v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, putURL, strings.NewReader("uploaded via presigned url"))
	if err != nil {
		t.Fatal(err)
	}
	putResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT presigned: %v", err)
	}
	defer func() { _ = putResp.Body.Close() }()
	if putResp.StatusCode != http.StatusOK {
		t.Fatalf("PUT presigned status = %d", putResp.StatusCode)
	}
	info, err := store.Stat(ctx, "signed/uploaded.bin")
	if err != nil {
		t.Fatalf("Stat uploaded: %v", err)
	}
	if info.Size != int64(len("uploaded via presigned url")) {
		t.Errorf("Size = %d", info.Size)
	}

	// An expired URL is refused.
	shortURL, err := store.SignedGetURL(ctx, "signed/pic.png", time.Second)
	if err != nil {
		t.Fatalf("SignedGetURL short: %v", err)
	}
	time.Sleep(1500 * time.Millisecond)
	expResp, err := http.Get(shortURL)
	if err != nil {
		t.Fatalf("GET expired: %v", err)
	}
	defer func() { _ = expResp.Body.Close() }()
	if expResp.StatusCode == http.StatusOK {
		t.Fatal("expired presigned URL still worked")
	}
}

func TestS3ThroughBucketFacade(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	bucket, err := objectstore.New(store,
		objectstore.WithMaxSize(1<<20),
		objectstore.WithAllowedTypes("image/png"),
		objectstore.WithScope(func(context.Context) (string, error) { return "tenant-1", nil }),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	info, err := bucket.Put(ctx, "avatar.png", bytes.NewReader(pngHead))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if info.ContentType != "image/png" || info.Size != int64(len(pngHead)) {
		t.Errorf("Info = %+v", info)
	}

	// The backend key is tenant-prefixed.
	if _, err := store.Stat(ctx, "tenant-1/avatar.png"); err != nil {
		t.Fatalf("backend Stat: %v", err)
	}

	// Rejections happen before any S3 write.
	if _, err := bucket.Put(ctx, "note.txt", strings.NewReader("not an image")); !errors.Is(err, objectstore.ErrUnsupportedType) {
		t.Fatalf("Put text = %v, want ErrUnsupportedType", err)
	}

	// The size cap aborts the streamed upload and leaves no object behind.
	huge := io.MultiReader(bytes.NewReader(pngHead), bytes.NewReader(make([]byte, 2<<20)))
	if _, err := bucket.Put(ctx, "huge.png", huge); !errors.Is(err, objectstore.ErrTooLarge) {
		t.Fatalf("Put huge = %v, want ErrTooLarge", err)
	}
	if _, err := bucket.Stat(ctx, "huge.png"); !errors.Is(err, objectstore.ErrNotFound) {
		t.Fatalf("Stat huge = %v, want ErrNotFound", err)
	}

	// Presigned URLs flow through the facade with the scoped key.
	url, err := bucket.SignedGetURL(ctx, "avatar.png", time.Minute)
	if err != nil {
		t.Fatalf("SignedGetURL: %v", err)
	}
	if !strings.Contains(url, "tenant-1/avatar.png") {
		t.Errorf("presigned URL %q lacks scoped key", url)
	}
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET presigned: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET presigned status = %d", resp.StatusCode)
	}
}

func TestS3MultipartUpload(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	// Larger than the uploader's 5 MiB default part size, streamed without a
	// known length, so it exercises the multipart path.
	const size = 6 << 20
	payload := bytes.Repeat([]byte("forge-multipart-"), size/16)
	if err := store.Put(ctx, "big/object.bin", "application/octet-stream", onlyReader{bytes.NewReader(payload)}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	rc, info, err := store.Get(ctx, "big/object.bin")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer func() { _ = rc.Close() }()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("content mismatch")
	}
	if info.Size != int64(len(payload)) {
		t.Errorf("Size = %d, want %d", info.Size, len(payload))
	}
}

// onlyReader hides Len/Seek so the SDK cannot learn the stream's length.
type onlyReader struct{ r io.Reader }

func (o onlyReader) Read(p []byte) (int, error) { return o.r.Read(p) }
