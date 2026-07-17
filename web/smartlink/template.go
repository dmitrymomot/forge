package smartlink

import (
	"fmt"
	"net/url"
	"strings"
)

// macroKind identifies which visit fact a template macro renders.
type macroKind uint8

const (
	macroNone macroKind = iota // literal segment, no macro
	macroCountry
	macroDevice
	macroLocale
	macroParam
)

// segment is one compiled piece of a URL template: a literal followed by an
// optional macro. Escaping is positional: macros before the first '?' render
// path-escaped, macros after it query-escaped, so values can never alter the
// URL structure.
type segment struct {
	literal    string
	param      string // param name for macroParam
	macro      macroKind
	pathEscape bool
}

// template is a compiled URL template. A nil segs means the URL is a plain
// literal and renders with zero work.
type template struct {
	raw  string
	segs []segment
}

// parseTemplate compiles a target URL template: splits out {macro}
// placeholders, resolves the vocabulary, assigns positional escaping, and
// validates the macro-elided template parses as a URL.
func parseTemplate(raw, where string) (template, error) {
	if !strings.ContainsRune(raw, '{') && !strings.ContainsRune(raw, '}') {
		if _, err := url.Parse(raw); err != nil {
			return template{}, fmt.Errorf("%w: %s: %v", ErrInvalidTemplate, where, err)
		}
		return template{raw: raw}, nil
	}

	var segs []segment
	var elided strings.Builder
	rest := raw
	inQuery := false
	for {
		open := strings.IndexByte(rest, '{')
		if open < 0 {
			break
		}
		close := strings.IndexByte(rest[open:], '}')
		if close < 0 {
			return template{}, fmt.Errorf("%w: %s: unclosed '{' in %q", ErrInvalidTemplate, where, raw)
		}
		close += open
		literal := rest[:open]
		if strings.ContainsRune(literal, '}') {
			return template{}, fmt.Errorf("%w: %s: unmatched '}' in %q", ErrInvalidTemplate, where, raw)
		}
		name := rest[open+1 : close]
		kind, param, err := resolveMacro(name, where)
		if err != nil {
			return template{}, err
		}
		if strings.ContainsRune(literal, '?') {
			inQuery = true
		}
		segs = append(segs, segment{literal: literal, macro: kind, param: param, pathEscape: !inQuery})
		elided.WriteString(literal)
		rest = rest[close+1:]
	}
	if strings.ContainsRune(rest, '}') {
		return template{}, fmt.Errorf("%w: %s: unmatched '}' in %q", ErrInvalidTemplate, where, raw)
	}
	if rest != "" {
		segs = append(segs, segment{literal: rest})
		elided.WriteString(rest)
	}
	if _, err := url.Parse(elided.String()); err != nil {
		return template{}, fmt.Errorf("%w: %s: %v", ErrInvalidTemplate, where, err)
	}
	return template{raw: raw, segs: segs}, nil
}

// resolveMacro maps a macro name to its kind; unknown names are compile
// errors, never empty substitutions at decide time.
func resolveMacro(name, where string) (macroKind, string, error) {
	switch name {
	case "country":
		return macroCountry, "", nil
	case "device":
		return macroDevice, "", nil
	case "locale":
		return macroLocale, "", nil
	}
	if param, ok := strings.CutPrefix(name, "param."); ok && param != "" {
		return macroParam, param, nil
	}
	return macroNone, "", fmt.Errorf("%w: %s: {%s}", ErrUnknownMacro, where, name)
}

// render substitutes visit values into the template. A known macro whose
// visit value is empty renders as an empty string — the visit context is
// genuinely sparse.
func (t template) render(v *Visit) string {
	if t.segs == nil {
		return t.raw
	}
	var b strings.Builder
	b.Grow(len(t.raw) + 16)
	for _, s := range t.segs {
		b.WriteString(s.literal)
		var val string
		switch s.macro {
		case macroNone:
			continue
		case macroCountry:
			val = v.Country
		case macroDevice:
			val = v.Device
		case macroLocale:
			val = v.Locale
		case macroParam:
			val = v.Params[s.param]
		}
		if val == "" {
			continue
		}
		if s.pathEscape {
			b.WriteString(url.PathEscape(val))
		} else {
			b.WriteString(url.QueryEscape(val))
		}
	}
	return b.String()
}
