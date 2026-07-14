package access

import "errors"

// errDenied is the generic error handed to the responder so the client body
// never leaks the decision reason. The full Decision (with reason) rides
// context via DecisionFrom for a custom responder and auditlog.
var errDenied = errors.New("access denied")
