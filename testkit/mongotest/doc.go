// Package mongotest provisions a throwaway MongoDB for integration tests.
//
// Its API is only built under the "integration" build tag; see mongotest.go.
// This file keeps the package non-empty under the default build so that
// `go build ./...` compiles it without pulling in the testcontainers
// dependency tree.
package mongotest
