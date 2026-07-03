// Package random provides small, safe helpers over crypto/rand for secure entropy:
// token nonces, salts, and opaque identifiers.
//
//	salt := random.Bytes(16)
//	id := random.URLSafe(24)
//
// Bytes/Hex/URLSafe/Int panic only on a crypto/rand failure, which means the OS RNG
// is broken and the program cannot safely continue. Use Read for an error-returning
// variant. Int is unbiased (rejection sampling via crypto/rand.Int).
package random
