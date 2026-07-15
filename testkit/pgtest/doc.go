// Package pgtest provisions a throwaway PostgreSQL for integration tests.
//
// Its API is only built under the "integration" build tag; see pgtest.go. This
// file keeps the package non-empty under the default build so that
// `go build ./...` compiles it without pulling in the testcontainers
// dependency tree.
package pgtest
