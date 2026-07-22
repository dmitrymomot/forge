package markdown

import (
	"bytes"
	"fmt"
	htmltemplate "html/template"
	"strings"

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

// Render converts one markdown document into a Message with Subject, HTML
// (markdown wrapped in the layout), and Text (the plain-text alternative)
// filled — From and recipients stay with the caller. Raw HTML in the
// markdown is dropped, never passed through. The source is static content:
// to template variables into it, run text/template over the source first.
func (r *Renderer) Render(src []byte) (email.Message, error) {
	if r == nil || r.md == nil {
		return email.Message{}, fmt.Errorf("%w: renderer not constructed with New", ErrInvalidDocument)
	}
	meta, body, err := splitFrontmatter(src)
	if err != nil {
		return email.Message{}, err
	}
	var fm frontmatter
	dec := yaml.NewDecoder(bytes.NewReader(meta))
	dec.KnownFields(true)
	if err := dec.Decode(&fm); err != nil {
		return email.Message{}, fmt.Errorf("%w: frontmatter: %v", ErrInvalidDocument, err)
	}
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
