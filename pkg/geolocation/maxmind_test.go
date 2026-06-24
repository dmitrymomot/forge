package geolocation_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/geolocation"
)

const testDBPath = "testdata/GeoIP2-City-Test.mmdb"

func TestNewMaxMindProvider(t *testing.T) {
	t.Parallel()

	t.Run("opens valid database", func(t *testing.T) {
		t.Parallel()
		p, err := geolocation.NewMaxMindProvider(testDBPath)
		require.NoError(t, err)
		require.NoError(t, p.Close())
	})

	t.Run("returns error for missing file", func(t *testing.T) {
		t.Parallel()
		_, err := geolocation.NewMaxMindProvider("nonexistent.mmdb")
		require.Error(t, err)
	})
}

func TestMaxMindProvider_Lookup(t *testing.T) {
	t.Parallel()

	p, err := geolocation.NewMaxMindProvider(testDBPath)
	require.NoError(t, err)
	t.Cleanup(func() { p.Close() })

	t.Run("resolves known public IP", func(t *testing.T) {
		t.Parallel()
		loc, err := p.Lookup(context.Background(), "81.2.69.142")
		require.NoError(t, err)
		require.NotNil(t, loc)
		require.Equal(t, "GB", loc.Country)
		require.Equal(t, "London", loc.City)
		require.Equal(t, "England", loc.Region)
		require.Equal(t, "Europe/London", loc.Timezone)
	})

	t.Run("populates region from subdivisions", func(t *testing.T) {
		t.Parallel()
		loc, err := p.Lookup(context.Background(), "2.125.160.216")
		require.NoError(t, err)
		require.NotNil(t, loc)
		require.Equal(t, "GB", loc.Country)
		require.Equal(t, "Boxford", loc.City)
		// The primary subdivision is the first entry, even when the
		// record carries multiple subdivision levels.
		require.Equal(t, "England", loc.Region)
	})

	t.Run("returns nil for public IP absent from database", func(t *testing.T) {
		t.Parallel()
		// 8.8.8.8 is a routable public address with no record in the
		// test database, exercising the record.HasData() == false branch.
		loc, err := p.Lookup(context.Background(), "8.8.8.8")
		require.NoError(t, err)
		require.Nil(t, loc)
	})

	t.Run("returns nil for loopback IPv4", func(t *testing.T) {
		t.Parallel()
		loc, err := p.Lookup(context.Background(), "127.0.0.1")
		require.NoError(t, err)
		require.Nil(t, loc)
	})

	t.Run("returns nil for loopback IPv6", func(t *testing.T) {
		t.Parallel()
		loc, err := p.Lookup(context.Background(), "::1")
		require.NoError(t, err)
		require.Nil(t, loc)
	})

	t.Run("returns nil for private RFC1918 10.x", func(t *testing.T) {
		t.Parallel()
		loc, err := p.Lookup(context.Background(), "10.0.0.1")
		require.NoError(t, err)
		require.Nil(t, loc)
	})

	t.Run("returns nil for private RFC1918 192.168.x", func(t *testing.T) {
		t.Parallel()
		loc, err := p.Lookup(context.Background(), "192.168.1.1")
		require.NoError(t, err)
		require.Nil(t, loc)
	})

	t.Run("returns nil for private RFC1918 172.16.x", func(t *testing.T) {
		t.Parallel()
		loc, err := p.Lookup(context.Background(), "172.16.0.1")
		require.NoError(t, err)
		require.Nil(t, loc)
	})

	t.Run("returns nil for link-local", func(t *testing.T) {
		t.Parallel()
		loc, err := p.Lookup(context.Background(), "169.254.1.1")
		require.NoError(t, err)
		require.Nil(t, loc)
	})

	t.Run("returns nil for unspecified 0.0.0.0", func(t *testing.T) {
		t.Parallel()
		loc, err := p.Lookup(context.Background(), "0.0.0.0")
		require.NoError(t, err)
		require.Nil(t, loc)
	})

	t.Run("returns nil for unspecified ::", func(t *testing.T) {
		t.Parallel()
		loc, err := p.Lookup(context.Background(), "::")
		require.NoError(t, err)
		require.Nil(t, loc)
	})

	t.Run("returns ErrInvalidIP for garbage input", func(t *testing.T) {
		t.Parallel()
		_, err := p.Lookup(context.Background(), "not-an-ip")
		require.ErrorIs(t, err, geolocation.ErrInvalidIP)
	})

	t.Run("returns ErrInvalidIP for empty string", func(t *testing.T) {
		t.Parallel()
		_, err := p.Lookup(context.Background(), "")
		require.ErrorIs(t, err, geolocation.ErrInvalidIP)
	})
}

func TestMaxMindProvider_Close(t *testing.T) {
	t.Parallel()

	t.Run("idempotent close", func(t *testing.T) {
		t.Parallel()
		p, err := geolocation.NewMaxMindProvider(testDBPath)
		require.NoError(t, err)

		require.NoError(t, p.Close())
		require.NoError(t, p.Close())
	})

	t.Run("lookup returns ErrClosed after close", func(t *testing.T) {
		t.Parallel()
		p, err := geolocation.NewMaxMindProvider(testDBPath)
		require.NoError(t, err)

		require.NoError(t, p.Close())

		_, err = p.Lookup(context.Background(), "81.2.69.142")
		require.ErrorIs(t, err, geolocation.ErrClosed)
	})
}

func TestMaxMindProvider_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	p, err := geolocation.NewMaxMindProvider(testDBPath)
	require.NoError(t, err)
	t.Cleanup(func() { p.Close() })

	var wg sync.WaitGroup
	ctx := context.Background()

	for range 50 {
		wg.Go(func() {
			loc, err := p.Lookup(ctx, "81.2.69.142")
			require.NoError(t, err)
			require.NotNil(t, loc)
			require.Equal(t, "GB", loc.Country)
		})
	}

	wg.Wait()
}
