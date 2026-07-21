package fanout_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/realtime/fanout"
)

type tenantKey struct{}

func tenantCtx(tenant string) context.Context {
	return context.WithValue(context.Background(), tenantKey{}, tenant)
}

func tenantScope(ctx context.Context) (string, error) {
	tenant, _ := ctx.Value(tenantKey{}).(string)
	return tenant, nil
}

func TestScope(t *testing.T) {
	t.Parallel()

	t.Run("tenants are isolated on the same topic", func(t *testing.T) {
		t.Parallel()
		hub, err := fanout.New(fanout.WithScope(tenantScope))
		require.NoError(t, err)
		defer hub.Close()

		alpha, err := hub.Subscribe(tenantCtx("alpha"), []string{"orders"})
		require.NoError(t, err)
		defer alpha.Close()
		beta, err := hub.Subscribe(tenantCtx("beta"), []string{"orders"})
		require.NoError(t, err)
		defer beta.Close()

		require.NoError(t, hub.Publish(tenantCtx("alpha"), "orders", []byte("a")))
		msg := recv(t, alpha)
		assert.Equal(t, "orders", msg.Topic, "subscriber sees the unscoped topic")
		assert.Equal(t, []byte("a"), msg.Payload)
		select {
		case m := <-beta.C():
			t.Fatalf("cross-tenant delivery: %+v", m)
		case <-time.After(50 * time.Millisecond):
		}
	})

	t.Run("fail closed on missing scope", func(t *testing.T) {
		t.Parallel()
		hub, err := fanout.New(fanout.WithScope(tenantScope))
		require.NoError(t, err)
		defer hub.Close()

		ctx := context.Background()
		assert.ErrorIs(t, hub.Publish(ctx, "orders", []byte("x")), fanout.ErrScopeMissing)
		_, err = hub.Subscribe(ctx, []string{"orders"})
		assert.ErrorIs(t, err, fanout.ErrScopeMissing)
	})

	t.Run("fail closed on scope hook error", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("boom")
		hub, err := fanout.New(fanout.WithScope(func(context.Context) (string, error) { return "", boom }))
		require.NoError(t, err)
		defer hub.Close()

		err = hub.Publish(context.Background(), "orders", []byte("x"))
		assert.ErrorIs(t, err, fanout.ErrScopeMissing)
		assert.ErrorIs(t, err, boom)
	})

	t.Run("closed hub wins over scope errors", func(t *testing.T) {
		t.Parallel()
		hub, err := fanout.New(fanout.WithScope(tenantScope))
		require.NoError(t, err)
		hub.Close()

		// No scope in ctx: the hook would fail, but ErrClosed must win.
		err = hub.Publish(context.Background(), "orders", []byte("x"))
		assert.ErrorIs(t, err, fanout.ErrClosed)
		assert.NotErrorIs(t, err, fanout.ErrScopeMissing)
	})

	t.Run("reserved bytes in scope rejected", func(t *testing.T) {
		t.Parallel()
		hub, err := fanout.New(fanout.WithScope(func(context.Context) (string, error) { return "bad\x1ftenant", nil }))
		require.NoError(t, err)
		defer hub.Close()

		assert.ErrorIs(t, hub.Publish(context.Background(), "orders", nil), fanout.ErrInvalidScope)
	})

	t.Run("scoped replay resume", func(t *testing.T) {
		t.Parallel()
		hub, err := fanout.New(fanout.WithScope(tenantScope), fanout.WithReplay(4))
		require.NoError(t, err)
		defer hub.Close()

		require.NoError(t, hub.Publish(tenantCtx("alpha"), "orders", []byte("a1")))
		require.NoError(t, hub.Publish(tenantCtx("beta"), "orders", []byte("b1")))

		sub, err := hub.Subscribe(tenantCtx("alpha"), []string{"orders"}, fanout.WithResumeAfter(0))
		require.NoError(t, err)
		defer sub.Close()
		msg := recv(t, sub)
		assert.Equal(t, "orders", msg.Topic)
		assert.Equal(t, []byte("a1"), msg.Payload)
		select {
		case m := <-sub.C():
			t.Fatalf("cross-tenant replay: %+v", m)
		case <-time.After(50 * time.Millisecond):
		}
	})
}
