// Package s3test provisions a throwaway MinIO server for integration tests
// of S3-backed packages.
//
// Its API is only built under the "integration" build tag; see s3test.go.
// This file keeps the package non-empty under the default build so that
// `go build ./...` compiles it without pulling in the testcontainers
// dependency tree.
package s3test
