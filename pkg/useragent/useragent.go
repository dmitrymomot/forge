package useragent

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// UserAgent contains the parsed information from a user agent string
type UserAgent struct {
	userAgent string

	deviceType  string
	deviceModel string

	os          string
	browserName string
	browserVer  string
}

func (ua UserAgent) String() string { return ua.userAgent }

func (ua UserAgent) UserAgent() string { return ua.userAgent }

func (ua UserAgent) DeviceType() string { return ua.deviceType }

func (ua UserAgent) DeviceModel() string { return ua.deviceModel }

func (ua UserAgent) OS() string { return ua.os }

func (ua UserAgent) BrowserName() string { return ua.browserName }

func (ua UserAgent) BrowserVer() string { return ua.browserVer }

func (ua UserAgent) BrowserInfo() Browser {
	return Browser{Name: ua.browserName, Version: ua.browserVer}
}

func (ua UserAgent) IsBot() bool { return ua.deviceType == DeviceTypeBot }

func (ua UserAgent) IsMobile() bool { return ua.deviceType == DeviceTypeMobile }

func (ua UserAgent) IsDesktop() bool { return ua.deviceType == DeviceTypeDesktop }

func (ua UserAgent) IsTablet() bool { return ua.deviceType == DeviceTypeTablet }

func (ua UserAgent) IsTV() bool { return ua.deviceType == DeviceTypeTV }

func (ua UserAgent) IsConsole() bool { return ua.deviceType == DeviceTypeConsole }

func (ua UserAgent) IsUnknown() bool {
	return ua.deviceType == DeviceTypeUnknown || ua.deviceType == ""
}

// Fast-path lookups for common bots to avoid regex overhead
var botNameMap = map[string]string{
	"googlebot":           "Googlebot",
	"bingbot":             "Bingbot",
	"yandexbot":           "Yandexbot",
	"baidubot":            "Baidubot",
	"twitterbot":          "Twitterbot",
	"facebookbot":         "Facebookbot",
	"facebookexternalhit": "Facebook",
	"linkedinbot":         "Linkedinbot",
	"slackbot":            "Slackbot",
	"telegrambot":         "Telegrambot",
	"adsbot":              "AdsBot",
}

// Fallback patterns for dynamic bot name extraction when fast-path fails.
// Patterns operate on lowercased input, so they need no case-insensitivity flag.
var botNamePatterns = []*regexp.Regexp{
	regexp.MustCompile(`([a-z0-9\-_]+bot)`),
	regexp.MustCompile(`(google-structured-data)`),
	regexp.MustCompile(`([a-z0-9\-_]+spider)`),
	regexp.MustCompile(`([a-z0-9\-_]+crawler)`),
}

// titleCaseASCII title-cases an ASCII bot identifier: the first letter of each
// word (a run delimited by any non-alphanumeric byte) is uppercased. It is a
// stateless, allocation-light, and goroutine-safe replacement for the
// golang.org/x/text title transformer, which is unsafe for concurrent use and
// would otherwise force a per-call allocation. Bot tokens captured by the
// fallback regexes are ASCII, so byte-wise casing is correct here.
func titleCaseASCII(s string) string {
	b := []byte(s)
	atWordStart := true
	for i := range b {
		c := b[i]
		isAlphaNum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if atWordStart && c >= 'a' && c <= 'z' {
			b[i] = c - ('a' - 'A')
		}
		atWordStart = !isAlphaNum
	}
	return string(b)
}

// extractBotName extracts bot names using fast-path lookups before falling back to regex.
// This two-tier approach optimizes for the 90% case where bots are well-known.
// The input is expected to be lowercased, consistent with the rest of Parse.
func extractBotName(lowerUA string) string {
	defaultName := "Unknown Bot"

	// Googlebot represents ~40% of bot traffic, so check it first
	if strings.Contains(lowerUA, "googlebot") {
		return "Googlebot"
	}
	for keyword, name := range botNameMap {
		if strings.Contains(lowerUA, keyword) {
			return name
		}
	}

	// Regex fallback for less common bots and dynamic extraction
	for _, pattern := range botNamePatterns {
		matches := pattern.FindStringSubmatch(lowerUA)
		if len(matches) > 1 {
			// Captured group contains the bot identifier
			return titleCaseASCII(matches[1])
		} else if len(matches) == 1 {
			// No capture group, use full match
			return titleCaseASCII(matches[0])
		}
	}

	return defaultName
}

func formatOSName(osName string) string {
	if osName == "" || osName == OSUnknown {
		return "Unknown OS"
	}

	// iOS requires special casing due to brand guidelines
	if strings.ToLower(osName) == "ios" {
		return "iOS"
	}

	if len(osName) > 0 {
		return strings.ToUpper(osName[:1]) + osName[1:]
	}

	return osName
}

func formatBrowserName(browserName string) string {
	if browserName == "" || browserName == BrowserUnknown {
		return "Unknown"
	}

	if len(browserName) > 0 {
		return strings.ToUpper(browserName[:1]) + browserName[1:]
	}

	return browserName
}

