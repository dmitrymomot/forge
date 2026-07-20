package workflow_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/workflow"
)

var _ workflow.Store = (*workflow.MemoryStore)(nil)

func TestMemoryStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("create requires id", func(t *testing.T) {
		t.Parallel()
		s := workflow.NewMemoryStore()
		assert.Error(t, s.Create(ctx, workflow.Run{}))
	})

	t.Run("create duplicate", func(t *testing.T) {
		t.Parallel()
		s := workflow.NewMemoryStore()
		require.NoError(t, s.Create(ctx, workflow.Run{ID: "r1", Version: 1}))
		assert.ErrorIs(t, s.Create(ctx, workflow.Run{ID: "r1", Version: 1}), workflow.ErrRunAlreadyExists)
	})

	t.Run("get missing", func(t *testing.T) {
		t.Parallel()
		s := workflow.NewMemoryStore()
		_, err := s.Get(ctx, "nope")
		assert.ErrorIs(t, err, workflow.ErrRunNotFound)
	})

	t.Run("update missing", func(t *testing.T) {
		t.Parallel()
		s := workflow.NewMemoryStore()
		assert.ErrorIs(t, s.Update(ctx, workflow.Run{ID: "nope", Version: 1}), workflow.ErrRunNotFound)
	})

	t.Run("update bumps version and stale write is rejected", func(t *testing.T) {
		t.Parallel()
		s := workflow.NewMemoryStore()
		require.NoError(t, s.Create(ctx, workflow.Run{ID: "r1", Status: workflow.StatusRunning, Version: 1}))

		run, err := s.Get(ctx, "r1")
		require.NoError(t, err)
		run.Step = 1
		require.NoError(t, s.Update(ctx, run))

		got, err := s.Get(ctx, "r1")
		require.NoError(t, err)
		assert.Equal(t, 2, got.Version)
		assert.Equal(t, 1, got.Step)

		// The first reader's copy is now stale: its write must not regress
		// the checkpoint.
		assert.ErrorIs(t, s.Update(ctx, run), workflow.ErrStaleRun)
	})

	t.Run("state is cloned on every boundary", func(t *testing.T) {
		t.Parallel()
		s := workflow.NewMemoryStore()
		state := json.RawMessage(`{"a":1}`)
		require.NoError(t, s.Create(ctx, workflow.Run{ID: "r1", State: state, Version: 1}))
		state[2] = 'X' // caller mutates the slice it handed over

		got, err := s.Get(ctx, "r1")
		require.NoError(t, err)
		assert.Equal(t, json.RawMessage(`{"a":1}`), got.State)

		got.State[2] = 'Y' // reader mutates the returned slice
		again, err := s.Get(ctx, "r1")
		require.NoError(t, err)
		assert.Equal(t, json.RawMessage(`{"a":1}`), again.State)
	})
}
