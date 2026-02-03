// Package useragent provides User-Agent string parsing to extract browser, operating system,
// and device information for web analytics, content optimization, and request handling.
//
// The package identifies device types (mobile, desktop, tablet, bot, TV, console),
// specific device models, operating systems, and browser information with optimized
// bot detection for common crawlers and social media bots.
//
// # Basic Usage
//
// Parse a User-Agent string and access the extracted information:
//
//	import "github.com/dmitrymomot/forge/pkg/useragent"
//
//	ua, err := useragent.Parse(r.Header.Get("User-Agent"))
//	if err != nil {
//		log.Printf("Failed to parse User-Agent: %v", err)
//		return
//	}
//
//	fmt.Printf("Device: %s\n", ua.DeviceType())    // "mobile"
//	fmt.Printf("OS: %s\n", ua.OS())                // "ios"
//	fmt.Printf("Browser: %s\n", ua.BrowserName())  // "safari"
//	fmt.Printf("Version: %s\n", ua.BrowserVer())   // "14.2"
//	fmt.Printf("Model: %s\n", ua.DeviceModel())    // "iphone"
//
// # Device Type Detection
//
// Use device type constants and convenience methods:
//
//	switch ua.DeviceType() {
//	case useragent.DeviceTypeMobile:
//		// Serve mobile UI
//	case useragent.DeviceTypeDesktop:
//		// Serve desktop UI
//	case useragent.DeviceTypeTablet:
//		// Serve tablet UI
//	case useragent.DeviceTypeBot:
//		// Handle bot traffic
//	default:
//		// Handle unknown devices
//	}
//
//	// Or use convenience methods
//	if ua.IsMobile() {
//		fmt.Println("Mobile device detected")
//	}
//
//	if ua.IsBot() {
//		fmt.Printf("Bot: %s\n", ua.GetShortIdentifier())
//	}
//
// # Error Handling
//
// The package defines specific error types for different failure modes:
//
//	ua, err := useragent.Parse(userAgentString)
//	if err != nil {
//		switch {
//		case errors.Is(err, useragent.ErrEmptyUserAgent):
//			// Handle empty User-Agent header
//		case errors.Is(err, useragent.ErrUnknownDevice):
//			// Handle unknown device type
//		case errors.Is(err, useragent.ErrMalformedUserAgent):
//			// Handle malformed User-Agent string
//		default:
//			// Handle other parsing errors
//		}
//
//		// Create fallback UserAgent for continued processing
//		ua = useragent.New(userAgentString, "unknown", "", "unknown", "unknown", "")
//	}
//
// # Performance
//
// The package uses fast-path lookups for common bots and keyword-based matching
// for device classification. Parsing typically takes 10-100 microseconds per request.
// For high-traffic applications, consider caching parsed results for identical
// User-Agent strings.
package useragent
