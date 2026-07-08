package useragent_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/web/useragent"
)

func TestParseEmptyString(t *testing.T) {
	res := useragent.Parse("")
	assert.Equal(t, "", res.Raw)
	assert.Equal(t, useragent.DeviceUnknown, res.Device.Type)
	assert.False(t, res.IsBot())
	assert.Empty(t, res.Browser.Name)
	assert.Empty(t, res.OS.Name)
}

func TestParseGarbage(t *testing.T) {
	for _, ua := range []string{
		"!!!???",
		"Mozilla/5.0",
		"\x00\x01\x02",
		"ÜmlautüberallÄÖ",
		strings.Repeat("x", 10*1024), // forces the heap fallback past the 512-byte stack buffer
	} {
		res := useragent.Parse(ua)
		assert.Equal(t, ua, res.Raw, "raw must round-trip")
		assert.Equal(t, useragent.DeviceUnknown, res.Device.Type)
		assert.False(t, res.IsBot())
	}
}
