package requests

// CreateContactRequest is the form data for creating a contact.
type CreateContactRequest struct {
	Name  string `form:"name"  sanitize:"trim,name"       validate:"required;min:2;max:100"`
	Email string `form:"email" sanitize:"trim,lower,email" validate:"required;email"`
}
