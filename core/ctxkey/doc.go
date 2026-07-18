// Package ctxkey provides a typed, collision-free context-key primitive.
//
// Declare a key once with New, then use its typed With/From accessors instead of
// hand-rolling an unexported key type and repeating ctx.Value(...).(T) assertions.
// Each key has its own identity, so keys never collide across packages even when
// they share a name.
//
// ctxkey deliberately does not import logger: adapting Key[T].From into a
// logger.ContextExtractor is a one-liner that belongs in application wiring.
//
// # Usage
//
//	ctx := context.Background()
//	userIDKey := ctxkey.New[string]("userID")
//
//	ctx = userIDKey.With(ctx, "u_123")
//	id, ok := userIDKey.From(ctx) // (string, bool) — no assertion at the call site
package ctxkey
