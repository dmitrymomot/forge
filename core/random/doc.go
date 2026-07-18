// Package random provides small, safe helpers over crypto/rand for secure entropy:
// token nonces, salts, and opaque identifiers.
//
// Bytes, Hex, and URLSafe panic on a crypto/rand failure, which means the OS
// RNG is broken and the program cannot safely continue; use Read for an
// error-returning variant. Int is unbiased (rejection sampling via crypto/rand.Int)
// and additionally panics if max <= 0.
//
// String draws n characters from one or more charsets (Lowercase, Uppercase, Digits,
// Alphabetic, Alphanumeric, Symbols), defaulting to Alphanumeric when none are given.
// Overlapping charsets are de-duplicated so the result stays uniform over distinct
// characters. Like Bytes, it panics on a crypto/rand failure, and additionally
// panics if n < 0 or the combined charset is empty; use Read if you need the
// error-returning escape hatch instead. DigitCode is a thin wrapper over
// String(n, Digits) for OTP and verification codes, preserving leading zeros, and
// panics if n <= 0.
//
// # Usage
//
//	salt := random.Bytes(16)
//	id := random.URLSafe(24)
//	code := random.DigitCode(6)
package random
