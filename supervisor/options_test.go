package supervisor

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()
	assert.Equal(t, 30*time.Second, cfg.ShutdownTimeout)
	assert.True(t, cfg.Recover)
	assert.NotNil(t, cfg.logger)
	assert.Empty(t, cfg.services)
}

func TestWithService_Appends(t *testing.T) {
	cfg := defaultConfig()
	a := fakeService{name: "a", run: func(ctx context.Context) error { return nil }}
	b := fakeService{name: "b", run: func(ctx context.Context) error { return nil }}

	WithService(a)(&cfg)
	WithService(b)(&cfg)

	require.Len(t, cfg.services, 2)
	assert.Equal(t, "a", cfg.services[0].Name())
	assert.Equal(t, "b", cfg.services[1].Name())
}

func TestWithServiceFunc_CreatesNamedService(t *testing.T) {
	cfg := defaultConfig()
	called := false

	WithServiceFunc("worker", func(ctx context.Context) error {
		called = true
		return nil
	})(&cfg)

	require.Len(t, cfg.services, 1)
	assert.Equal(t, "worker", cfg.services[0].Name())
	require.NoError(t, cfg.services[0].Run(context.Background()))
	assert.True(t, called)
}

func TestWithShutdownTimeout(t *testing.T) {
	cfg := defaultConfig()
	WithShutdownTimeout(5 * time.Second)(&cfg)
	assert.Equal(t, 5*time.Second, cfg.ShutdownTimeout)
}

func TestWithLogger_StoresValueIncludingNil(t *testing.T) {
	cfg := defaultConfig()
	l := slog.New(slog.NewTextHandler(io.Discard, nil))

	WithLogger(l)(&cfg)
	assert.Same(t, l, cfg.logger)

	WithLogger(nil)(&cfg)
	assert.Nil(t, cfg.logger, "nil must be stored verbatim; Run resolves it to a discard logger")
}

func TestWithRecover_Toggles(t *testing.T) {
	cfg := defaultConfig()
	WithRecover(false)(&cfg)
	assert.False(t, cfg.Recover)
	WithRecover(true)(&cfg)
	assert.True(t, cfg.Recover)
}

func TestWithConfig_SetsWholeBlock(t *testing.T) {
	cfg := defaultConfig()
	WithConfig(Config{ShutdownTimeout: 7 * time.Second, Recover: false})(&cfg)
	assert.Equal(t, 7*time.Second, cfg.ShutdownTimeout)
	assert.False(t, cfg.Recover)
}

func TestWithService_NilAppendsError(t *testing.T) {
	cfg := defaultConfig()
	WithService(nil)(&cfg)
	require.Len(t, cfg.errs, 1)
	assert.ErrorIs(t, cfg.errs[0], ErrInvalidConfig)
	assert.Empty(t, cfg.services)
}

func TestWithServiceFunc_NilFuncAppendsError(t *testing.T) {
	cfg := defaultConfig()
	WithServiceFunc("w", nil)(&cfg)
	require.Len(t, cfg.errs, 1)
	assert.ErrorIs(t, cfg.errs[0], ErrInvalidConfig)
	assert.Empty(t, cfg.services)
}
