package job

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestInQueue(t *testing.T) {
	t.Parallel()

	cfg := &enqueueConfig{}

	opt := InQueue("email")
	opt(cfg)

	assert.Equal(t, "email", cfg.queue)
}

func TestInQueue_Empty(t *testing.T) {
	t.Parallel()

	cfg := &enqueueConfig{queue: "existing"}

	opt := InQueue("")
	opt(cfg)

	// Should not change if empty
	assert.Equal(t, "existing", cfg.queue)
}

func TestScheduledAt(t *testing.T) {
	t.Parallel()

	cfg := &enqueueConfig{}

	future := time.Now().Add(24 * time.Hour)
	opt := ScheduledAt(future)
	opt(cfg)

	assert.NotNil(t, cfg.scheduledAt)
	assert.Equal(t, future, *cfg.scheduledAt)
}

func TestScheduledIn(t *testing.T) {
	t.Parallel()

	cfg := &enqueueConfig{}

	before := time.Now()
	opt := ScheduledIn(time.Hour)
	opt(cfg)
	after := time.Now()

	assert.NotNil(t, cfg.scheduledAt)
	assert.True(t, cfg.scheduledAt.After(before.Add(time.Hour-time.Second)))
	assert.True(t, cfg.scheduledAt.Before(after.Add(time.Hour+time.Second)))
}

func TestMaxAttempts(t *testing.T) {
	t.Parallel()

	cfg := &enqueueConfig{}

	opt := MaxAttempts(5)
	opt(cfg)

	assert.Equal(t, 5, cfg.maxAttempts)
}

func TestMaxAttempts_Zero(t *testing.T) {
	t.Parallel()

	cfg := &enqueueConfig{maxAttempts: 10}

	opt := MaxAttempts(0)
	opt(cfg)

	// Should not change if 0
	assert.Equal(t, 10, cfg.maxAttempts)
}

func TestMaxAttempts_Negative(t *testing.T) {
	t.Parallel()

	cfg := &enqueueConfig{maxAttempts: 10}

	opt := MaxAttempts(-1)
	opt(cfg)

	// Should not change if negative
	assert.Equal(t, 10, cfg.maxAttempts)
}

func TestUniqueFor(t *testing.T) {
	t.Parallel()

	cfg := &enqueueConfig{}

	opt := UniqueFor(time.Hour)
	opt(cfg)

	assert.Equal(t, time.Hour, cfg.uniqueFor)
}

func TestUniqueKey(t *testing.T) {
	t.Parallel()

	cfg := &enqueueConfig{}

	opt := UniqueKey("user:123")
	opt(cfg)

	assert.Equal(t, "user:123", cfg.uniqueKey)
}

func TestPriority(t *testing.T) {
	t.Parallel()

	cfg := &enqueueConfig{}

	opt := Priority(5)
	opt(cfg)

	assert.Equal(t, 5, cfg.priority)
}

func TestTags(t *testing.T) {
	t.Parallel()

	cfg := &enqueueConfig{}

	opt := Tags("email", "marketing")
	opt(cfg)

	assert.Equal(t, []string{"email", "marketing"}, cfg.tags)
}

func TestTags_Append(t *testing.T) {
	t.Parallel()

	cfg := &enqueueConfig{tags: []string{"existing"}}

	opt := Tags("new")
	opt(cfg)

	assert.Equal(t, []string{"existing", "new"}, cfg.tags)
}

func TestTags_Empty(t *testing.T) {
	t.Parallel()

	cfg := &enqueueConfig{}

	opt := Tags()
	opt(cfg)

	assert.Empty(t, cfg.tags)
}

func TestCombinedOptions(t *testing.T) {
	t.Parallel()

	cfg := &enqueueConfig{}

	opts := []EnqueueOption{
		InQueue("email"),
		MaxAttempts(3),
		Priority(2),
		Tags("urgent"),
		UniqueFor(time.Hour),
		UniqueKey("email:user:123"),
	}

	for _, opt := range opts {
		opt(cfg)
	}

	assert.Equal(t, "email", cfg.queue)
	assert.Equal(t, 3, cfg.maxAttempts)
	assert.Equal(t, 2, cfg.priority)
	assert.Equal(t, []string{"urgent"}, cfg.tags)
	assert.Equal(t, time.Hour, cfg.uniqueFor)
	assert.Equal(t, "email:user:123", cfg.uniqueKey)
}
