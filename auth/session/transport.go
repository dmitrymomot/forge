package session

import "net/http"

// Transport moves the session credential between the server and the client.
// Implementations live outside this package: if a transport cannot be written
// against this interface alone, the interface is wrong.
type Transport interface {
	// Extract finds the credential on the request.
	Extract(r *http.Request) (token string, ok bool)
	// Embed writes the credential to the response. It sets headers only — it
	// must never write a status or a body, because commit runs inside the
	// handler's WriteHeader and would override the handler's own response.
	// A transport that can read but not write returns ErrNoEmbed.
	Embed(w http.ResponseWriter, r *http.Request, s *Session) error
	// Clear removes the credential from the client.
	Clear(w http.ResponseWriter, r *http.Request)
}
