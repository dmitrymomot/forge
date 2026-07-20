package workflow_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/workflow"
)

func noop(context.Context, *struct{}) error { return nil }

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("empty workflow name", func(t *testing.T) {
		t.Parallel()
		assert.Panics(t, func() { workflow.New[struct{}]("") })
	})

	t.Run("no steps", func(t *testing.T) {
		t.Parallel()
		assert.Panics(t, func() { workflow.New[struct{}]("wf") })
	})

	t.Run("empty step name", func(t *testing.T) {
		t.Parallel()
		assert.Panics(t, func() {
			workflow.New("wf", workflow.Step[struct{}]{Run: noop})
		})
	})

	t.Run("nil run", func(t *testing.T) {
		t.Parallel()
		assert.Panics(t, func() {
			workflow.New("wf", workflow.Step[struct{}]{Name: "a"})
		})
	})

	t.Run("negative max attempts", func(t *testing.T) {
		t.Parallel()
		assert.Panics(t, func() {
			workflow.New("wf", workflow.Step[struct{}]{Name: "a", Run: noop, MaxAttempts: -1})
		})
	})

	t.Run("duplicate step name", func(t *testing.T) {
		t.Parallel()
		assert.Panics(t, func() {
			workflow.New("wf",
				workflow.Step[struct{}]{Name: "a", Run: noop},
				workflow.Step[struct{}]{Name: "a", Run: noop},
			)
		})
	})

	t.Run("valid", func(t *testing.T) {
		t.Parallel()
		wf := workflow.New("wf", workflow.Step[struct{}]{Name: "a", Run: noop})
		assert.Equal(t, "wf", wf.Name())
	})
}

func TestFail(t *testing.T) {
	t.Parallel()

	assert.NoError(t, workflow.Fail(nil))

	base := errors.New("boom")
	err := workflow.Fail(base)
	require.Error(t, err)
	assert.True(t, workflow.IsFail(err))
	assert.ErrorIs(t, err, base)
	assert.False(t, workflow.IsFail(base))
}
