package useragent_test

import (
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/pkg/useragent"

	"github.com/stretchr/testify/require"
)

func TestParseOS(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		ua       string
		expected string
	}{
		{
			name:     "Windows 10",
			ua:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
			expected: useragent.OSWindows,
		},
		{
			name:     "macOS",
			ua:       "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
			expected: useragent.OSMacOS,
		},
		{
			name:     "iOS",
			ua:       "Mozilla/5.0 (iPhone; CPU iPhone OS 14_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.0 Mobile/15E148 Safari/604.1",
			expected: useragent.OSiOS,
		},
		{
			name:     "Android",
			ua:       "Mozilla/5.0 (Linux; Android 11; Pixel 5) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Mobile Safari/537.36",
			expected: useragent.OSAndroid,
		},
		{
			name:     "Linux",
			ua:       "Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:89.0) Gecko/20100101 Firefox/89.0",
			expected: useragent.OSLinux,
		},
		{
			name:     "Empty UA",
			ua:       "",
			expected: useragent.OSUnknown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := useragent.ParseOS(strings.ToLower(tc.ua))
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestParseBrowser(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		ua       string
		expected useragent.Browser
	}{
		{
			name: "Chrome",
			ua:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
			expected: useragent.Browser{
				Name:    useragent.BrowserChrome,
				Version: "91.0.4472.124",
			},
		},
		{
			name: "Firefox",
			ua:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:89.0) Gecko/20100101 Firefox/89.0",
			expected: useragent.Browser{
				Name:    useragent.BrowserFirefox,
				Version: "89.0",
			},
		},
		{
			name: "Safari",
			ua:   "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.0.3 Safari/605.1.15",
			expected: useragent.Browser{
				Name:    useragent.BrowserSafari,
				Version: "14.0.3",
			},
		},
		{
			name: "Edge",
			ua:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36 Edg/91.0.864.59",
			expected: useragent.Browser{
				Name:    useragent.BrowserEdge,
				Version: "91.0.864.59",
			},
		},
		{
			name: "Empty UA",
			ua:   "",
			expected: useragent.Browser{
				Name:    useragent.BrowserUnknown,
				Version: "",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := useragent.ParseBrowser(strings.ToLower(tc.ua))
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestParseUserAgent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		ua          string
		expected    useragent.UserAgent
		expectedErr error
	}{
		{
			name: "Desktop Chrome on Windows",
			ua:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
			expected: useragent.New(
				"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
				useragent.DeviceTypeDesktop,
				"", // deviceModel
				useragent.OSWindows,
				useragent.BrowserChrome,
				"91.0.4472.124",
			),
			expectedErr: nil,
		},
		{
			name: "Mobile Safari on iPhone",
			ua:   "Mozilla/5.0 (iPhone; CPU iPhone OS 14_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.0 Mobile/15E148 Safari/604.1",
			expected: useragent.New(
				"Mozilla/5.0 (iPhone; CPU iPhone OS 14_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.0 Mobile/15E148 Safari/604.1",
				useragent.DeviceTypeMobile,
				useragent.MobileDeviceIPhone, // deviceModel
				useragent.OSiOS,
				useragent.BrowserSafari,
				"14.0",
			),
			expectedErr: nil,
		},
		{
			name: "Googlebot",
			ua:   "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
			expected: useragent.New(
				"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
				useragent.DeviceTypeBot,
				"", // deviceModel
				useragent.OSUnknown,
				useragent.BrowserUnknown,
				"",
			),
			expectedErr: nil,
		},
		{
			name:        "Empty UA",
			ua:          "",
			expected:    useragent.UserAgent{}, // Zero value for empty UA
			expectedErr: useragent.ErrEmptyUserAgent,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result, err := useragent.Parse(tc.ua)

			if tc.expectedErr != nil {
				require.ErrorIs(t, err, tc.expectedErr)
				// Every parse failure is also matchable via the parent sentinel.
				require.ErrorIs(t, err, useragent.ErrParsingFailed)
			} else {
				require.NoError(t, err)
			}

			// Use getter methods to compare values
			require.Equal(t, tc.expected.UserAgent(), result.UserAgent())
			require.Equal(t, tc.expected.DeviceType(), result.DeviceType())
			require.Equal(t, tc.expected.OS(), result.OS())
			require.Equal(t, tc.expected.BrowserName(), result.BrowserName())
			require.Equal(t, tc.expected.BrowserVer(), result.BrowserVer())
			require.Equal(t, tc.expected.IsBot(), result.IsBot())
			require.Equal(t, tc.expected.IsMobile(), result.IsMobile())
			require.Equal(t, tc.expected.IsDesktop(), result.IsDesktop())
			require.Equal(t, tc.expected.IsTablet(), result.IsTablet())
			require.Equal(t, tc.expected.IsUnknown(), result.IsUnknown())
		})
	}
}

// TestNewUserAgent tests the NewUserAgent constructor
func TestNewUserAgent(t *testing.T) {
	t.Parallel()
	ua := useragent.New(
		"test-ua",
		useragent.DeviceTypeMobile,
		useragent.MobileDeviceIPhone, // Added device model
		useragent.OSiOS,
		useragent.BrowserSafari,
		"15.0",
	)

	require.Equal(t, "test-ua", ua.UserAgent())
	require.Equal(t, useragent.DeviceTypeMobile, ua.DeviceType())
	require.Equal(t, useragent.MobileDeviceIPhone, ua.DeviceModel())
	require.Equal(t, useragent.OSiOS, ua.OS())
	require.Equal(t, useragent.BrowserSafari, ua.BrowserName())
	require.Equal(t, "15.0", ua.BrowserVer())
	require.True(t, ua.IsMobile())
	require.False(t, ua.IsDesktop())
	require.False(t, ua.IsTablet())
	require.False(t, ua.IsBot())
	require.False(t, ua.IsUnknown())
	require.False(t, ua.IsTV())
	require.False(t, ua.IsConsole())
}

// TestGetShortIdentifier tests the GetShortIdentifier method
func TestGetShortIdentifier(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		ua       useragent.UserAgent
		expected string
	}{
		{
			name: "Chrome on Windows",
			ua: useragent.New(
				"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
				useragent.DeviceTypeDesktop,
				"", // deviceModel
				useragent.OSWindows,
				useragent.BrowserChrome,
				"91.0.4472.124",
			),
			// Version is kept intact (4 dot-separated components), not garbled by
			// raw character truncation; the platform suffix is uniform.
			expected: "Chrome/91.0.4472.124 (Windows, desktop)",
		},
		{
			name: "Safari on iOS",
			ua: useragent.New(
				"Mozilla/5.0 (iPhone; CPU iPhone OS 14_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.0 Mobile/15E148 Safari/604.1",
				useragent.DeviceTypeMobile,
				useragent.MobileDeviceIPhone, // deviceModel
				useragent.OSiOS,
				useragent.BrowserSafari,
				"14.0",
			),
			expected: "Safari/14.0 (iOS, mobile)",
		},
		{
			name: "Chrome on Linux desktop",
			ua: useragent.New(
				"linux desktop chrome",
				useragent.DeviceTypeDesktop,
				"",
				useragent.OSLinux,
				useragent.BrowserChrome,
				"120.0",
			),
			// Same uniform format applies to every OS/device combination, not
			// just the two that were previously special-cased.
			expected: "Chrome/120.0 (Linux, desktop)",
		},
		{
			name: "Bot",
			ua: useragent.New(
				"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
				useragent.DeviceTypeBot,
				"", // deviceModel
				useragent.OSUnknown,
				useragent.BrowserUnknown,
				"",
			),
			expected: "Bot: Googlebot",
		},
		{
			name: "All Unknown Components - Empty Strings",
			ua: useragent.New(
				"",
				"",
				"", // deviceModel
				"",
				"",
				"",
			),
			expected: "Unknown device",
		},
		{
			name: "All Unknown Components - Unknown Constants",
			ua: useragent.New(
				"",
				useragent.DeviceTypeUnknown,
				"", // deviceModel
				useragent.OSUnknown,
				useragent.BrowserUnknown,
				"",
			),
			expected: "Unknown device",
		},
		{
			name: "Unknown Browser but Known OS and Device",
			ua: useragent.New(
				"Some obscure browser",
				useragent.DeviceTypeDesktop,
				"", // deviceModel
				useragent.OSWindows,
				useragent.BrowserUnknown,
				"",
			),
			expected: "Windows desktop",
		},
		{
			name: "Known Browser but Unknown OS and Device",
			ua: useragent.New(
				"Partial information",
				useragent.DeviceTypeUnknown,
				"", // deviceModel
				useragent.OSUnknown,
				useragent.BrowserChrome,
				"100.0",
			),
			expected: "Chrome/100.0 (Unknown OS)",
		},
		{
			name: "Browser with long version",
			ua: useragent.New(
				"Browser with long version string",
				useragent.DeviceTypeDesktop,
				"", // deviceModel
				useragent.OSWindows,
				useragent.BrowserFirefox,
				"100.0.12345.67890.beta",
			),
			// Truncation keeps the first 4 dot-separated components intact rather
			// than slicing mid-number.
			expected: "Firefox/100.0.12345.67890 (Windows, desktop)",
		},
		{
			name: "Browser with version ending with dot",
			ua: useragent.New(
				"Browser with version ending with dot",
				useragent.DeviceTypeDesktop,
				"", // deviceModel
				useragent.OSWindows,
				useragent.BrowserFirefox,
				"100.0.12345.",
			),
			// The empty trailing segment from the stray dot is dropped, not turned
			// into a fabricated "1".
			expected: "Firefox/100.0.12345 (Windows, desktop)",
		},
		{
			name: "Browser with garbled-prone version",
			ua: useragent.New(
				"Browser with single huge version segment",
				useragent.DeviceTypeDesktop,
				"", // deviceModel
				useragent.OSWindows,
				useragent.BrowserChrome,
				"123456789.0",
			),
			// Regression for the old defect where "123456789.0" became the
			// misleading "1234567891"; component-wise truncation keeps it correct.
			expected: "Chrome/123456789.0 (Windows, desktop)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := tc.ua.GetShortIdentifier()
			require.Equal(t, tc.expected, result)
		})
	}
}

