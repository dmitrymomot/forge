package markdown

import (
	"bytes"
	"fmt"
	htmltemplate "html/template"
	"strings"
	texttemplate "text/template"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	"gopkg.in/yaml.v3"

	"github.com/dmitrymomot/forge/comms/email"
)

// LayoutData is what a layout template executes with: the frontmatter
// subject and preheader plus the rendered markdown body.
type LayoutData struct {
	Subject   string
	Preheader string
	Body      htmltemplate.HTML
}

// Renderer converts markdown documents with YAML frontmatter into ready
// email.Message content. Construct once and reuse; it is safe for concurrent
// use.
type Renderer struct {
	md     goldmark.Markdown
	layout *htmltemplate.Template
}

// New returns a Renderer. Defaults: a minimal responsive single-column
// layout (hidden preheader, 600px card, system font stack) and a blue CTA
// button; see WithLayout and WithButtonColor.
func New(opts ...Option) (*Renderer, error) {
	c := config{layout: defaultLayout, buttonColor: defaultButtonColor}
	for _, o := range opts {
		o(&c)
	}
	layout, err := htmltemplate.New("layout").Parse(c.layout)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidLayout, err)
	}
	// Executing with zero data now surfaces field typos at construction
	// instead of on the first send.
	if err := layout.Execute(&bytes.Buffer{}, LayoutData{}); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidLayout, err)
	}
	md := goldmark.New(
		goldmark.WithParserOptions(
			parser.WithASTTransformers(util.Prioritized(buttonTransformer{}, 500)),
		),
		goldmark.WithRendererOptions(
			renderer.WithNodeRenderers(util.Prioritized(&buttonHTMLRenderer{color: c.buttonColor}, 500)),
		),
	)
	return &Renderer{md: md, layout: layout}, nil
}

// frontmatter is the document header. Decoding is strict: an unknown key is
// an error, so a typo ("subjct") fails the render instead of sending a
// broken email.
type frontmatter struct {
	Subject   string `yaml:"subject"`
	Preheader string `yaml:"preheader"`
}

// RenderData renders a templated document: the frontmatter subject and
// preheader values and the markdown body are each executed as text/template
// with data, then rendered like Render. Missing keys are errors
// (missingkey=error), so a typo'd field fails the render instead of sending
// "<no value>".
//
// The document structure is fixed before data enters it: the frontmatter is
// split and YAML-decoded from the raw source, and only the decoded values
// are templated — a data value containing "---" or "key: x" cannot
// re-terminate the frontmatter or inject keys, and a newline landing in the
// rendered subject or preheader fails the render. Body values are still
// interpreted as markdown (a hostile string can inject a link), so data
// should be application-owned; user-controlled strings that must render
// verbatim belong in a static Render document.
func (r *Renderer) RenderData(src []byte, data any) (email.Message, error) {
	if r == nil || r.md == nil {
		return email.Message{}, fmt.Errorf("%w: renderer not constructed with New", ErrInvalidDocument)
	}
	fm, body, err := parseDocument(src)
	if err != nil {
		return email.Message{}, err
	}
	if fm.Subject, err = execTemplate("subject", fm.Subject, data); err != nil {
		return email.Message{}, err
	}
	if fm.Preheader, err = execTemplate("preheader", fm.Preheader, data); err != nil {
		return email.Message{}, err
	}
	renderedBody, err := execTemplate("body", string(body), data)
	if err != nil {
		return email.Message{}, err
	}
	return r.render(fm, []byte(renderedBody))
}

