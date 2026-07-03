package logger_test

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCoreHasNoSentryDependency guards the design rule: the core logger package must not
// import the Sentry SDK (it lives only under logger/sentry). It parses the package's own
// non-test source files (the test binary's cwd is the package dir) — no module resolution,
// stdlib only.
func TestCoreHasNoSentryDependency(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		require.NoError(t, err)
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			assert.False(t, strings.HasPrefix(path, "github.com/getsentry"),
				"core logger file %s must not import %s", name, path)
		}
	}
}
