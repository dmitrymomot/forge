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
// # Non-goals
//
//   - No variable interpolation: the source is static content; run
//     text/template over it first if a document needs data.
//   - No CSS inliner or theming system: WithLayout replaces the whole shell
//     when the default card doesn't fit.
//   - No GFM extensions (tables, strikethrough): transactional email bodies
//     are prose, headings, lists, and buttons.
//
// # Usage
//
//	r, err := markdown.New()
//	if err != nil {
//		// bad custom layout
//	}
//	msg, err := r.Render(welcomeMD)
//	if err != nil {
//		// malformed frontmatter or markdown
//	}
//	msg.To = []string{"ann@example.com"}
//	err = sender.Send(ctx, msg) // any email.Sender
package markdown
