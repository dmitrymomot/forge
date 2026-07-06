// Package cookie is a signed + encrypted cookie codec with secure defaults
// (HttpOnly, Secure, SameSite=Lax, __Host- support) and keyset rotation.
//
// Three security levels, chosen per call:
//   - Set/Get: plain value, policy flags still applied.
//   - SetSigned/GetSigned: HMAC, tamper-proof but client-readable.
//   - SetEncrypted/GetEncrypted: AEAD, tamper-proof AND opaque. The AEAD auth
//     tag already provides integrity — encrypted cookies are not additionally
//     signed because that would be pure overhead.
//
// Values are bound to their cookie name (MAC message / AEAD AAD), so a value
// minted for one cookie cannot be replayed under another name.
//
//	ks, _ := keyset.New(keyset.WithBase64Keys(os.Getenv("COOKIE_KEYS")))
//	codec, _ := cookie.New(ks)
//	_ = codec.SetSigned(w, "__Host-csrf", token)
//	_ = codec.SetEncrypted(w, "session", sid, cookie.WithWriteMaxAge(24*time.Hour))
package cookie
