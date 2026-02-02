package handlers

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/dmitrymomot/forge"
	"github.com/dmitrymomot/forge/examples/full-app/repository"
	"github.com/dmitrymomot/forge/examples/full-app/requests"
	"github.com/dmitrymomot/forge/examples/full-app/views"
)

// ContactHandler handles contact-related HTTP requests.
// Receives dependencies via constructor injection.
type ContactHandler struct {
	repo *repository.Queries
}

// NewContactHandler creates a new contact handler with injected dependencies.
func NewContactHandler(repo *repository.Queries) *ContactHandler {
	return &ContactHandler{repo: repo}
}

// uuidPattern is a regex pattern for matching UUID path parameters.
const uuidPattern = `[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`

// Routes declares all routes for the contact handler.
// Implements the forge.Handler interface.
func (h *ContactHandler) Routes(r forge.Router) {
	r.Route("/contacts", func(r forge.Router) {
		r.GET("/", h.list)
		r.GET("/new", h.form)
		r.POST("/", h.create)
		r.DELETE("/{id:"+uuidPattern+"}", h.delete)
	})
}

// list shows all contacts.
// Uses RenderPartial for HTMX-aware rendering.
func (h *ContactHandler) list(c forge.Context) error {
	contacts, err := h.repo.ListContacts(c.Context())
	if err != nil {
		return err
	}

	return c.RenderPartial(http.StatusOK,
		views.ContactsPage(contacts), // Full page for regular requests
		views.ContactsList(contacts), // Partial for HTMX requests
	)
}

// form shows the new contact form.
func (h *ContactHandler) form(c forge.Context) error {
	return c.RenderPartial(http.StatusOK,
		views.ContactsFormPage(requests.CreateContactRequest{}, nil),
		views.ContactsForm(requests.CreateContactRequest{}, nil),
	)
}

// create handles contact creation with validation.
// Demonstrates the 3-step binding: bind → sanitize → validate
func (h *ContactHandler) create(c forge.Context) error {
	var req requests.CreateContactRequest

	// Bind performs: form binding → sanitization → validation
	errs, err := c.Bind(&req)
	if err != nil {
		return err
	}
	if !errs.IsEmpty() {
		// Validation failed - re-render form with errors
		return c.Render(http.StatusUnprocessableEntity, views.ContactsForm(req, errs))
	}

	_, err = h.repo.CreateContact(c.Context(), repository.CreateContactParams{
		Name:  req.Name,
		Email: req.Email,
	})
	if err != nil {
		return err
	}

	// Redirect after successful creation
	return c.Redirect(http.StatusSeeOther, "/contacts")
}

// delete removes a contact.
func (h *ContactHandler) delete(c forge.Context) error {
	id := c.Param("id")
	if id == "" {
		return c.Error(http.StatusBadRequest, "missing contact id")
	}

	contactID, err := uuid.Parse(id)
	if err != nil {
		return c.Error(http.StatusBadRequest, "invalid contact id")
	}

	if err := h.repo.DeleteContact(c.Context(), contactID); err != nil {
		return err
	}

	// For HTMX requests, return empty response to remove the element
	if c.IsHTMX() {
		return c.NoContent(http.StatusOK)
	}

	return c.Redirect(http.StatusSeeOther, "/contacts")
}
