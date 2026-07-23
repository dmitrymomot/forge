package email_test

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/comms/email"
)

const welcomeTmpl = `{{define "welcome:subject"}}Welcome, {{.Name}}!{{end}}
{{define "welcome:html"}}<p>Hi {{.Name}},</p>
{{template "footer" .}}{{end}}
{{define "welcome:text"}}Hi {{.Name}},{{end}}

{{define "html-only:subject"}}Report{{end}}
{{define "html-only:html"}}<h1>Report</h1>{{end}}

{{define "no-subject:html"}}<p>orphan</p>{{end}}

{{define "empty-subject:subject"}}  {{end}}
{{define "empty-subject:text"}}body{{end}}

{{define "no-body:subject"}}Subject{{end}}
`

const footerTmpl = `{{define "footer"}}<p>— Acme</p>{{end}}`

func parseTestTemplates(t *testing.T) *email.Templates {
	t.Helper()
	fsys := fstest.MapFS{
		"templates/welcome.tmpl": {Data: []byte(welcomeTmpl)},
		"templates/footer.tmpl":  {Data: []byte(footerTmpl)},
	}
	tpl, err := email.ParseFS(fsys, "templates/*.tmpl")
	require.NoError(t, err)
	return tpl
}

func TestTemplatesRender(t *testing.T) {
	t.Parallel()
	tpl := parseTestTemplates(t)

	msg, err := tpl.Render("welcome", map[string]string{"Name": "Ann"})
	require.NoError(t, err)
	assert.Equal(t, "Welcome, Ann!", msg.Subject)
	assert.Equal(t, "<p>Hi Ann,</p>\n<p>— Acme</p>", msg.HTML, "shared partial must be callable")
	assert.Equal(t, "Hi Ann,", msg.Text)
	assert.Empty(t, msg.From, "rendering never sets addressing")
	assert.Empty(t, msg.To)
}

func TestTemplatesHTMLEscaping(t *testing.T) {
	t.Parallel()
	tpl := parseTestTemplates(t)

	msg, err := tpl.Render("welcome", map[string]string{"Name": `<script>alert(1)</script>`})
	require.NoError(t, err)
	assert.NotContains(t, msg.HTML, "<script>", "html block must auto-escape data")
	assert.Contains(t, msg.Text, "<script>", "text block must not html-escape")
}

func TestTemplatesHTMLOnly(t *testing.T) {
	t.Parallel()
	tpl := parseTestTemplates(t)

	msg, err := tpl.Render("html-only", nil)
	require.NoError(t, err)
	assert.Equal(t, "<h1>Report</h1>", msg.HTML)
	assert.Empty(t, msg.Text)
}

func TestTemplatesRenderErrors(t *testing.T) {
	t.Parallel()
	tpl := parseTestTemplates(t)

	_, err := tpl.Render("nope", nil)
	assert.ErrorIs(t, err, email.ErrTemplateNotFound)

	_, err = tpl.Render("no-subject", nil)
	assert.ErrorIs(t, err, email.ErrInvalidTemplate)

	_, err = tpl.Render("empty-subject", nil)
	assert.ErrorIs(t, err, email.ErrInvalidTemplate)

	_, err = tpl.Render("no-body", nil)
	assert.ErrorIs(t, err, email.ErrInvalidTemplate)

	var zero email.Templates
	_, err = zero.Render("welcome", nil)
	assert.ErrorIs(t, err, email.ErrInvalidTemplate)
}

func TestTemplatesMultilineSubject(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{"e.tmpl": {Data: []byte(
		"{{define \"evil:subject\"}}line one\nline two{{end}}\n{{define \"evil:text\"}}b{{end}}\n",
	)}}
	tpl, err := email.ParseFS(fsys, "*.tmpl")
	require.NoError(t, err)
	_, err = tpl.Render("evil", nil)
	assert.ErrorIs(t, err, email.ErrInvalidTemplate)
}

func TestParseFSErrors(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{"broken.tmpl": {Data: []byte(`{{define "x:subject"}}{{.Name`)}}
	_, err := email.ParseFS(fsys, "*.tmpl")
	require.Error(t, err)
}
