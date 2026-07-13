package oauthclient

import "errors"

var (
	// ErrUnknownProvider is returned when a provider name matches neither the
	// static registry nor the provider source.
	ErrUnknownProvider = errors.New("oauthclient: unknown provider")
	// ErrFlowExpired is returned when the flow token/cookie is missing,
	// expired, or tampered with.
	ErrFlowExpired = errors.New("oauthclient: flow expired or missing")
	// ErrStateMismatch is returned when the callback state does not match the flow.
	ErrStateMismatch = errors.New("oauthclient: state mismatch")
	// ErrNonceMismatch is returned when the id_token nonce does not match the flow.
	ErrNonceMismatch = errors.New("oauthclient: nonce mismatch")
	// ErrScopeBinding is returned when the tenancy scope at Exchange differs
	// from the one sealed at Begin.
	ErrScopeBinding = errors.New("oauthclient: scope binding mismatch")
	// ErrNoIdentity is returned when a provider yields neither an id_token
	// nor an Identity hook result with a subject.
	ErrNoIdentity = errors.New("oauthclient: provider returned no identity")
	// ErrInvalidConfig is returned by New/FromConfig/AuthURL for invalid setup.
	ErrInvalidConfig = errors.New("oauthclient: invalid config")
	// ErrReservedParam is returned when Provider.AuthParams collides with a
	// protocol-owned authorize parameter.
	ErrReservedParam = errors.New("oauthclient: reserved auth param")
	// ErrDiscovery is returned when OIDC discovery fails or the returned
	// issuer does not match the requested one.
	ErrDiscovery = errors.New("oauthclient: discovery failed")
)

// ProviderError is an OAuth 2.0 error returned by the provider, either via
// the error= callback parameters or an RFC 6749 §5.2 token-endpoint response.
type ProviderError struct {
	Code        string
	Description string
}

func (e *ProviderError) Error() string {
	if e.Description == "" {
		return "oauthclient: provider error: " + e.Code
	}
	return "oauthclient: provider error: " + e.Code + ": " + e.Description
}
