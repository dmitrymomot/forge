package mailer

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"path/filepath"
	texttemplate "text/template"

	"github.com/yuin/goldmark"
)

// Renderer converts markdown templates with YAML frontmatter to HTML.
type Renderer struct {
	fs          fs.FS
	md          goldmark.Markdown // cached markdown processor
	templateDir string
	layoutDir   string
}

// RendererConfig configures the renderer.
type RendererConfig struct {
	TemplateDir string // Default: "."
	LayoutDir   string // Default: "layouts"
}

// NewRenderer creates a new renderer with default config.
func NewRenderer(filesystem fs.FS) *Renderer {
	return NewRendererWithConfig(filesystem, RendererConfig{})
}

// NewRendererWithConfig creates a new renderer with custom config.
func NewRendererWithConfig(filesystem fs.FS, opts RendererConfig) *Renderer {
	if opts.TemplateDir == "" {
		opts.TemplateDir = "."
	}
	if opts.LayoutDir == "" {
		opts.LayoutDir = "layouts"
	}

	return &Renderer{
		fs:          filesystem,
		templateDir: opts.TemplateDir,
		layoutDir:   opts.LayoutDir,
		md: goldmark.New(
			goldmark.WithExtensions(NewButtonExtension()),
		),
	}
}

// RenderResult contains the rendered HTML and extracted metadata.
type RenderResult struct {
	Metadata map[string]any
	HTML     string
}

// Render processes a markdown template with layout.
// Returns the rendered HTML and extracted metadata.
func (r *Renderer) Render(layout, templateName string, data any) (*RenderResult, error) {
	templatePath := filepath.Join(r.templateDir, templateName)
	templateContent, err := fs.ReadFile(r.fs, templatePath)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrTemplateNotFound, templateName, err)
	}

	tmpl, err := ParseTemplate(templateContent)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrRenderFailed, templateName, err)
	}

	textTmpl, err := texttemplate.New(templateName).Parse(tmpl.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to parse template body: %v", ErrRenderFailed, err)
	}

	var processedMarkdown bytes.Buffer
	if err := textTmpl.Execute(&processedMarkdown, data); err != nil {
		return nil, fmt.Errorf("%w: failed to execute template: %v", ErrRenderFailed, err)
	}

	var htmlContent bytes.Buffer
	if err := r.md.Convert(processedMarkdown.Bytes(), &htmlContent); err != nil {
		return nil, fmt.Errorf("%w: failed to convert markdown: %v", ErrRenderFailed, err)
	}

	layoutPath := filepath.Join(r.layoutDir, layout)
	layoutContent, err := fs.ReadFile(r.fs, layoutPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrLayoutNotFound, layout, err)
	}

	layoutTmpl, err := template.New(layout).Parse(string(layoutContent))
	if err != nil {
		return nil, fmt.Errorf("%w: failed to parse layout: %v", ErrRenderFailed, err)
	}

	var finalHTML bytes.Buffer
	layoutData := map[string]any{
		"Content":  template.HTML(htmlContent.String()),
		"Metadata": tmpl.Metadata,
	}

	if err := layoutTmpl.Execute(&finalHTML, layoutData); err != nil {
		return nil, fmt.Errorf("%w: failed to execute layout: %v", ErrRenderFailed, err)
	}

	return &RenderResult{
		HTML:     finalHTML.String(),
		Metadata: tmpl.Metadata,
	}, nil
}
