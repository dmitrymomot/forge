package smartlink

import "time"

// Visit is the caller-built context of one click. The caller assembles it from
// whatever request facts it has (web/clientip + web/geoip for Country,
// web/useragent for Device, parsed query params) — this package never touches
// net/http.
type Visit struct {
	// At overrides the decision time for TimeWindow matchers — useful when
	// replaying click logs. Zero means "now" per the link's clock.
	At time.Time
	// Params carries inbound query params / sub-IDs. ParamEquals matches
	// against it, {param.NAME} macros render from it, and the link's
	// ParamPolicy merges it into the final URL.
	Params map[string]string
	// Country is the visitor's ISO 3166-1 alpha-2 country code, any case.
	Country string
	// Device is the device class in web/useragent DeviceType vocabulary
	// ("desktop", "mobile", "tablet", ...); pass string(ua.Device.Type).
	Device string
	// Locale is a BCP-47-style tag ("en" or "en-US"), any case.
	Locale string
	// StickyKey is the bucketing identity (click ID, visitor ID) that keeps
	// Percent matchers and weighted splits deterministic per visitor. With an
	// empty key, Percent never matches and splits pick their first target.
	StickyKey string
}
