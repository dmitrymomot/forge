package forgetest_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge"
	"github.com/dmitrymomot/forge/forgetest"
)

func TestMockComponent_Render(t *testing.T) {
	t.Parallel()

	t.Run("writes HTML to writer", func(t *testing.T) {
		t.Parallel()

		mock := &forgetest.MockComponent{
			HTML: "<h1>Hello, World!</h1>",
		}

		var buf bytes.Buffer
		err := mock.Render(context.Background(), &buf)

		require.NoError(t, err)
		require.Equal(t, "<h1>Hello, World!</h1>", buf.String())
	})

	t.Run("returns error when Err is set and does not write HTML", func(t *testing.T) {
		t.Parallel()

		expectedErr := errors.New("render failed")
		mock := &forgetest.MockComponent{
			HTML: "<h1>Should not be written</h1>",
			Err:  expectedErr,
		}

		var buf bytes.Buffer
		err := mock.Render(context.Background(), &buf)

		require.Error(t, err)
		require.Equal(t, expectedErr, err)
		require.Empty(t, buf.String(), "buffer should be empty when error is set")
	})

	t.Run("writes empty string when HTML is empty and Err is nil", func(t *testing.T) {
		t.Parallel()

		mock := &forgetest.MockComponent{
			HTML: "",
			Err:  nil,
		}

		var buf bytes.Buffer
		err := mock.Render(context.Background(), &buf)

		require.NoError(t, err)
		require.Empty(t, buf.String())
	})

	t.Run("handles large HTML content", func(t *testing.T) {
		t.Parallel()

		largeHTML := string(make([]byte, 1024*1024)) // 1MB of null bytes
		mock := &forgetest.MockComponent{
			HTML: largeHTML,
		}

		var buf bytes.Buffer
		err := mock.Render(context.Background(), &buf)

		require.NoError(t, err)
		require.Equal(t, largeHTML, buf.String())
	})
}

// Compile-time check that MockComponent satisfies forge.Component interface.
var _ forge.Component = (*forgetest.MockComponent)(nil)
