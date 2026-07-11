package assets

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"path"
)

// shortHash is the first 8 hex chars of the SHA-256 of data — the filename tag.
func shortHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:8]
}

// sri is the Subresource Integrity value (SHA-384, base64) for data.
func sri(data []byte) string {
	sum := sha512.Sum384(data)
	return "sha384-" + base64.StdEncoding.EncodeToString(sum[:])
}

// injectHash inserts hash before name's extension: app.css → app.<hash>.css.
// A name without an extension gets the hash appended: LICENSE → LICENSE.<hash>.
func injectHash(name, hash string) string {
	ext := path.Ext(name)
	return name[:len(name)-len(ext)] + "." + hash + ext
}
