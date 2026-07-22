// Package markdown renders markdown documents with YAML frontmatter into
// ready email.Message content — the designer-free transactional email
// format. One source file yields the subject, an HTML body wrapped in an
// email-safe layout (hidden preheader, 600px card), and the plain-text
// alternative; goldmark is confined to this subpackage.
//
// A document is YAML frontmatter (subject required, preheader optional)
// followed by CommonMark:
//
//	---
//	subject: Confirm your email
//	preheader: One click and you're in.
//	---
//	# Almost there
//
//	Hi! Confirm your address to activate your account.
//
//	[Button: Confirm email](https://app.acme.example/confirm?t=abc)
//
// A paragraph holding exactly one link whose text starts with "Button:"
// becomes a table-based CTA button (plain text renders it as "label: url").
// Frontmatter decoding is strict — an unknown key fails the render — and raw
// HTML in the markdown is dropped, never passed through.
//
// Render treats the source as static content ("{{" stays literal).
// RenderData templates the frontmatter subject/preheader values and the body
// with data ({{.Field}}), with missingkey=error so a typo'd field fails the
// render. The document structure is parsed before data enters it, so a data
// value cannot re-terminate the frontmatter or inject keys, and a newline
// landing in the subject or preheader fails the render. Body values are
// still interpreted as markdown. Because the frontmatter is YAML first, a
// value that begins with a placeholder must be quoted:
//
//	subject: 'Welcome, {{.Name}}!'   // always safe
//	subject: {{.Subject}}            // invalid YAML — quote it
//
// # Non-goals
//
//   - No CSS inliner or theming system: WithLayout replaces the whole shell
//     when the default card doesn't fit.
//   - No GFM extensions (tables, strikethrough): transactional email bodies
//     are prose, headings, lists, and buttons.
//   - No markdown-escaping of templated body values: a hostile string can
//     still inject a link or emphasis (never raw HTML — that is dropped), so
//     RenderData data is application-owned by contract; user-generated
//     strings that must render verbatim belong in static Render documents.
//
// # Usage
//
//	r, err := markdown.New()
//	if err != nil {
//		// bad custom layout
//	}
//	msg, err := r.RenderData(welcomeMD, map[string]string{"Name": "Ann"})
//	if err != nil {
//		// malformed frontmatter, markdown, or template data
//	}
//	msg.To = []string{"ann@example.com"}
//	err = sender.Send(ctx, msg) // any email.Sender
package markdown
