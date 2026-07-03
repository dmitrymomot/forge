// Package ctxkey provides a typed, collision-free context-key primitive.
//
// Declare a key once with New, then use its typed With/From accessors instead of
// hand-rolling an unexported key type and repeating ctx.Value(...).(T) assertions.
// Each key has its own identity, so keys never collide across packages even when
// they share a name.
//
//	var userKey = ctxkey.New[User]("user")
//
//	ctx = userKey.With(ctx, currentUser)
//	u, ok := userKey.From(ctx) // (User, bool) — no assertion at the call site
//
// ctxkey deliberately does not import logger: adapting Key[T].From into a
// logger.ContextExtractor is a one-liner that belongs in application wiring.
package ctxkey
