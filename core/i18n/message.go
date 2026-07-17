package i18n

import (
	"fmt"
	"strconv"
	"strings"
)

// segment is one compiled piece of a message: a literal run of text (arg ==
// "") or a placeholder reference (arg holds the placeholder name; lit is
// unused).
type segment struct {
	lit string
	arg string
}

// compiledMsg is a message parsed exactly once, at Bundle construction, into
// a segment list — so rendering is a single pass over segs with no
// re-parsing and no fmt on the hot path.
type compiledMsg struct {
	segs []segment
	size int // rendered-size hint, used to presize the render buffer
}

// countArg is the reserved placeholder name TN injects the plural count
// under. An explicit "count" k/v pair in args always wins over the injected
// value.
const countArg = "count"

// compileMessage parses "{{name}}" placeholders out of s into a segment
// list. It is total over arbitrary input: a placeholder that is empty
// ("{{}}"), contains whitespace or nested braces ("{{a b}}"), or is never
// closed ("{{a") is left as literal text rather than rejected — an authoring
// mistake must degrade visibly in the rendered output, never panic and never
// silently vanish.
func compileMessage(s string) *compiledMsg {
	m := &compiledMsg{}
	for len(s) > 0 {
		open := strings.Index(s, "{{")
		if open < 0 {
			m.appendLit(s)
			break
		}
		body := s[open+2:]
		closeAt := strings.Index(body, "}}")
		if closeAt < 0 {
			m.appendLit(s)
			break
		}
		name := body[:closeAt]
		if name == "" || strings.ContainsAny(name, " \t\n{}") {
			// Not a valid placeholder: keep the "{{" as literal text and
			// resume scanning right after it, so a stray "{{" doesn't
			// swallow the rest of the message.
			m.appendLit(s[:open+2])
			s = s[open+2:]
			continue
		}
		if open > 0 {
			m.appendLit(s[:open])
		}
		m.segs = append(m.segs, segment{arg: name})
		m.size += len(name) + 8 // rough per-placeholder size hint
		s = body[closeAt+2:]
	}
	return m
}

// appendLit appends a literal segment, tracking the render-size hint.
func (m *compiledMsg) appendLit(s string) {
	m.segs = append(m.segs, segment{lit: s})
	m.size += len(s)
}

// appendArgValue appends v's rendered form to dst, avoiding fmt for the
// placeholder argument types real messages carry. fmt.Sprintf-style
// formatting is the fallback for anything else.
func appendArgValue(dst []byte, v any) []byte {
	switch x := v.(type) {
	case string:
		return append(dst, x...)
	case int:
		return strconv.AppendInt(dst, int64(x), 10)
	case int64:
		return strconv.AppendInt(dst, x, 10)
	case float64:
		return strconv.AppendFloat(dst, x, 'f', -1, 64)
	case fmt.Stringer:
		return append(dst, x.String()...)
	default:
		return fmt.Append(dst, v)
	}
}

// lookupArg scans args, a flat "name", value, "name", value, ... list, for
// name. A linear scan beats a map allocation at the handful of arguments a
// real message carries. A dangling final element (odd len(args)) and any
// non-string key are ignored rather than causing a panic.
func lookupArg(args []any, name string) (any, bool) {
	for i := 0; i+1 < len(args); i += 2 {
		if k, ok := args[i].(string); ok && k == name {
			return args[i+1], true
		}
	}
	return nil, false
}

// appendTo renders the compiled message into dst and returns the extended
// slice, preserving whatever dst already held. When hasCount is set,
// {{count}} renders count unless args carries an explicit "count" pair,
// which wins. Any other placeholder without a matching arg renders
// literally as "{{name}}", so a missing argument stays visible in the
// output instead of vanishing.
func (m *compiledMsg) appendTo(dst []byte, args []any, count int, hasCount bool) []byte {
	for _, seg := range m.segs {
		if seg.arg == "" {
			dst = append(dst, seg.lit...)
			continue
		}
		if v, ok := lookupArg(args, seg.arg); ok {
			dst = appendArgValue(dst, v)
			continue
		}
		if hasCount && seg.arg == countArg {
			dst = strconv.AppendInt(dst, int64(count), 10)
			continue
		}
		dst = append(dst, '{', '{')
		dst = append(dst, seg.arg...)
		dst = append(dst, '}', '}')
	}
	return dst
}

// render renders the compiled message to a new string. A literal-only
// message (the common case for an untranslated or argument-free string)
// returns the interned literal directly, with zero allocations.
func (m *compiledMsg) render(args []any, count int, hasCount bool) string {
	switch len(m.segs) {
	case 0:
		return ""
	case 1:
		if m.segs[0].arg == "" {
			return m.segs[0].lit
		}
	}
	return string(m.appendTo(make([]byte, 0, m.size), args, count, hasCount))
}
