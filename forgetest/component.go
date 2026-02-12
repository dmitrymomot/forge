package forgetest

import (
	"context"
	"io"
)

// MockComponent implements forge.Component for testing.
// It renders static HTML or returns a configured error.
type MockComponent struct {
	Err  error
	HTML string
}

// Render writes the HTML string to w or returns the configured error.
func (m *MockComponent) Render(_ context.Context, w io.Writer) error {
	if m.Err != nil {
		return m.Err
	}
	_, err := io.WriteString(w, m.HTML)
	return err
}