// TestParseComplexBots tests complex bot detection scenarios using direct t.Run() style
func TestParseComplexBots(t *testing.T) {
	t.Parallel()

	t.Run("Googlebot with complex pattern", func(t *testing.T) {
		t.Parallel()
		ua := "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"
		result, err := useragent.Parse(ua)
		require.NoError(t, err)
		require.True(t, result.IsBot())
		require.Equal(t, "Bot: Googlebot", result.GetShortIdentifier())
	})

	t.Run("Bot with unusual casing and special characters", func(t *testing.T) {
		t.Parallel()
		ua := "MyCompany-WebCrawler_v2.1.3-bot (+https://example.com/crawler)"
		result, err := useragent.Parse(ua)
		require.NoError(t, err)
		require.True(t, result.IsBot())
		// Bot-name extraction is case-consistent: the mixed-case UA is lowercased
		// before pattern matching, then title-cased, so the dynamic identifier is
		// stable regardless of the original casing. The regex stops at the '.'
		// separator, so the captured token is the trailing "3-bot".
		require.Equal(t, "Bot: 3-Bot", result.GetShortIdentifier())
	})

	t.Run("Bot pattern at end of UA string", func(t *testing.T) {
		t.Parallel()
		ua := "CustomUserAgent/1.0 (compatible; Linux x86_64) SearchBot"
		result, err := useragent.Parse(ua)
		require.NoError(t, err)
		require.True(t, result.IsBot())
		require.Equal(t, "Bot: Searchbot", result.GetShortIdentifier())
	})
}