// maxVersionComponents bounds how many dot-separated version segments are kept
// so excessively precise version strings don't overflow display contexts.
const maxVersionComponents = 4

// formatBrowserVersion trims overly precise version strings that can appear in
// some UAs. It truncates on dot-separated component boundaries rather than raw
// characters, so segments are never merged into a misleading number (e.g.
// "91.0.4472.124" stays "91.0.4472.124", not "91.0.44721"; and a trailing dot
// in "100.0.12345." is dropped rather than producing a fabricated digit).
func formatBrowserVersion(version string) string {
	if version == "" {
		return "?"
	}

	components := strings.Split(version, ".")

	// Drop empty trailing/leading segments produced by stray dots.
	cleaned := components[:0]
	for _, c := range components {
		if c != "" {
			cleaned = append(cleaned, c)
		}
	}
	if len(cleaned) == 0 {
		return version
	}

	if len(cleaned) > maxVersionComponents {
		cleaned = cleaned[:maxVersionComponents]
	}

	return strings.Join(cleaned, ".")
}

func formatDeviceType(deviceType string) string {
	if deviceType == "" || deviceType == DeviceTypeUnknown {
		return "unknown"
	}
	return deviceType
}

// GetShortIdentifier creates human-readable session identifiers for logging and analytics.
// Handles various edge cases to provide consistent, useful output across all UA types.
func (ua UserAgent) GetShortIdentifier() string {
	if ua.IsBot() {
		return ua.formatBotIdentifier()
	}
	if ua.isAllUnknown() {
		return "Unknown device"
	}
	return ua.formatStandardIdentifier()
}

// formatBotIdentifier formats bot user agents for display.
func (ua UserAgent) formatBotIdentifier() string {
	return fmt.Sprintf("Bot: %s", extractBotName(strings.ToLower(ua.userAgent)))
}

// isAllUnknown checks if all user agent components are unknown.
func (ua UserAgent) isAllUnknown() bool {
	return (ua.BrowserName() == "" || ua.BrowserName() == BrowserUnknown) &&
		(ua.OS() == "" || ua.OS() == OSUnknown) &&
		(ua.DeviceType() == "" || ua.DeviceType() == DeviceTypeUnknown)
}

// formatStandardIdentifier formats standard user agents with browser, OS, and device information.
// The output uses a single, uniform shape regardless of platform:
//
//	Browser/Version (OS, device)
//
// with the comma-separated platform suffix collapsing to just the OS when the
// device type is unknown.
func (ua UserAgent) formatStandardIdentifier() string {
	osKnown := ua.OS() != "" && ua.OS() != OSUnknown
	deviceKnown := ua.DeviceType() != "" && ua.DeviceType() != DeviceTypeUnknown

	// When only browser detection fails, show OS and device.
	if (ua.BrowserName() == "" || ua.BrowserName() == BrowserUnknown) && osKnown && deviceKnown {
		return fmt.Sprintf("%s %s", formatOSName(ua.OS()), formatDeviceType(ua.DeviceType()))
	}

	browserName := formatBrowserName(ua.BrowserName())
	browserVersion := formatBrowserVersion(ua.BrowserVer())
	osName := formatOSName(ua.OS())

	// Collapse to just the OS when the device type is unknown to avoid a
	// redundant "unknown" in the suffix.
	if !deviceKnown {
		return fmt.Sprintf("%s/%s (%s)", browserName, browserVersion, osName)
	}

	return fmt.Sprintf("%s/%s (%s, %s)", browserName, browserVersion, osName, formatDeviceType(ua.DeviceType()))
}

// Parse analyzes a user agent string and extracts device, OS, and browser information.
// Returns structured data with appropriate errors for various failure modes.
func Parse(ua string) (UserAgent, error) {
	var zero UserAgent
	if ua == "" {
		return zero, errors.Join(ErrParsingFailed, ErrEmptyUserAgent)
	}

	// Normalize case for consistent string matching across parsers
	lowerUA := strings.ToLower(ua)

	deviceType := ParseDeviceType(lowerUA)
	if deviceType == DeviceTypeUnknown && !strings.Contains(lowerUA, "bot") {
		// Unknown devices are only errors for non-bots since bot patterns can be unusual
		return zero, errors.Join(ErrParsingFailed, ErrUnknownDevice)
	}

	deviceModel := GetDeviceModel(lowerUA, deviceType)

	os := ParseOS(lowerUA)

	browser := ParseBrowser(lowerUA)

	// Detect malformed UAs: non-empty but all parsers failed
	if os == OSUnknown && browser.Name == BrowserUnknown && ua != "" && deviceType == DeviceTypeUnknown {
		return zero, errors.Join(ErrParsingFailed, ErrMalformedUserAgent)
	}

	return New(ua, deviceType, deviceModel, os, browser.Name, browser.Version), nil
}

// New creates a UserAgent struct with the provided parameters
func New(ua, deviceType, deviceModel, os, browserName, browserVer string) UserAgent {
	return UserAgent{
		userAgent:   ua,
		deviceType:  deviceType,
		deviceModel: deviceModel,
		os:          os,
		browserName: browserName,
		browserVer:  browserVer,
	}
}
