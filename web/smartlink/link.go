package smartlink

import "time"

// Link is a stored short-code record: a code bound to a redirect target —
// either a literal Target URL or, via Ref, a compiled [Spec] resolved by a
// Manager (a later task) — plus tenant scope and caller metadata. ShortURL is
// derived from Code and the app's base URL for API responses; it is never
// persisted.
type Link struct {
	CreatedAt     time.Time         `json:"created_at"`
	ExpiresAt     time.Time         `json:"expires_at,omitzero"`
	DeactivatedAt time.Time         `json:"deactivated_at,omitzero"`
	Code          string            `json:"code"`
	Target        string            `json:"target,omitempty"`
	Ref           string            `json:"ref,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	Tenant        string            `json:"tenant,omitempty"`
	ShortURL      string            `json:"short_url,omitempty"`
}

// CreateParams are the caller-supplied fields for creating a Link. A Manager
// (a later task) validates Target/Ref, generates Code when empty, and stamps
// CreatedAt before handing the resulting Link to Store.Create. SkipRefCheck
// bypasses the Manager's check that Ref names a known compiled Spec, for
// callers that register the Spec after the Link.
type CreateParams struct {
	ExpiresAt    time.Time
	Target       string
	Ref          string
	Code         string
	Metadata     map[string]string
	Tenant       string
	SkipRefCheck bool
}

// Filter narrows Store.List. Zero fields match everything; Limit 0 means no cap.
type Filter struct {
	Tenant string
	Limit  int
}
