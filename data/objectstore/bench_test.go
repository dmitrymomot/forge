package objectstore_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/data/objectstore"
)

func BenchmarkValidateKey(b *testing.B) {
	keys := map[string]string{
		"short": "avatars/u123.png",
		"deep":  "tenants/acme/projects/42/exports/2026/07/report-final.parquet",
		"long":  strings.Repeat("segment/", 50) + "leaf.bin",
	}
	for name, key := range keys {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if err := objectstore.ValidateKey(key); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func benchPayload() []byte {
	payload := make([]byte, 64<<10)
	copy(payload, pngHead)
	return payload
}

func BenchmarkBucketPutMemory(b *testing.B) {
	ctx := context.Background()
	payload := benchPayload()
	bkt, err := objectstore.New(objectstore.NewMemory(),
		objectstore.WithMaxSize(1<<20),
		objectstore.WithAllowedTypes("image/png"),
	)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	for b.Loop() {
		if _, err := bkt.Put(ctx, "bench/object.png", bytes.NewReader(payload)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBucketPutMemoryScoped(b *testing.B) {
	ctx := context.Background()
	payload := benchPayload()
	bkt, err := objectstore.New(objectstore.NewMemory(),
		objectstore.WithScope(func(context.Context) (string, error) { return "tenant-1", nil }),
	)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	for b.Loop() {
		if _, err := bkt.Put(ctx, "bench/object.png", bytes.NewReader(payload)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBucketGetMemory(b *testing.B) {
	ctx := context.Background()
	payload := benchPayload()
	bkt, err := objectstore.New(objectstore.NewMemory())
	if err != nil {
		b.Fatal(err)
	}
	if _, err := bkt.Put(ctx, "bench/object.png", bytes.NewReader(payload)); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		rc, _, err := bkt.Get(ctx, "bench/object.png")
		if err != nil {
			b.Fatal(err)
		}
		_ = rc.Close()
	}
}

func BenchmarkDiskPut(b *testing.B) {
	ctx := context.Background()
	payload := benchPayload()
	d, err := objectstore.NewDisk(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	for b.Loop() {
		if err := d.Put(ctx, "bench/object.png", "image/png", bytes.NewReader(payload)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDiskGet(b *testing.B) {
	ctx := context.Background()
	payload := benchPayload()
	d, err := objectstore.NewDisk(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	if err := d.Put(ctx, "bench/object.png", "image/png", bytes.NewReader(payload)); err != nil {
		b.Fatal(err)
	}
	buf := make([]byte, 32<<10)
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	for b.Loop() {
		rc, _, err := d.Get(ctx, "bench/object.png")
		if err != nil {
			b.Fatal(err)
		}
		for {
			if _, err := rc.Read(buf); err != nil {
				break
			}
		}
		_ = rc.Close()
	}
}

func BenchmarkMemoryList(b *testing.B) {
	ctx := context.Background()
	m := objectstore.NewMemory()
	for i := range 1000 {
		key := "objects/" + strings.Repeat("x", i%7+1) + "/" + strings.Repeat("y", i%13+1)
		_ = m.Put(ctx, key, "application/octet-stream", bytes.NewReader(binBlob))
	}
	b.ReportAllocs()
	for b.Loop() {
		var n int
		for _, err := range m.List(ctx, "objects/") {
			if err != nil {
				b.Fatal(err)
			}
			n++
		}
	}
}
