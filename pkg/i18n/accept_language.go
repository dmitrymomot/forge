package i18n

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
)

// maxAcceptLanguageLength prevents DoS attacks through oversized Accept-Language headers.
const maxAcceptLanguageLength = 4096

// languageTag represents a parsed language tag with quality value.
type languageTag struct {
	tag     string
	quality float64
}

// ParseAcceptLanguage parses the Accept-Language header and returns the most
// applicable language from the available languages list.
// It supports quality values (q=0.9) and will match the highest quality
// available language. If no match is found, returns the first available language.
//
// Example header: "en-US,en;q=0.9,pl;q=0.8"
// Available: ["pl", "en", "de"]
// Returns: "en" (highest quality match)
func ParseAcceptLanguage(header string, available []string) string {
	if len(available) == 0 {
		return ""
	}

	if header == "" {
		return available[0]
	}

	tags := parseLanguageTags(header)

	var bestMatch string
	var bestQuality float64 = -1
	var bestIsExact bool

	for _, avail := range available {
		availNorm := normalizeLanguageTag(avail)

		for _, tag := range tags {
			if tag.tag == availNorm {
				if tag.quality > bestQuality || (tag.quality == bestQuality && !bestIsExact) {
					bestMatch = avail
					bestQuality = tag.quality
					bestIsExact = true
				}
				break
			}

			if matchesLanguage(tag.tag, avail) {
				if bestMatch == "" || (!bestIsExact && tag.quality > bestQuality) || (bestIsExact && tag.quality > bestQuality) {
					if !bestIsExact || tag.quality > bestQuality {
						bestMatch = avail
						bestQuality = tag.quality
						bestIsExact = false
					}
				}
				break
			}
		}
	}

	if bestMatch != "" {
		return bestMatch
	}

	return available[0]
}

// parseLanguageTags parses the Accept-Language header into language tags with quality values.
func parseLanguageTags(header string) []languageTag {
	if len(header) > maxAcceptLanguageLength {
		header = header[:maxAcceptLanguageLength]
	}

	var tags []languageTag

	for part := range strings.SplitSeq(header, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		quality := 1.0
		langPart, qPart, hasQuality := strings.Cut(part, ";")
		langPart = strings.TrimSpace(langPart)

		if hasQuality {
			qPart = strings.TrimSpace(qPart)

			if strings.HasPrefix(qPart, "q=") {
				if q, err := strconv.ParseFloat(qPart[2:], 64); err == nil && q >= 0 && q <= 1 {
					quality = q
				}
			}
		}

		if langPart != "" && langPart != "*" {
			tags = append(tags, languageTag{
				tag:     normalizeLanguageTag(langPart),
				quality: quality,
			})
		}
	}

	slices.SortFunc(tags, func(a, b languageTag) int {
		return cmp.Compare(b.quality, a.quality)
	})

	return tags
}

// normalizeLanguageTag normalizes a language tag to lowercase.
func normalizeLanguageTag(tag string) string {
	return strings.ToLower(strings.TrimSpace(tag))
}

// matchesLanguage checks if a requested language matches an available language.
// Supports partial matching: "en" matches "en-us" and vice versa.
func matchesLanguage(requested, available string) bool {
	requested = normalizeLanguageTag(requested)
	available = normalizeLanguageTag(available)

	if requested == available {
		return true
	}

	reqParts := strings.Split(requested, "-")
	availParts := strings.Split(available, "-")
	if len(reqParts) == 0 || len(availParts) == 0 {
		return false
	}

	return reqParts[0] == availParts[0]
}
