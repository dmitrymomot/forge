// Package session stores durable per-visitor state behind a rotating token.
//
// A session is a bucket, not an authentication mechanism: it holds data for
// anonymous and signed-in visitors alike. Authentication gating belongs to
// auth/guard, authorization to auth/access. Session records who the visitor is
// and hands that fact to those packages through an adapter.
//
// There are two entry points. New returns a Manager, which owns lifecycle and
// storage and knows nothing about HTTP. Middleware owns the request layer:
// it extracts the credential, loads and validates the record, runs policies,
// exposes the session on the context, and commits exactly once before the
// first byte of the response — which is why a login handler can redirect and
// still set its cookie.
//
// Stores, transports, and policies all live in sibling packages that import
// this one and use only its exported API, so a third-party implementation is
// indistinguishable from a first-party one.
//
// Session is single-tenant by default: no option costs a single-tenant app
// any ceremony, and every record's Tenant column stays empty. Passing
// WithScope to New turns on multi-tenancy: the hook resolves the tenant for
// the current request from context — web/tenant.Scope plugs in directly —
// every save stamps it, and every load and device-management call is
// confined to it. A hook error or an empty scope fails the operation closed
// rather than letting a session cross tenants.
//
// # Usage
//
//	var Cart = session.NewNamespace[CartData]("cart")
//
//	sessions, err := session.New(session.DefaultConfig(),
//		session.WithStore(session.NewMemoryStore()),
//		session.WithIdle(24*time.Hour),
//		session.WithMaxTTL(7*24*time.Hour),
//	)
//	if err != nil {
//		return err
//	}
//
//	mux.Handle("/", session.Middleware(sessions,
//		session.WithTransport(myTransport),
//	)(handler))
//
//	func addToCart(w http.ResponseWriter, r *http.Request) {
//		sess := sessions.MustFor(r)
//		cart, err := Cart.Get(sess)
//		if err != nil {
//			http.Error(w, "bad session", http.StatusInternalServerError)
//			return
//		}
//		cart.Items = append(cart.Items, "sku-1")
//		Cart.Set(sess, cart)
//	}
package session
