// Package keyset is an in-memory versioned keyring backing key rotation for the sign,
// secret, and token packages. It holds one primary key (used for new operations) plus
// any number of retired keys (used to decrypt or verify older material), loaded from
// base64 environment material or set explicitly.
//
//	ks, _ := keyset.New(keyset.WithBase64Keys(os.Getenv("FORGE_SECRET_KEYS")))
//	// FORGE_SECRET_KEYS = "2:<base64 new>,1:<base64 old>"
//	box, _ := secret.FromKeyset(ks)
//
// It is not a cloud KMS client; fetching secrets from a vault belongs to secretsource.
package keyset