// TestParseMalformedUserAgents tests edge cases with malformed user agents
func TestParseMalformedUserAgents(t *testing.T) {
	t.Parallel()

	t.Run("Empty user agent string", func(t *testing.T) {
		t.Parallel()
		result, err := useragent.Parse("")
		require.Error(t, err)
		require.ErrorIs(t, err, useragent.ErrEmptyUserAgent)
		require.ErrorIs(t, err, useragent.ErrParsingFailed)
		require.Equal(t, "Unknown device", result.GetShortIdentifier())
	})

	t.Run("Random gibberish without any recognizable patterns", func(t *testing.T) {
		t.Parallel()
		ua := "!@#$%^&*()_+-={}[]|:;<>?,./"
		_, err := useragent.Parse(ua)
		require.Error(t, err)
		// The gibberish is detected as an unknown device, not malformed
		require.ErrorIs(t, err, useragent.ErrUnknownDevice)
		require.ErrorIs(t, err, useragent.ErrParsingFailed)
	})

	t.Run("Very long user agent exceeding reasonable limits", func(t *testing.T) {
		t.Parallel()
		// Create a very long string that might trigger length checks
		longUA := strings.Repeat("Mozilla/5.0 ", 100)
		result, err := useragent.Parse(longUA)
		// "Mozilla/5.0" alone has no device/OS/browser signal, so Parse reports an
		// unknown device rather than panicking on the oversized input.
		require.ErrorIs(t, err, useragent.ErrUnknownDevice)
		require.Equal(t, "Unknown device", result.GetShortIdentifier())
	})
}

