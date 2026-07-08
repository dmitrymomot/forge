package useragent

// detectOS is ordered: consoles and phone OSes hide inside generic tokens
// (Xbox UAs contain "Windows NT", HarmonyOS UAs contain "Android"), so the
// specific checks run first.
func detectOS(in input, raw string) OS {
	switch {
	case in.contains("playstation"):
		return OS{Name: "PlayStation", Version: in.versionAfter(raw, "playstation ")}
	case in.contains("xbox"):
		return OS{Name: "Xbox"}
	case in.contains("nintendo switch"):
		return OS{Name: "Nintendo Switch"}
	case in.contains("nintendo"):
		return OS{Name: "Nintendo"}
	case in.contains("windows phone"):
		v := in.versionAfter(raw, "windows phone os ")
		if v.IsZero() {
			v = in.versionAfter(raw, "windows phone ")
		}
		return OS{Name: "Windows Phone", Version: v}
	case in.contains("windows nt "):
		return windowsOS(in, raw)
	case in.contains("iphone os "):
		return OS{Name: "iOS", Version: in.versionAfter(raw, "iphone os ")}
	case in.contains("ipad"):
		return OS{Name: "iPadOS", Version: in.versionAfter(raw, "cpu os ")}
	case in.contains("harmonyos"):
		return OS{Name: "HarmonyOS"}
	case in.contains("kaios/"):
		// before android: KaiOS UAs carry an "Android" token too
		return OS{Name: "KaiOS", Version: in.versionAfter(raw, "kaios/")}
	case in.contains("android"):
		return OS{Name: "Android", Version: in.versionAfter(raw, "android ")}
	case in.contains("tizen"):
		return OS{Name: "Tizen", Version: in.versionAfter(raw, "tizen ")}
	case in.contains("web0s"), in.contains("webos"):
		return OS{Name: "webOS"}
	case in.contains("cros "):
		return OS{Name: "ChromeOS"}
	case in.contains("mac os x"):
		if in.contains("mobile/") {
			// Desktop-mode iPad / iPad in-app view sends a Mac UA plus a
			// Mobile/ build token. Best-effort: true desktop-mode Safari on
			// iPad is indistinguishable from a Mac and stays macOS.
			return OS{Name: "iPadOS"}
		}
		return OS{Name: "macOS", Version: in.versionAfter(raw, "mac os x ")}
	case in.contains("freebsd"):
		return OS{Name: "FreeBSD"}
	case in.contains("openbsd"):
		return OS{Name: "OpenBSD"}
	case in.contains("netbsd"):
		return OS{Name: "NetBSD"}
	case in.contains("linux"), in.contains("x11;"):
		return OS{Name: "Linux"}
	}
	return OS{}
}

// windowsOS maps NT kernel versions to marketing versions. NT 10.0 is
// reported as "10" — Windows 11 also sends NT 10.0 and is only
// distinguishable via Client Hints (Task 7).
func windowsOS(in input, raw string) OS {
	nt := in.versionAfter(raw, "windows nt ")
	switch {
	case nt.Major == 10:
		return OS{Name: "Windows", Version: Version{Major: 10, Full: "10"}}
	case nt.Major == 6 && nt.Minor == 3:
		return OS{Name: "Windows", Version: Version{Major: 8, Minor: 1, Full: "8.1"}}
	case nt.Major == 6 && nt.Minor == 2:
		return OS{Name: "Windows", Version: Version{Major: 8, Full: "8"}}
	case nt.Major == 6 && nt.Minor == 1:
		return OS{Name: "Windows", Version: Version{Major: 7, Full: "7"}}
	case nt.Major == 6 && nt.Minor == 0:
		return OS{Name: "Windows", Version: Version{Full: "Vista"}}
	case nt.Major == 5:
		return OS{Name: "Windows", Version: Version{Full: "XP"}}
	}
	return OS{Name: "Windows", Version: nt}
}
