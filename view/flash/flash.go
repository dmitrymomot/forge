package flash

import (
	"strings"
)

// Level is how loudly a message is meant to read.
type Level string

const (
	LevelInfo    Level = "info"
	LevelSuccess Level = "success"
	LevelWarning Level = "warning"
	LevelError   Level = "error"
)

// Valid reports whether l is one of the four defined levels. A decoded message
// carrying anything else is dropped, so a tampered payload cannot inject a level
// into a template.
func (l Level) Valid() bool {
	switch l {
	case LevelInfo, LevelSuccess, LevelWarning, LevelError:
		return true
	default:
		return false
	}
}

// Message is one flash: a level and the text a reader sees.
type Message struct {
	Level Level
	Text  string
}

// Info returns an informational message.
func Info(text string) Message { return Message{Level: LevelInfo, Text: text} }

// Success returns a message reporting a write that landed.
func Success(text string) Message { return Message{Level: LevelSuccess, Text: text} }

// Warning returns a message reporting a refusal the reader can act on.
func Warning(text string) Message { return Message{Level: LevelWarning, Text: text} }

// Error returns a message reporting a failure the reader cannot fix.
func Error(text string) Message { return Message{Level: LevelError, Text: text} }

// encode renders messages as "level:text" lines. A text containing a newline is
// impossible to round-trip in this framing, so newlines collapse to spaces on the
// way in rather than corrupting the following message on the way out.
func encode(msgs []Message) string {
	var b strings.Builder
	for i, m := range msgs {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(string(m.Level))
		b.WriteByte(':')
		b.WriteString(collapseNewlines(m.Text))
	}
	return b.String()
}

func collapseNewlines(s string) string {
	if !strings.ContainsAny(s, "\r\n") {
		return s
	}
	return strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(s)
}

// decode parses what encode produced, dropping any line with an unknown level or
// empty text. A malformed payload yields the messages it could read, never an error:
// a lost flash is not worth failing a page render over.
func decode(raw string) []Message {
	if raw == "" {
		return nil
	}
	lines := strings.Split(raw, "\n")
	msgs := make([]Message, 0, len(lines))
	for _, line := range lines {
		level, text, found := strings.Cut(line, ":")
		if !found || text == "" || !Level(level).Valid() {
			continue
		}
		msgs = append(msgs, Message{Level: Level(level), Text: text})
	}
	if len(msgs) == 0 {
		return nil
	}
	return msgs
}
