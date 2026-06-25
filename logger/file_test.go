package logger

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenFileCreatesNestedDirsAndAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "deep", "app.log")
	f, err := openFile(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	_, err = f.WriteString("line1\n")
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "line1\n", string(data))
}

func TestOpenFileErrorWhenDirUncreatable(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644)) // a FILE where a dir is needed
	_, err := openFile(filepath.Join(blocker, "sub", "app.log"))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrOpenFile)
}

func TestOpenFileBareFilenameSkipsMkdir(t *testing.T) {
	// A bare filename has filepath.Dir == "." — openFile must skip MkdirAll and create the
	// file in the working directory. t.Chdir keeps the file inside a temp dir, exercising
	// the guard branch without an mkdir.
	t.Chdir(t.TempDir())
	f, err := openFile("bare.log")
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	_, err = f.WriteString("x\n")
	require.NoError(t, err)

	data, err := os.ReadFile("bare.log")
	require.NoError(t, err)
	assert.Equal(t, "x\n", string(data))
}
