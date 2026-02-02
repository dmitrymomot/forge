package views

import (
	"fmt"
	"time"

	"github.com/dmitrymomot/forge/examples/full-app/repository"
)

// formatTime formats a time.Time for display.
func formatTime(t time.Time) string {
	return t.Format("Jan 2, 2006")
}

// lenStr returns the count of contacts as a string.
func lenStr(contacts []repository.Contact) string {
	return fmt.Sprintf("%d", len(contacts))
}
