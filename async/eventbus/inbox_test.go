package eventbus_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/eventbus"
)

var _ eventbus.Inbox = (*eventbus.MemoryInbox)(nil)

func TestMemoryInbox(t *testing.T) {
	t.Parallel()

	t.Run("first claim then duplicate", func(t *testing.T) {
		t.Parallel()
		inbox := eventbus.NewMemoryInbox()
		seen, err := inbox.Seen(context.Background(), nil, "evt-1")
		require.NoError(t, err)
		assert.False(t, seen)

		seen, err = inbox.Seen(context.Background(), nil, "evt-1")
		require.NoError(t, err)
		assert.True(t, seen)
	})

	t.Run("ids are independent", func(t *testing.T) {
		t.Parallel()
		inbox := eventbus.NewMemoryInbox()
		_, err := inbox.Seen(context.Background(), nil, "evt-1")
		require.NoError(t, err)
		seen, err := inbox.Seen(context.Background(), nil, "evt-2")
		require.NoError(t, err)
		assert.False(t, seen)
	})

	t.Run("empty id errors", func(t *testing.T) {
		t.Parallel()
		inbox := eventbus.NewMemoryInbox()
		_, err := inbox.Seen(context.Background(), nil, "")
		assert.Error(t, err)
	})

	t.Run("exactly one concurrent claimer wins", func(t *testing.T) {
		t.Parallel()
		inbox := eventbus.NewMemoryInbox()
		const claimers = 32
		firsts := make(chan bool, claimers)
		var wg sync.WaitGroup
		for range claimers {
			wg.Go(func() {
				seen, err := inbox.Seen(context.Background(), nil, "evt-1")
				assert.NoError(t, err)
				firsts <- !seen
			})
		}
		wg.Wait()
		close(firsts)
		wins := 0
		for first := range firsts {
			if first {
				wins++
			}
		}
		assert.Equal(t, 1, wins)
	})
}
