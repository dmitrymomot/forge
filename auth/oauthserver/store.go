package oauthserver

import "context"

// Store persists the client registry. Implementations must return
// ErrDuplicateClient from Create on an existing ID and ErrClientNotFound
// from Get/Update on a missing one. List("") returns every client;
// List(tenantID) filters by tenant.
type Store interface {
	Create(ctx context.Context, c Client) error
	Get(ctx context.Context, id string) (Client, error)
	Update(ctx context.Context, c Client) error
	List(ctx context.Context, tenantID string) ([]Client, error)
	Delete(ctx context.Context, id string) error
}
