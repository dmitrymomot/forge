package markdown

import (
	"strconv"
	"strings"

	"github.com/yuin/goldmark/ast"
)

// renderText walks the parsed document and produces the plain-text
// alternative: headings and paragraphs as bare lines separated by blank
// lines, lists with "-"/"1." markers, blockquotes with "> ", links as
// "text (url)", buttons as "label: url". Raw HTML is dropped.
func renderText(sb *strings.Builder, source []byte, doc ast.Node) {
	first := true
	for block := doc.FirstChild(); block != nil; block = block.NextSibling() {
		var out strings.Builder
		renderBlock(&out, source, block)
		text := strings.TrimRight(out.String(), "\n")
		if text == "" {
			continue
		}
		if !first {
			sb.WriteString("\n\n")
		}
		first = false
		sb.WriteString(text)
	}
}

func renderBlock(sb *strings.Builder, source []byte, block ast.Node) {
	switch n := block.(type) {
	case *ast.Heading, *ast.Paragraph, *ast.TextBlock:
		inlineText(sb, source, block)
	case *ast.Blockquote:
		var inner strings.Builder
		renderText(&inner, source, block)
		for i, line := range strings.Split(inner.String(), "\n") {
			if i > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(strings.TrimRight("> "+line, " "))
		}
	case *ast.List:
		renderList(sb, source, n)
	case *ast.CodeBlock:
		writeCodeLines(sb, source, block)
	case *ast.FencedCodeBlock:
		writeCodeLines(sb, source, block)
	case *ast.ThematicBreak:
		sb.WriteString("---")
	case *buttonNode:
		sb.WriteString(n.label)
		sb.WriteString(": ")
		sb.WriteString(n.url)
	case *ast.HTMLBlock: // dropped: raw HTML has no plain-text form
	default:
		inlineText(sb, source, block)
	}
}

func renderList(sb *strings.Builder, source []byte, list *ast.List) {
	number := list.Start
	if number == 0 {
		number = 1
	}
	firstItem := true
	for item := list.FirstChild(); item != nil; item = item.NextSibling() {
		if !firstItem {
			sb.WriteByte('\n')
		}
		firstItem = false
		marker := "- "
		if list.IsOrdered() {
			marker = strconv.Itoa(number) + ". "
			number++
		}
		var inner strings.Builder
		renderText(&inner, source, item)
		indent := strings.Repeat(" ", len(marker))
		for i, line := range strings.Split(inner.String(), "\n") {
			if i == 0 {
				sb.WriteString(marker)
			} else {
				sb.WriteByte('\n')
				if line != "" {
					sb.WriteString(indent)
				}
			}
			sb.WriteString(line)
		}
	}
}

func writeCodeLines(sb *strings.Builder, source []byte, block ast.Node) {
	lines := block.Lines()
	for i := range lines.Len() {
		line := lines.At(i)
		sb.WriteString(strings.TrimRight(string(line.Value(source)), "\n"))
		if i < lines.Len()-1 {
			sb.WriteByte('\n')
		}
	}
}

// inlineText flattens an inline tree to plain text. Shared by the text
// renderer and the button transformer's label extraction.
func inlineText(sb *strings.Builder, source []byte, parent ast.Node) {
	for c := parent.FirstChild(); c != nil; c = c.NextSibling() {
		switch n := c.(type) {
		case *ast.Text:
			sb.Write(n.Segment.Value(source))
			if n.SoftLineBreak() || n.HardLineBreak() {
				sb.WriteByte('\n')
			}
		case *ast.String:
			sb.Write(n.Value)
		case *ast.Link:
			var label strings.Builder
			inlineText(&label, source, c)
			url := string(n.Destination)
			switch {
			case label.String() == "" || label.String() == url:
				sb.WriteString(url)
			default:
				sb.WriteString(label.String())
				sb.WriteString(" (")
				sb.WriteString(url)
				sb.WriteString(")")
			}
		case *ast.AutoLink:
			sb.Write(n.URL(source))
		case *ast.Image:
			var alt strings.Builder
			inlineText(&alt, source, c)
			if alt.Len() > 0 {
				sb.WriteString(alt.String())
			}
		case *ast.RawHTML: // dropped
		default:
			inlineText(sb, source, c)
		}
	}
}
