package formula

import "errors"

// ErrInvalidSpec is returned by Spec.Validate (and therefore Compile) when the
// spec structure is invalid: missing version, no stages, duplicate or empty
// stage names, a term or arg referencing the stage itself or a later stage, an
// empty clamp, min > max, or a bad round scale/mode. The wrapping error names
// the offending stage.
var ErrInvalidSpec = errors.New("formula: invalid spec")

// ErrInvalidFunc is returned by Compile when a WithFunc registration is
// unusable: an empty name, a nil function, or a duplicate name.
var ErrInvalidFunc = errors.New("formula: invalid func registration")

// ErrUnknownFunc is returned by Compile when a func stage references a name
// that no WithFunc option registered.
var ErrUnknownFunc = errors.New("formula: unknown func")

// ErrUnknownMetric is returned by Eval when a term or arg references a name
// that is neither a prior stage nor a provided input.
var ErrUnknownMetric = errors.New("formula: unknown metric")

// ErrMetricCollision is returned by Eval when an input name collides with a
// stage name, which would make the reference ambiguous.
var ErrMetricCollision = errors.New("formula: input name collides with stage name")

// ErrFuncFailed is returned by Eval when a registered function returns an
// error; the function's own error is wrapped alongside for errors.Is matching.
var ErrFuncFailed = errors.New("formula: func failed")
