// Package clickhousetest provisions a throwaway ClickHouse for integration tests.
//
// Its API is only built under the "integration" build tag; see clickhousetest.go.
// This file keeps the package non-empty under the default build so that
// `go build ./...` compiles it without pulling in the testcontainers
// dependency tree.
package clickhousetest
