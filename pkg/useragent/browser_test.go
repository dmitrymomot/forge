package useragent_test

import (
	"testing"

	"github.com/dmitrymomot/forge/pkg/useragent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBrowserInfo tests the BrowserInfo method
func TestBrowserInfo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		ua       useragent.UserAgent
		expected useragent.Browser
	}{
		{
			name: "Chrome browser",
			ua: useragent.New(
				"test-user-agent-string",
				useragent.DeviceTypeDesktop,
				"",
				useragent.OSWindows,
				useragent.BrowserChrome,
				"91.0.4472.124",
			),
			expected: useragent.Browser{
				Name:    useragent.BrowserChrome,
				Version: "91.0.4472.124",
			},
		},
		{
			name: "Firefox browser",
			ua: useragent.New(
				"test-user-agent-string",
				useragent.DeviceTypeDesktop,
				"",
				useragent.OSWindows,
				useragent.BrowserFirefox,
				"89.0",
			),
			expected: useragent.Browser{
				Name:    useragent.BrowserFirefox,
				Version: "89.0",
			},
		},
		{
			name: "Unknown browser",
			ua: useragent.New(
				"test-user-agent-string",
				useragent.DeviceTypeDesktop,
				"",
				useragent.OSWindows,
				useragent.BrowserUnknown,
				"",
			),
			expected: useragent.Browser{
				Name:    useragent.BrowserUnknown,
				Version: "",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			browserInfo := tc.ua.BrowserInfo()
			assert.Equal(t, tc.expected.Name, browserInfo.Name)
			assert.Equal(t, tc.expected.Version, browserInfo.Version)
		})
	}
}

// TestParseBrowserQQFalsePositives verifies that the QQ detection only fires on
// the explicit "qqbrowser" token, not on UAs that merely contain "qq" plus
// "browser" somewhere (the old over-broad pattern misclassified these).
func TestParseBrowserQQFalsePositives(t *testing.T) {
	t.Parallel()

	t.Run("Chrome UA containing qq substring is not QQ", func(t *testing.T) {
		t.Parallel()
		// "qq" appears inside an unrelated token, alongside the word "browser".
		ua := "mozilla/5.0 (linux; android 11; somebrand_qq build/abc) applewebkit/537.36 (khtml, like gecko) chrome/91.0.4472.120 mobile safari/537.36 browser"
		result := useragent.ParseBrowser(ua)
		require.Equal(t, useragent.BrowserChrome, result.Name)
		require.NotEqual(t, useragent.BrowserQQ, result.Name)
	})

	t.Run("Genuine QQ browser is still detected", func(t *testing.T) {
		t.Parallel()
		ua := "mozilla/5.0 (linux; android 10) applewebkit/537.36 (khtml, like gecko) version/4.0 chrome/76.0.3809.89 mobile safari/537.36 mqqbrowser/11.4"
		result := useragent.ParseBrowser(ua)
		require.Equal(t, useragent.BrowserQQ, result.Name)
		require.Equal(t, "11.4", result.Version)
	})
}

// Additional tests for Browser parsing are already in useragent_test.go (TestParseBrowser)
