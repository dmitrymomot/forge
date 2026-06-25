module github.com/dmitrymomot/forge

go 1.26

tool (
	github.com/dkorunic/betteralign/cmd/betteralign
	github.com/golangci/golangci-lint/cmd/golangci-lint
	github.com/vektra/mockery/v3
	go.uber.org/nilaway/cmd/nilaway
	golang.org/x/tools/cmd/goimports
	golang.org/x/tools/go/analysis/passes/modernize/cmd/modernize
)
