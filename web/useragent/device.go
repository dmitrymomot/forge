package useragent

import "strings"

var desktopOS = map[string]bool{
	"Windows": true, "macOS": true, "Linux": true, "ChromeOS": true,
	"FreeBSD": true, "OpenBSD": true, "NetBSD": true,
}

// detectDevice is ordered most-specific-first: consoles and TVs before the
// mobile/tablet split, desktop as the OS-derived fallback.
func detectDevice(in input, osName string) Device {
	model, brand := deviceModel(in)
	switch {
	case in.contains("playstation 5"):
		return Device{Type: DeviceConsole, Brand: "Sony", Model: "PlayStation 5"}
	case in.contains("playstation 4"):
		return Device{Type: DeviceConsole, Brand: "Sony", Model: "PlayStation 4"}
	case in.contains("playstation"):
		return Device{Type: DeviceConsole, Brand: "Sony", Model: "PlayStation"}
	case in.contains("xbox"):
		return Device{Type: DeviceConsole, Brand: "Microsoft", Model: "Xbox"}
	case in.contains("nintendo switch"):
		return Device{Type: DeviceConsole, Brand: "Nintendo", Model: "Switch"}
	case in.contains("nintendo"):
		return Device{Type: DeviceConsole, Brand: "Nintendo"}
	case in.contains("appletv"), in.contains("apple tv"):
		return Device{Type: DeviceTV, Brand: "Apple", Model: "Apple TV"}
	case in.contains("crkey"):
		return Device{Type: DeviceTV, Brand: "Google", Model: "Chromecast"}
	case in.contains("roku"):
		return Device{Type: DeviceTV, Brand: "Roku"}
	case in.contains("bravia"):
		return Device{Type: DeviceTV, Brand: "Sony", Model: model}
	case hasPrefixFold(model, "aft"):
		return Device{Type: DeviceTV, Brand: "Amazon", Model: model}
	case in.contains("android tv"), in.contains("googletv"), in.contains("smart-tv"),
		in.contains("smarttv"), in.contains("hbbtv"), in.contains("netcast"),
		osName == "webOS", osName == "Tizen" && in.contains("tv"):
		return Device{Type: DeviceTV, Brand: brand, Model: model}
	case in.contains("watchos"), in.contains("apple watch"):
		return Device{Type: DeviceWearable, Brand: "Apple", Model: "Apple Watch"}
	case in.contains("watch"):
		return Device{Type: DeviceWearable, Brand: brand, Model: model}
	case in.contains("kobo"):
		return Device{Type: DeviceEReader, Brand: "Kobo"}
	case in.contains("kindle"):
		return Device{Type: DeviceEReader, Brand: "Amazon", Model: "Kindle"}
	case in.contains("ipad"):
		return Device{Type: DeviceTablet, Brand: "Apple", Model: "iPad"}
	case in.contains("iphone"):
		return Device{Type: DeviceMobile, Brand: "Apple", Model: "iPhone"}
	case in.contains("ipod"):
		return Device{Type: DeviceMobile, Brand: "Apple", Model: "iPod"}
	case osName == "iPadOS":
		return Device{Type: DeviceTablet, Brand: "Apple", Model: "iPad"}
	case osName == "Android":
		if hasPrefixFold(model, "kf") {
			return Device{Type: DeviceTablet, Brand: "Amazon", Model: model}
		}
		if in.contains("mobile") || in.contains("opera mini") {
			return Device{Type: DeviceMobile, Brand: brand, Model: model}
		}
		return Device{Type: DeviceTablet, Brand: brand, Model: model}
	case in.contains("opera mini"), in.contains("windows phone"), in.contains("mobile"):
		return Device{Type: DeviceMobile, Brand: brand, Model: model}
	case in.contains("tablet"):
		return Device{Type: DeviceTablet, Brand: brand, Model: model}
	case desktopOS[osName]:
		return Device{Type: DeviceDesktop}
	}
	return Device{Type: DeviceUnknown}
}

