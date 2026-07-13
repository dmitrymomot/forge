package oauthclient

import (
	"net/http"
	"time"
)

// Begin starts a login flow and sends the user to the provider: AuthURL +
// flow cookie + 303 redirect. The cookie is HttpOnly, Secure, SameSite=Lax
// (Lax survives the top-level redirect back from the provider).
func (c *Client) Begin(w http.ResponseWriter, r *http.Request, provider string, opts ...BeginOption) error {
	flow, err := c.AuthURL(r.Context(), provider, opts...)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     c.cookieName,
		Value:    flow.FlowToken,
		Path:     "/",
		MaxAge:   int(c.flowTTL / time.Second),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, flow.URL, http.StatusSeeOther)
	return nil
}

// Complete finishes the flow on the callback request: reads and clears the
// flow cookie, then runs Exchange with the callback query.
func (c *Client) Complete(w http.ResponseWriter, r *http.Request) (*Result, error) {
	ck, err := r.Cookie(c.cookieName)
	if err != nil {
		return nil, ErrFlowExpired
	}
	http.SetCookie(w, &http.Cookie{
		Name:     c.cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	return c.Exchange(r.Context(), ck.Value, r.URL.Query())
}
