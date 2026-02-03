package job

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// optionsTestTask implements the task interface.
type optionsTestTask struct{}

func (t *optionsTestTask) Name() string { return "options_test" }

func (t *optionsTestTask) Handle(ctx context.Context, p struct{}) error {
	return nil
}

func TestWithTask(t *testing.T) {
	t.Parallel()

	cfg := newConfig()

	task := &optionsTestTask{}
	opt := WithTask[struct{}, *optionsTestTask](task)
	opt(cfg)

	// Verify task was registered
	executor, ok := cfg.registry.get("options_test")
	assert.True(t, ok)
	assert.NotNil(t, executor)
}

// scheduledTestTask implements the scheduled task interface.
type scheduledTestTask struct {
	schedule string
}

func (t *scheduledTestTask) Name() string     { return "scheduled_test" }
func (t *scheduledTestTask) Schedule() string { return t.schedule }

func (t *scheduledTestTask) Handle(ctx context.Context) error {
	return nil
}

func TestWithScheduledTask(t *testing.T) {
	t.Parallel()

	cfg := newConfig()

	task := &scheduledTestTask{schedule: "0 * * * *"}
	opt := WithScheduledTask[*scheduledTestTask](task)
	opt(cfg)

	// Verify schedule was added
	require.Len(t, cfg.schedules, 1)
	assert.Equal(t, "scheduled_test", cfg.schedules[0].name)
	assert.Equal(t, "0 * * * *", cfg.schedules[0].schedule)
	assert.NotNil(t, cfg.schedules[0].handler)
}

func TestWithQueue(t *testing.T) {
	t.Parallel()

	cfg := newConfig()

	opt := WithQueue("email", 10)
	opt(cfg)

	assert.Equal(t, 10, cfg.queues["email"])
}

func TestWithQueue_ZeroWorkers(t *testing.T) {
	t.Parallel()

	cfg := newConfig()

	opt := WithQueue("email", 0)
	opt(cfg)

	_, ok := cfg.queues["email"]
	assert.False(t, ok, "queue with 0 workers should not be added")
}

func TestWithQueue_NegativeWorkers(t *testing.T) {
	t.Parallel()

	cfg := newConfig()

	opt := WithQueue("email", -5)
	opt(cfg)

	_, ok := cfg.queues["email"]
	assert.False(t, ok, "queue with negative workers should not be added")
}

func TestWithLogger(t *testing.T) {
	t.Parallel()

	cfg := newConfig()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	opt := WithLogger(logger)
	opt(cfg)

	assert.Same(t, logger, cfg.logger)
}

func TestWithLogger_Nil(t *testing.T) {
	t.Parallel()

	cfg := newConfig()
	cfg.logger = slog.Default()

	opt := WithLogger(nil)
	opt(cfg)

	// Should not change if nil
	assert.Same(t, slog.Default(), cfg.logger)
}

func TestWithMaxWorkers(t *testing.T) {
	t.Parallel()

	cfg := newConfig()

	opt := WithMaxWorkers(50)
	opt(cfg)

	assert.Equal(t, 50, cfg.maxWorkers)
}

func TestWithMaxWorkers_Zero(t *testing.T) {
	t.Parallel()

	cfg := newConfig()
	cfg.maxWorkers = 100

	opt := WithMaxWorkers(0)
	opt(cfg)

	// Should not change if 0
	assert.Equal(t, 100, cfg.maxWorkers)
}

func TestWithMaxWorkers_Negative(t *testing.T) {
	t.Parallel()

	cfg := newConfig()
	cfg.maxWorkers = 100

	opt := WithMaxWorkers(-10)
	opt(cfg)

	// Should not change if negative
	assert.Equal(t, 100, cfg.maxWorkers)
}

func TestNewConfig(t *testing.T) {
	t.Parallel()

	cfg := newConfig()

	assert.NotNil(t, cfg.registry)
	assert.NotNil(t, cfg.queues)
	assert.Empty(t, cfg.schedules)
	assert.Nil(t, cfg.logger)
	assert.Equal(t, 0, cfg.maxWorkers)
}
