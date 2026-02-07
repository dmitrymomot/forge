.PHONY: test bench lint fmt test-up test-down test-integration

test: ## Run all tests with race detection and coverage
	go clean -testcache && go test -race -cover ./...

bench: ## Run all benchmarks
	go test -bench=. -benchmem ./...

lint: ## Run code linters
	go vet ./...
	go build -o /dev/null ./...
	go tool golangci-lint run ./...
	go tool nilaway ./...
	go tool betteralign ./...
	go tool modernize $$(go list ./... | grep -v 'examples' | grep -v 'mocks')

fmt: ## Format code and imports
	go fmt ./...
	go tool goimports -w -local github.com/dmitrymomot/forge .
	go tool betteralign -apply -generated_files -exclude_dirs examples ./...

test-up: ## Start test infrastructure (docker containers)
	docker compose up -d --wait postgres mailpit rustfs redis
	docker compose up rustfs-bucket-init

test-down: ## Stop and remove test infrastructure
	docker compose down -v --remove-orphans

test-integration: test-up ## Run integration tests with docker infrastructure
	go test -tags=integration -race -cover ./... || ($(MAKE) test-down && exit 1)
	$(MAKE) test-down
