package mailer

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"path/filepath"
	"sync"
	texttemplate "text/template"

	"github.com/yuin/goldmark"
)

// Renderer converts markdown templates with YAML frontmatter to HTML.
type Renderer struct {
	fs fs.FS
	md goldmark.Markdown // cached markdown processor

	// Caches (safe: stores parsed structure, not rendered output)
	templateCache map[string]*cachedTemplate
	layoutCache   map[string]*template.Template
	templateDir   string
	layoutDir     string

	mu sync.RWMutex
}

// cachedTemplate holds parsed template data for reuse.
type cachedTemplate struct {
	metadata map[string]any
	tmpl     *texttemplate.Template
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
		templateCache: make(map[string]*cachedTemplate),
		layoutCache:   make(map[string]*template.Template),
	}
}

// RenderResult contains the rendered HTML, plain text, and extracted metadata.
type RenderResult struct {
	Metadata map[string]any
	HTML     string
	Text     string // Plain text from processed markdown (before HTML conversion)
}

// Render processes a markdown template with layout.
// Returns the rendered HTML, plain text, and extracted metadata.
func (r *Renderer) Render(layout, templateName string, data any) (*RenderResult, error) {
	// Get cached template (or parse and cache)
	cached, err := r.getTemplate(templateName)
	if err != nil {
		return nil, err
	}

	// Execute template with fresh data
	var processedMarkdown bytes.Buffer
	if err := cached.tmpl.Execute(&processedMarkdown, data); err != nil {
		return nil, fmt.Errorf("%w: failed to execute template: %v", ErrRenderFailed, err)
	}

	// Plain text = processed markdown (before HTML conversion)
	plainText := processedMarkdown.String()

	// Convert to HTML
	var htmlContent bytes.Buffer
	if err := r.md.Convert(processedMarkdown.Bytes(), &htmlContent); err != nil {
		return nil, fmt.Errorf("%w: failed to convert markdown: %v", ErrRenderFailed, err)
	}

	// Get cached layout (or parse and cache)
	layoutTmpl, err := r.getLayout(layout)
	if err != nil {
		return nil, err
	}

	// Execute layout with fresh content
	var finalHTML bytes.Buffer
	layoutData := map[string]any{
		"Content":  template.HTML(htmlContent.String()),
		"Metadata": cached.metadata,
	}

	if err := layoutTmpl.Execute(&finalHTML, layoutData); err != nil {
		return nil, fmt.Errorf("%w: failed to execute layout: %v", ErrRenderFailed, err)
	}

	return &RenderResult{
		HTML:     finalHTML.String(),
		Text:     plainText,
		Metadata: cached.metadata,
	}, nil
}

// getTemplate returns a cached template or parses and caches it.
func (r *Renderer) getTemplate(name string) (*cachedTemplate, error) {
	r.mu.RLock()
	if cached, ok := r.templateCache[name]; ok {
		r.mu.RUnlock()
		return cached, nil
	}
	r.mu.RUnlock()

	// Parse and cache
	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring write lock
	if cached, ok := r.templateCache[name]; ok {
		return cached, nil
	}

	path := filepath.Join(r.templateDir, name)
	content, err := fs.ReadFile(r.fs, path)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrTemplateNotFound, name, err)
	}

	parsed, err := ParseTemplate(content)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrRenderFailed, name, err)
	}

	tmpl, err := texttemplate.New(name).Parse(parsed.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to parse template body: %v", ErrRenderFailed, err)
	}

	cached := &cachedTemplate{metadata: parsed.Metadata, tmpl: tmpl}
	r.templateCache[name] = cached
	return cached, nil
}

// getLayout returns a cached layout template or parses and caches it.
func (r *Renderer) getLayout(name string) (*template.Template, error) {
	r.mu.RLock()
	if cached, ok := r.layoutCache[name]; ok {
		r.mu.RUnlock()
		return cached, nil
	}
	r.mu.RUnlock()

	// Parse and cache
	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring write lock
	if cached, ok := r.layoutCache[name]; ok {
		return cached, nil
	}

	path := filepath.Join(r.layoutDir, name)
	content, err := fs.ReadFile(r.fs, path)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrLayoutNotFound, name, err)
	}

	layoutTmpl, err := template.New(name).Parse(string(content))
	if err != nil {
		return nil, fmt.Errorf("%w: failed to parse layout: %v", ErrRenderFailed, err)
	}

	r.layoutCache[name] = layoutTmpl
	return layoutTmpl, nil
}
