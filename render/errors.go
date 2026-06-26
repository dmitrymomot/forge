package render

import "errors"

// ErrNilTemplate is returned by HTML when the provided template is nil.
var ErrNilTemplate = errors.New("render: nil template")

// ErrNilComponent is returned by Templ when the provided component is nil.
var ErrNilComponent = errors.New("render: nil component")
