package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDefaultTextToOutput(t *testing.T) {
	var buf bytes.Buffer
	log, err := New(WithOutput(&buf))
	require.NoError(t, err)
	log.Info("hello", slog.String("k", "v"))
	out := buf.String()
	assert.Contains(t, out, "msg=hello")
	assert.Contains(t, out, "k=v")
}

func TestNewJSONFormatAndLevel(t *testing.T) {
	var buf bytes.Buffer
	log, err := New(WithOutput(&buf), WithFormat(FormatJSON), WithLevel(slog.LevelWarn))
	require.NoError(t, err)
	log.Info("dropped")
	log.Warn("kept")
	assert.NotContains(t, buf.String(), "dropped")

	var m map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &m))
	assert.Equal(t, "kept", m["msg"])
}

func TestNewWithFileWritesToFileNotStdout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "app.log")
	log, err := New(WithFile(path))
	require.NoError(t, err)
	log.Info("filemsg")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "filemsg")
}

func TestNewOutputOverridesFileXOR(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	var buf bytes.Buffer
	log, err := New(WithFile(path), WithOutput(&buf))
	require.NoError(t, err)
	log.Info("hi")
	assert.Contains(t, buf.String(), "hi")
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr), "file must not be created when WithOutput wins")
}

func TestNewWithHandlerFanOutAndExtraction(t *testing.T) {
	var primary, extra bytes.Buffer
	ex := func(ctx context.Context) (slog.Attr, bool) {
		if v, ok := ctx.Value(ctxKey{}).(string); ok {
			return slog.String("request_id", v), true
		}
		return slog.Attr{}, false
	}
	log, err := New(
		WithOutput(&primary),
		WithFormat(FormatJSON),
		WithHandler(slog.NewJSONHandler(&extra, nil)),
		WithContextExtractors(ex),
	)
	require.NoError(t, err)
	ctx := context.WithValue(context.Background(), ctxKey{}, "abc-123")
	log.InfoContext(ctx, "both")

	assert.Contains(t, primary.String(), "both")
	var m map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(extra.Bytes()), &m))
	assert.Equal(t, "both", m["msg"])
	assert.Equal(t, "abc-123", m["request_id"], "extra handler must receive extracted attrs")
}

func TestNewInvalidOptionAndConfig(t *testing.T) {
	_, err := New(WithFile(""))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfig)

	_, err = New(WithConfig(Config{Level: "loud", Format: "text"}))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestNewOpenFileError(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))
	_, err := New(WithFile(filepath.Join(blocker, "app.log")))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrOpenFile)
}

func TestNewNopeDiscards(t *testing.T) {
	log := NewNope()
	require.NotNil(t, log)
	log.Error("nothing") // must not panic
}
