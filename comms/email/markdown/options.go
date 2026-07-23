package markdown

type config struct {
	layout      string
	buttonColor string
}

// Option configures the Renderer.
type Option func(*config)

const defaultButtonColor = "#2563eb"

// defaultLayout is the built-in single-column email shell: hidden preheader
// for inbox preview text, a centered 600px card, and a system font stack.
const defaultLayout = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Subject}}</title>
</head>
<body style="margin:0;padding:0;background-color:#f4f5f7;">
{{if .Preheader}}<div style="display:none;overflow:hidden;line-height:1px;opacity:0;max-height:0;max-width:0;">{{.Preheader}}</div>
{{end}}<table role="presentation" width="100%" border="0" cellspacing="0" cellpadding="0" style="background-color:#f4f5f7;">
<tr><td align="center" style="padding:24px 12px;">
<table role="presentation" border="0" cellspacing="0" cellpadding="0" style="width:600px;max-width:100%;background-color:#ffffff;border-radius:8px;">
<tr><td style="padding:32px 40px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:16px;line-height:1.6;color:#111827;">
{{.Body}}
</td></tr>
</table>
</td></tr>
</table>
</body>
</html>`

// WithLayout replaces the built-in HTML shell. The source is an
// html/template document executed with LayoutData; reference the rendered
// markdown as {{.Body}}. An empty layout is ignored.
func WithLayout(tmpl string) Option {
	return func(c *config) {
		if tmpl != "" {
			c.layout = tmpl
		}
	}
}

// WithButtonColor sets the CTA button background (any CSS color value,
// default "#2563eb"). An empty value is ignored.
func WithButtonColor(color string) Option {
	return func(c *config) {
		if color != "" {
			c.buttonColor = color
		}
	}
}