// execTemplate runs one document fragment as a text/template.
func execTemplate(name, src string, data any) (string, error) {
	tmpl, err := texttemplate.New(name).Option("missingkey=error").Parse(src)
	if err != nil {
		return "", fmt.Errorf("%w: %s template: %v", ErrInvalidDocument, name, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("email/markdown: execute %s template: %w", name, err)
	}
	return buf.String(), nil
}

// Render converts one static markdown document into a Message with Subject,
// HTML (markdown wrapped in the layout), and Text (the plain-text
// alternative) filled — From and recipients stay with the caller. Raw HTML
// in the markdown is dropped, never passed through. The source is rendered
// verbatim ("{{" stays literal); use RenderData to template values in.
func (r *Renderer) Render(src []byte) (email.Message, error) {
	if r == nil || r.md == nil {
		return email.Message{}, fmt.Errorf("%w: renderer not constructed with New", ErrInvalidDocument)
	}
	fm, body, err := parseDocument(src)
	if err != nil {
		return email.Message{}, err
	}
	return r.render(fm, body)
}

// parseDocument splits and strictly decodes the frontmatter. Subject checks
// live in render — for RenderData the values are templated in between.
func parseDocument(src []byte) (frontmatter, []byte, error) {
	meta, body, err := splitFrontmatter(src)
	if err != nil {
		return frontmatter{}, nil, err
	}
	var fm frontmatter
	dec := yaml.NewDecoder(bytes.NewReader(meta))
	dec.KnownFields(true)
	if err := dec.Decode(&fm); err != nil {
		// YAML reserves an opening brace (flow mapping), so the natural
		// RenderData pattern `subject: {{.Field}}` fails decode unless
		// quoted — point straight at the fix instead of a bare yaml error.
		if bytes.Contains(meta, []byte("{{")) {
			return frontmatter{}, nil, fmt.Errorf("%w: frontmatter: %v (a value starting with a placeholder must be quoted: subject: '%s')", ErrInvalidDocument, err, "{{.Field}}")
		}
		return frontmatter{}, nil, fmt.Errorf("%w: frontmatter: %v", ErrInvalidDocument, err)
	}
	return fm, body, nil
}

// render is the shared back half: subject contract checks, markdown to HTML
// inside the layout, and the plain-text alternative.
func (r *Renderer) render(fm frontmatter, body []byte) (email.Message, error) {
	fm.Subject = strings.TrimSpace(fm.Subject)
	fm.Preheader = strings.TrimSpace(fm.Preheader)
	if fm.Subject == "" {
		return email.Message{}, fmt.Errorf("%w: frontmatter has no subject", ErrInvalidDocument)
	}
	if strings.ContainsAny(fm.Subject, "\r\n") || strings.ContainsAny(fm.Preheader, "\r\n") {
		return email.Message{}, fmt.Errorf("%w: subject and preheader must be single-line", ErrInvalidDocument)
	}

	doc := r.md.Parser().Parse(text.NewReader(body))
	var htmlBody bytes.Buffer
	if err := r.md.Renderer().Render(&htmlBody, body, doc); err != nil {
		return email.Message{}, fmt.Errorf("email/markdown: render html: %w", err)
	}
	var page bytes.Buffer
	data := LayoutData{Subject: fm.Subject, Preheader: fm.Preheader, Body: htmltemplate.HTML(htmlBody.String())} //nolint:gosec // goldmark output with raw HTML filtered, plus our own escaped button markup
	if err := r.layout.Execute(&page, data); err != nil {
		return email.Message{}, fmt.Errorf("email/markdown: render layout: %w", err)
	}

	var plain strings.Builder
	renderText(&plain, body, doc)

	return email.Message{Subject: fm.Subject, HTML: page.String(), Text: plain.String()}, nil
}

// splitFrontmatter cuts the leading "---" YAML block from the body. The
// frontmatter is mandatory — it carries the subject.
func splitFrontmatter(src []byte) (meta, body []byte, err error) {
	normalized := bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n"))
	rest, ok := bytes.CutPrefix(normalized, []byte("---\n"))
	if !ok {
		return nil, nil, fmt.Errorf("%w: missing frontmatter", ErrInvalidDocument)
	}
	if bytes.HasPrefix(rest, []byte("---\n")) || bytes.Equal(rest, []byte("---")) {
		return nil, nil, fmt.Errorf("%w: empty frontmatter", ErrInvalidDocument)
	}
	if before, after, found := bytes.Cut(rest, []byte("\n---\n")); found {
		return before, after, nil
	}
	if before, found := bytes.CutSuffix(rest, []byte("\n---")); found {
		return before, nil, nil
	}
	return nil, nil, fmt.Errorf("%w: unterminated frontmatter", ErrInvalidDocument)
}
