package mailer

import (
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// ButtonNode represents a button link in the AST.
type ButtonNode struct {
	ast.BaseInline
	URL   []byte
	Label []byte
}

func (n *ButtonNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

// KindButton is the node kind for ButtonNode.
var KindButton = ast.NewNodeKind("Button")

// buttonPrefix is the syntax prefix that triggers button parsing.
const buttonPrefix = "[!button|"

func (n *ButtonNode) Kind() ast.NodeKind {
	return KindButton
}

// buttonParser parses button syntax: [!button|Text](URL).
type buttonParser struct{}

// NewButtonParser creates a new button inline parser.
func NewButtonParser() parser.InlineParser {
	return &buttonParser{}
}

func (s *buttonParser) Trigger() []byte {
	return []byte{'['}
}

func (s *buttonParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, _ := block.PeekLine()
	if line == nil {
		return nil
	}

	if len(line) < len(buttonPrefix) || string(line[:len(buttonPrefix)]) != buttonPrefix {
		return nil
	}

	textEnd := -1
	for i := len(buttonPrefix); i < len(line); i++ {
		if line[i] == ']' {
			textEnd = i
			break
		}
	}

	if textEnd == -1 {
		return nil
	}

	buttonText := line[len(buttonPrefix):textEnd]

	if textEnd+1 >= len(line) || line[textEnd+1] != '(' {
		return nil
	}

	// Extract URL from parentheses
	urlStart := textEnd + 2
	urlEnd := -1
	for i := urlStart; i < len(line); i++ {
		if line[i] == ')' {
			urlEnd = i
			break
		}
	}

	if urlEnd == -1 {
		return nil
	}

	url := line[urlStart:urlEnd]

	block.Advance(urlEnd + 1)

	return &ButtonNode{
		URL:   url,
		Label: buttonText,
	}
}

// buttonRenderer renders ButtonNode to HTML.
type buttonRenderer struct {
	html.Config
}

// NewButtonRenderer creates a new button node renderer.
func NewButtonRenderer(opts ...html.Option) renderer.NodeRenderer {
	r := &buttonRenderer{
		Config: html.NewConfig(),
	}
	for _, opt := range opts {
		opt.SetHTMLOption(&r.Config)
	}
	return r
}

func (r *buttonRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(KindButton, r.renderButton)
}

func (r *buttonRenderer) renderButton(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}

	n := node.(*ButtonNode)

	_, _ = w.WriteString(`<a href="`)
	_, _ = w.Write(util.EscapeHTML(n.URL))
	_, _ = w.WriteString(`" class="btn">`)
	_, _ = w.Write(util.EscapeHTML(n.Label))
	_, _ = w.WriteString(`</a>`)

	return ast.WalkContinue, nil
}

// ButtonExtension is a goldmark extension for button links.
type ButtonExtension struct{}

func (e *ButtonExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithInlineParsers(
		util.Prioritized(NewButtonParser(), 50),
	))
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(NewButtonRenderer(), 50),
	))
}

// NewButtonExtension creates a new button extension for goldmark.
func NewButtonExtension() goldmark.Extender {
	return &ButtonExtension{}
}
