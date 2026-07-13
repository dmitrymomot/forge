package oauthserver

import (
	"encoding/json"
	"net/http"
)

// writeTokenError writes an RFC 6749 §5.2 error. The token endpoint
// deliberately does NOT speak problem+json: partners' OAuth libraries
// expect the RFC shape. Descriptions are static strings — internal error
// text never reaches the wire.
func writeTokenError(w http.ResponseWriter, status int, code, desc string) {
	w.Header().Set("Content-Type", "application/json;charset=UTF-8")
	w.Header().Set("Cache-Control", "no-store")
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Basic realm="oauth", charset="UTF-8"`)
	}
	w.WriteHeader(status)
	body := map[string]string{"error": code}
	if desc != "" {
		body["error_description"] = desc
	}
	_ = json.NewEncoder(w).Encode(body)
}

// tokenResponse is the RFC 6749 §5.1 success body.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope,omitempty"`
	IDToken     string `json:"id_token,omitempty"`
	ExpiresIn   int64  `json:"expires_in"`
}

func writeTokenResponse(w http.ResponseWriter, resp tokenResponse) {
	w.Header().Set("Content-Type", "application/json;charset=UTF-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(resp)
}
