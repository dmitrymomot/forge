package filetype

import "errors"

// ErrNilReader is returned by DetectReader when passed a nil io.Reader.
var ErrNilReader = errors.New("filetype: nil reader")
