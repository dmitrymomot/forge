package access

import "errors"

// errDenied is the generic error handed to the responder so the client body
// never leaks the decision reason. The full Decision (with reason) rides
// context via DecisionFrom for a custom responder and auditlog.
var errDenied = errors.New("access denied")

// errNotFound is the generic error handed to the default load-error
// responder so the client body never leaks the raw Load error text.
var errNotFound = errors.New("not found")
