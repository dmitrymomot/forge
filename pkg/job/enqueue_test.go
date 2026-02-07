package job

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestWithQueue_Enqueue(t *testing.T) {
	t.Parallel()

	cfg := &enqueueConfig{}

	opt := WithQueue("email")
	opt(cfg)

	assert.Equal(t, "email", cfg.queue)
}

func TestWithQueue_Enqueue_Empty(t *testing.T) {
	t.Parallel()

	cfg := &enqueueConfig{queue: "existing"}

	opt := WithQueue("")
	opt(cfg)

	// Should not change if empty
	assert.Equal(t, "existing", cfg.queue)
}

func TestWithScheduledAt(t *testing.T) {
	t.Parallel()

	cfg := &enqueueConfig{}

	future := time.Now().Add(24 * time.Hour)
	opt := WithScheduledAt(future)
	opt(cfg)

	assert.NotNil(t, cfg.scheduledAt)
	assert.Equal(t, future, *cfg.scheduledAt)
}

func TestWithScheduledIn(t *testing.T) {
	t.Parallel()

	cfg := &enqueueConfig{}

	before := time.Now()
	opt := WithScheduledIn(time.Hour)
	opt(cfg)
	after := time.Now()

	assert.NotNil(t, cfg.scheduledAt)
	assert.True(t, cfg.scheduledAt.After(before.Add(time.Hour-time.Second)))
	assert.True(t, cfg.scheduledAt.Before(after.Add(time.Hour+time.Second)))
}

func TestWithMaxAttempts(t *testing.T) {
	t.Parallel()

	cfg := &enqueueConfig{}

	opt := WithMaxAttempts(5)
	opt(cfg)

	assert.Equal(t, 5, cfg.maxAttempts)
}

func TestWithMaxAttempts_Zero(t *testing.T) {
	t.Parallel()

	cfg := &enqueueConfig{maxAttempts: 10}

	opt := WithMaxAttempts(0)
	opt(cfg)

	// Should not change if 0
	assert.Equal(t, 10, cfg.maxAttempts)
}

func TestWithMaxAttempts_Negative(t *testing.T) {
	t.Parallel()

	cfg := &enqueueConfig{maxAttempts: 10}

	opt := WithMaxAttempts(-1)
	opt(cfg)

	// Should not change if negative
	assert.Equal(t, 10, cfg.maxAttempts)
}

func TestWithUniqueFor(t *testing.T) {
	t.Parallel()

	cfg := &enqueueConfig{}

	opt := WithUniqueFor(time.Hour)
	opt(cfg)

	assert.Equal(t, time.Hour, cfg.uniqueFor)
}

func TestWithUniqueKey(t *testing.T) {
	t.Parallel()

	cfg := &enqueueConfig{}

	opt := WithUniqueKey("user:123")
	opt(cfg)

	assert.Equal(t, "user:123", cfg.uniqueKey)
}

func TestWithPriority(t *testing.T) {
	t.Parallel()

	cfg := &enqueueConfig{}

	opt := WithPriority(5)
	opt(cfg)

	assert.Equal(t, 5, cfg.priority)
}

func TestWithTags(t *testing.T) {
	t.Parallel()

	cfg := &enqueueConfig{}

	opt := WithTags("email", "marketing")
	opt(cfg)

	assert.Equal(t, []string{"email", "marketing"}, cfg.tags)
}

func TestWithTags_Append(t *testing.T) {
	t.Parallel()

	cfg := &enqueueConfig{tags: []string{"existing"}}

	opt := WithTags("new")
	opt(cfg)

	assert.Equal(t, []string{"existing", "new"}, cfg.tags)
}

func TestWithTags_Empty(t *testing.T) {
	t.Parallel()

	cfg := &enqueueConfig{}

	opt := WithTags()
	opt(cfg)

	assert.Empty(t, cfg.tags)
}

func TestCombinedEnqueueOptions(t *testing.T) {
	t.Parallel()

	cfg := &enqueueConfig{}

	opts := []EnqueueOption{
		WithQueue("email"),
		WithMaxAttempts(3),
		WithPriority(2),
		WithTags("urgent"),
		WithUniqueFor(time.Hour),
		WithUniqueKey("email:user:123"),
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
