// Package redistest provisions a throwaway Redis for integration tests.
//
// Its API is only built under the "integration" build tag; see redistest.go.
// This file keeps the package non-empty under the default build so that
// `go build ./...` compiles it without pulling in the testcontainers
// dependency tree.
package redistest