// TestParseMultiStepVerification tests scenarios requiring multiple parsing steps
func TestParseMultiStepVerification(t *testing.T) {
	t.Parallel()

	t.Run("Mobile device with all components detected", func(t *testing.T) {
		t.Parallel()
		ua := "Mozilla/5.0 (iPhone; CPU iPhone OS 14_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.0 Mobile/15E148 Safari/604.1"
		result, err := useragent.Parse(ua)
		require.NoError(t, err)

		// Verify each component was parsed correctly
		require.Equal(t, useragent.DeviceTypeMobile, result.DeviceType())
		require.Equal(t, useragent.MobileDeviceIPhone, result.DeviceModel())
		require.Equal(t, useragent.OSiOS, result.OS())
		require.Equal(t, useragent.BrowserSafari, result.BrowserName())
		require.Equal(t, "14.0", result.BrowserVer())

		// Verify the full short identifier, not just substrings.
		require.Equal(t, "Safari/14.0 (iOS, mobile)", result.GetShortIdentifier())
	})

	t.Run("Desktop with partial information", func(t *testing.T) {
		t.Parallel()
		// UA with a browser-like token but no device/OS signal is treated as an
		// unknown device.
		ua := "CustomBrowser/1.0"
		result, err := useragent.Parse(ua)
		require.ErrorIs(t, err, useragent.ErrUnknownDevice)
		// On error Parse returns the zero value, which renders as "Unknown device".
		require.Equal(t, "Unknown device", result.GetShortIdentifier())
	})

	t.Run("Tablet device detection and formatting", func(t *testing.T) {
		t.Parallel()
		ua := "Mozilla/5.0 (iPad; CPU OS 13_5_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/13.1.1 Mobile/15E148 Safari/604.1"
		result, err := useragent.Parse(ua)
		require.NoError(t, err)

		require.Equal(t, useragent.DeviceTypeTablet, result.DeviceType())
		require.Equal(t, useragent.TabletDeviceIPad, result.DeviceModel())
		require.Equal(t, useragent.OSiOS, result.OS())
		require.Equal(t, "Safari/13.1.1 (iOS, tablet)", result.GetShortIdentifier())
	})
}
