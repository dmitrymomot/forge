package email

import (
	"fmt"
	htmltemplate "html/template"
	"io/fs"
	"strings"
	texttemplate "text/template"
)

// Templates renders named emails into a Message's content fields. Every
// template file defines up to three blocks per email name — "<name>:subject"
// (required), "<name>:html", "<name>:text" (at least one body required):
//
//	{{define "welcome:subject"}}Welcome, {{.Name}}!{{end}}
//	{{define "welcome:html"}}<p>Hi {{.Name}},</p>{{end}}
//	{{define "welcome:text"}}Hi {{.Name}},{{end}}
//
// The html blocks render through html/template (contextual auto-escaping);
// subject and text blocks render through text/template. Shared partials work
// the usual way: any block defined in the parsed set is callable from any
// other.
type Templates struct {
	html *htmltemplate.Template
	text *texttemplate.Template
}

// ParseFS parses every template matching patterns (fs.Glob syntax, like
// template.ParseFS) into one named set.
func ParseFS(fsys fs.FS, patterns ...string) (*Templates, error) {
	ht, err := htmltemplate.ParseFS(fsys, patterns...)
	if err != nil {
		return nil, fmt.Errorf("email: parse templates: %w", err)
	}
	tt, err := texttemplate.ParseFS(fsys, patterns...)
	if err != nil {
		return nil, fmt.Errorf("email: parse templates: %w", err)
	}
	return &Templates{html: ht, text: tt}, nil
}

// Render executes name's blocks with data and returns a Message with
// Subject, HTML, and Text filled — From and recipients stay with the caller.
// A name with no blocks at all returns ErrTemplateNotFound; a name missing
// its subject or defining neither body returns ErrInvalidTemplate.
func (t *Templates) Render(name string, data any) (Message, error) {
	if t == nil || t.text == nil || t.html == nil {
		return Message{}, fmt.Errorf("%w: templates not constructed with ParseFS", ErrInvalidTemplate)
	}
	subjectTpl := t.text.Lookup(name + ":subject")
	htmlTpl := t.html.Lookup(name + ":html")
	textTpl := t.text.Lookup(name + ":text")
	if subjectTpl == nil && htmlTpl == nil && textTpl == nil {
		return Message{}, fmt.Errorf("%w: %q", ErrTemplateNotFound, name)
	}
	if subjectTpl == nil {
		return Message{}, fmt.Errorf("%w: %q defines no subject block", ErrInvalidTemplate, name)
	}
	if htmlTpl == nil && textTpl == nil {
		return Message{}, fmt.Errorf("%w: %q defines neither html nor text block", ErrInvalidTemplate, name)
	}

	var sb strings.Builder
	if err := subjectTpl.Execute(&sb, data); err != nil {
		return Message{}, fmt.Errorf("email: render %s:subject: %w", name, err)
	}
	subject := strings.TrimSpace(sb.String())
	if subject == "" {
		return Message{}, fmt.Errorf("%w: %q renders an empty subject", ErrInvalidTemplate, name)
	}
	if strings.ContainsAny(subject, "\r\n") {
		return Message{}, fmt.Errorf("%w: %q renders a multi-line subject", ErrInvalidTemplate, name)
	}

	msg := Message{Subject: subject}
	if htmlTpl != nil {
		sb.Reset()
		if err := htmlTpl.Execute(&sb, data); err != nil {
			return Message{}, fmt.Errorf("email: render %s:html: %w", name, err)
		}
		msg.HTML = strings.TrimSpace(sb.String())
	}
	if textTpl != nil {
		sb.Reset()
		if err := textTpl.Execute(&sb, data); err != nil {
			return Message{}, fmt.Errorf("email: render %s:text: %w", name, err)
		}
		msg.Text = strings.TrimSpace(sb.String())
	}
	return msg, nil
}
