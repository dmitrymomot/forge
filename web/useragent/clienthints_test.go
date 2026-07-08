package useragent_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/web/useragent"
)

const frozenChromeWin = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36"

func headers(ua string, kv map[string]string) http.Header {
	h := http.Header{}
	h.Set("User-Agent", ua)
	for k, v := range kv {
		h.Set(k, v)
	}
	return h
}

func TestClientHintsBrave(t *testing.T) {
	got := useragent.ParseHeaders(headers(frozenChromeWin, map[string]string{
		"Sec-CH-UA": `"Brave";v="138", "Chromium";v="138", "Not?A_Brand";v="99"`,
	}))
	assert.Equal(t, "Brave", got.Browser.Name)
	assert.Equal(t, 138, got.Browser.Version.Major)
}

func TestClientHintsEdgeAndArc(t *testing.T) {
	got := useragent.ParseHeaders(headers(frozenChromeWin, map[string]string{
		"Sec-CH-UA": `"Microsoft Edge";v="138", "Chromium";v="138", "Not/A)Brand";v="24"`,
	}))
	assert.Equal(t, "Edge", got.Browser.Name)

	got = useragent.ParseHeaders(headers(frozenChromeWin, map[string]string{
		"Sec-CH-UA": `"Arc";v="1", "Chromium";v="138", "Not A;Brand";v="99"`,
	}))
	assert.Equal(t, "Arc", got.Browser.Name)
}

func TestClientHintsFullVersionUnfreezes(t *testing.T) {
	got := useragent.ParseHeaders(headers(frozenChromeWin, map[string]string{
		"Sec-CH-UA-Full-Version-List": `"Chromium";v="138.0.7204.97", "Google Chrome";v="138.0.7204.97", "Not/A)Brand";v="99.0.0.0"`,
	}))
	assert.Equal(t, "Chrome", got.Browser.Name)
	assert.Equal(t, useragent.Version{Major: 138, Minor: 0, Patch: 7204, Full: "138.0.7204.97"}, got.Browser.Version)
}

func TestClientHintsWindows11(t *testing.T) {
	got := useragent.ParseHeaders(headers(frozenChromeWin, map[string]string{
		"Sec-CH-UA-Platform":         `"Windows"`,
		"Sec-CH-UA-Platform-Version": `"15.0.0"`,
	}))
	assert.Equal(t, "Windows", got.OS.Name)
	assert.Equal(t, "11", got.OS.Version.Full)

	got = useragent.ParseHeaders(headers(frozenChromeWin, map[string]string{
		"Sec-CH-UA-Platform":         `"Windows"`,
		"Sec-CH-UA-Platform-Version": `"10.0.0"`,
	}))
	assert.Equal(t, "10", got.OS.Version.Full)
}

func TestClientHintsMacRealVersion(t *testing.T) {
	frozenMac := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36"
	got := useragent.ParseHeaders(headers(frozenMac, map[string]string{
		"Sec-CH-UA-Platform":         `"macOS"`,
		"Sec-CH-UA-Platform-Version": `"14.5.0"`,
	}))
	assert.Equal(t, "macOS", got.OS.Name)
	assert.Equal(t, 14, got.OS.Version.Major)
}

func TestClientHintsModelAndMobile(t *testing.T) {
	reduced := "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Mobile Safari/537.36"
	got := useragent.ParseHeaders(headers(reduced, map[string]string{
		"Sec-CH-UA-Model": `"Pixel 8 Pro"`,
	}))
	assert.Equal(t, "Pixel 8 Pro", got.Device.Model)
	assert.Equal(t, "Google", got.Device.Brand)

	got = useragent.ParseHeaders(headers(frozenChromeWin, map[string]string{
		"Sec-CH-UA-Mobile": "?1",
	}))
	assert.Equal(t, useragent.DeviceMobile, got.Device.Type)
}

func TestClientHintsMissingHeadersLeaveParseUntouched(t *testing.T) {
	got := useragent.ParseHeaders(headers(frozenChromeWin, nil))
	assert.Equal(t, useragent.Parse(frozenChromeWin), got)
}

func TestClientHintsMalformedIgnored(t *testing.T) {
	got := useragent.ParseHeaders(headers(frozenChromeWin, map[string]string{
		"Sec-CH-UA":                  `;;;"""not even close`,
		"Sec-CH-UA-Platform":         `"`,
		"Sec-CH-UA-Platform-Version": `"garbage"`,
		"Sec-CH-UA-Model":            `""`,
	}))
	assert.Equal(t, "Chrome", got.Browser.Name)
	assert.Equal(t, "Windows", got.OS.Name)
}

func TestClientHintsSkippedForBots(t *testing.T) {
	got := useragent.ParseHeaders(headers("Googlebot/2.1 (+http://www.google.com/bot.html)", map[string]string{
		"Sec-CH-UA": `"Brave";v="138"`,
	}))
	assert.True(t, got.IsBot())
	assert.Empty(t, got.Browser.Name)
}

func TestParseRequestDelegates(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("User-Agent", frozenChromeWin)
	r.Header.Set("Sec-CH-UA", `"Brave";v="138", "Chromium";v="138", "Not?A_Brand";v="99"`)
	got := useragent.ParseRequest(r)
	assert.Equal(t, "Brave", got.Browser.Name)
}
