package markdown

import (
	"html"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// buttonNode is a paragraph promoted to a call-to-action button by
// buttonTransformer.
type buttonNode struct {
	label string
	url   string
	ast.BaseBlock
}

var kindButton = ast.NewNodeKind("EmailButton")

func (n *buttonNode) Kind() ast.NodeKind { return kindButton }

func (n *buttonNode) Dump(src []byte, level int) {
	ast.DumpHelper(n, src, level, map[string]string{"Label": n.label, "URL": n.url}, nil)
}

// buttonTransformer promotes CTA paragraphs to buttons: a paragraph whose
// entire content is one link whose text starts with "Button:" and whose
// destination is absolute http(s). Anything else — extra text in the
// paragraph, a relative or non-http destination — is left as a plain link,
// so a malformed CTA degrades visibly instead of silently vanishing.
type buttonTransformer struct{}

func (buttonTransformer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
	type hit struct {
		paragraph *ast.Paragraph
		label     string
		url       string
	}
	var hits []hit
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		paragraph, ok := n.(*ast.Paragraph)
		if !ok || paragraph.ChildCount() != 1 {
			return ast.WalkContinue, nil
		}
		link, ok := paragraph.FirstChild().(*ast.Link)
		if !ok {
			return ast.WalkContinue, nil
		}
		var sb strings.Builder
		inlineText(&sb, source, link)
		label, ok := strings.CutPrefix(sb.String(), "Button:")
		if !ok {
			return ast.WalkContinue, nil
		}
		label = strings.TrimSpace(label)
		url := string(link.Destination)
		if label == "" || (!strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://")) {
			return ast.WalkContinue, nil
		}
		hits = append(hits, hit{paragraph: paragraph, label: label, url: url})
		return ast.WalkSkipChildren, nil
	})
	for _, h := range hits {
		button := &buttonNode{label: h.label, url: h.url}
		parent := h.paragraph.Parent()
		parent.ReplaceChild(parent, h.paragraph, button)
	}
}

// buttonHTMLRenderer emits the bulletproof-button table markup email clients
// require (a styled <a> alone collapses in Outlook).
type buttonHTMLRenderer struct {
	color string
}

func (r *buttonHTMLRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindButton, r.render)
}

func (r *buttonHTMLRenderer) render(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*buttonNode)
	w.WriteString(`<table role="presentation" border="0" cellspacing="0" cellpadding="0" style="margin:24px 0;"><tr><td style="border-radius:6px;background-color:`) //nolint:errcheck // BufWriter errors surface on flush
	w.WriteString(html.EscapeString(r.color))                                                                                                                        //nolint:errcheck // BufWriter errors surface on flush
	w.WriteString(`;"><a href="`)                                                                                                                                    //nolint:errcheck // BufWriter errors surface on flush
	w.WriteString(html.EscapeString(n.url))                                                                                                                          //nolint:errcheck // BufWriter errors surface on flush
	w.WriteString(`" target="_blank" style="display:inline-block;padding:12px 28px;font-size:16px;font-weight:600;color:#ffffff;text-decoration:none;">`)            //nolint:errcheck // BufWriter errors surface on flush
	w.WriteString(html.EscapeString(n.label))                                                                                                                        //nolint:errcheck // BufWriter errors surface on flush
	w.WriteString(`</a></td></tr></table>`)                                                                                                                          //nolint:errcheck // BufWriter errors surface on flush
	return ast.WalkContinue, nil
}