// deviceModel extracts the Android-style device segment with original
// casing: "(Linux; Android 14; SM-S918B Build/UP1A)" → "SM-S918B".
// The frozen reduced-UA placeholder "K" maps to no model.
func deviceModel(in input) (model, brand string) {
	var seg string
	if end := in.index(" build/"); end >= 0 {
		start := end
		for start > 0 && in.lower[start-1] != ';' && in.lower[start-1] != '(' {
			start--
		}
		seg = strings.TrimSpace(in.raw[start:end])
	} else if i := in.index("android "); i >= 0 {
		// reduced UA without Build/: model is the next comment segment,
		// e.g. "(Linux; Android 13; SM-X710)"
		j := i
		for j < len(in.lower) && in.lower[j] != ';' && in.lower[j] != ')' {
			j++
		}
		if j < len(in.lower) && in.lower[j] == ';' {
			k := j + 1
			for k < len(in.lower) && in.lower[k] != ';' && in.lower[k] != ')' {
				k++
			}
			seg = strings.TrimSpace(in.raw[j+1 : k])
		}
	}
	if seg == "" || seg == "K" || seg == "k" || isLocale(seg) {
		return "", brandToken(in)
	}
	if b := brandFromModel(seg); b != "" {
		return seg, b
	}
	return seg, brandToken(in)
}

// isLocale filters legacy "(...; en-us; Model)" segments that would
// otherwise be mistaken for a model in the reduced-UA fallback.
func isLocale(s string) bool {
	if len(s) != 2 && len(s) != 5 {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if (c < 'a' || c > 'z') && c != '-' {
			return false
		}
	}
	return len(s) == 2 || s[2] == '-'
}

var modelBrandPrefixes = []struct{ prefix, brand string }{
	{"sm-", "Samsung"}, {"gt-", "Samsung"}, {"galaxy", "Samsung"},
	{"pixel", "Google"}, {"nexus", "Google"},
	{"redmi", "Xiaomi"}, {"poco", "Xiaomi"}, {"mi ", "Xiaomi"},
	{"kf", "Amazon"}, {"aft", "Amazon"},
	{"moto", "Motorola"}, {"xperia", "Sony"},
	{"nokia", "Nokia"}, {"lumia", "Nokia"},
	{"lm-", "LG"}, {"lg-", "LG"},
	{"lenovo", "Lenovo"}, {"tb-", "Lenovo"}, {"tcl", "TCL"},
	{"cph", "OPPO"}, {"rmx", "realme"}, {"oneplus", "OnePlus"},
	{"vivo", "vivo"}, {"huawei", "Huawei"}, {"honor", "Honor"},
}

// brandFromModel infers the vendor from well-known model prefixes.
// Shared with the Client Hints path (Sec-CH-UA-Model).
func brandFromModel(model string) string {
	for _, p := range modelBrandPrefixes {
		if hasPrefixFold(model, p.prefix) {
			return p.brand
		}
	}
	return ""
}

var uaBrandTokens = []struct{ token, brand string }{
	{"huawei", "Huawei"}, {"honor", "Honor"}, {"xiaomi", "Xiaomi"},
	{"oneplus", "OnePlus"}, {"oppo", "OPPO"}, {"vivo", "vivo"},
	{"samsung", "Samsung"}, {"nokia", "Nokia"},
}

func brandToken(in input) string {
	for _, t := range uaBrandTokens {
		if in.contains(t.token) {
			return t.brand
		}
	}
	return ""
}

// hasPrefixFold is an ASCII case-insensitive strings.HasPrefix.
func hasPrefixFold(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	for i := range len(prefix) {
		a, b := s[i], prefix[i]
		if a >= 'A' && a <= 'Z' {
			a |= 0x20
		}
		if b >= 'A' && b <= 'Z' {
			b |= 0x20
		}
		if a != b {
			return false
		}
	}
	return true
}
