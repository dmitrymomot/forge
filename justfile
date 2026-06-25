# Forge task runner

# List available recipes
help:
    @just --list

# Run all tests with race detection and coverage
test path='./...':
    go clean -testcache && go test -race -cover {{ path }}

# Run all benchmarks
bench path='./...':
    go clean -testcache && go test -bench=. -benchmem {{ path }}

# Run code linters
lint:
    go vet ./...
    go build -o /dev/null ./...
    go tool golangci-lint run ./...
    go tool nilaway ./...
    go tool betteralign ./...
    go tool modernize $(go list ./... | grep -v 'examples' | grep -v 'mocks')

# Format code and imports
fmt path='./...':
    go fmt {{ path }}
    go tool goimports -w -local github.com/dmitrymomot/forge .
    go tool betteralign -apply -generated_files -exclude_dirs examples {{ path }}

# Run fmt, lint, and test
check: fmt lint test
