package geolocation_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/geolocation"
)

func TestLocation_String(t *testing.T) {
	t.Parallel()

	t.Run("all fields populated", func(t *testing.T) {
		t.Parallel()
		loc := &geolocation.Location{
			City: "London", Region: "England", Country: "GB",
		}
		require.Equal(t, "London, England, GB", loc.String())
	})

	t.Run("no city", func(t *testing.T) {
		t.Parallel()
		loc := &geolocation.Location{
			Region: "California", Country: "US",
		}
		require.Equal(t, "California, US", loc.String())
	})

	t.Run("country only", func(t *testing.T) {
		t.Parallel()
		loc := &geolocation.Location{Country: "DE"}
		require.Equal(t, "DE", loc.String())
	})

	t.Run("all empty", func(t *testing.T) {
		t.Parallel()
		loc := &geolocation.Location{}
		require.Equal(t, "", loc.String())
	})

	t.Run("city and country no region", func(t *testing.T) {
		t.Parallel()
		loc := &geolocation.Location{
			City: "Berlin", Country: "DE",
		}
		require.Equal(t, "Berlin, DE", loc.String())
	})

	t.Run("region only", func(t *testing.T) {
		t.Parallel()
		loc := &geolocation.Location{Region: "Bavaria"}
		require.Equal(t, "Bavaria", loc.String())
	})
}
