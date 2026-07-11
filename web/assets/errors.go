package assets

import "errors"

// ErrManifest marks a malformed external manifest, or one that references a
// file absent from the fs.FS.
var ErrManifest = errors.New("assets: invalid manifest")
