package render

import "errors"

// ErrNilTemplate is returned by HTML when the provided template is nil.
var ErrNilTemplate = errors.New("render: nil template")

// ErrNilComponent is returned by Templ when the provided component is nil.
var ErrNilComponent = errors.New("render: nil component")

// ErrNilBody is returned by Stream and Attachment when the body reader is nil.
var ErrNilBody = errors.New("render: nil body")
