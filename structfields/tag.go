package structfields

import "strings"

// Tag is a parsed struct tag for a single tagKey. Name is the first
// comma-separated segment (empty when the tag is absent); Options holds the
// remaining segments; Raw is the unparsed tag value for tagKey.
type Tag struct {
	Name    string   // first comma-segment of the tag ("" when absent)
	Options []string // remaining comma-separated segments
	Raw     string   // raw tag value for tagKey
}

// Ignored reports whether the tag explicitly excludes the field (Name == "-").
func (t Tag) Ignored() bool {
	return t.Name == "-"
}

// HasOption reports whether opt appears among the tag's Options.
func (t Tag) HasOption(opt string) bool {
	for _, o := range t.Options {
		if o == opt {
			return true
		}
	}
	return false
}

// parseTag splits a raw struct-tag value into Name + Options. An empty raw
// value yields a zero Tag (Name == "", nil Options).
func parseTag(raw string) Tag {
	if raw == "" {
		return Tag{Raw: raw}
	}
	parts := strings.Split(raw, ",")
	return Tag{
		Name:    parts[0],
		Options: parts[1:],
		Raw:     raw,
	}
}
