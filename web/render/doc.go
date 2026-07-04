// Package render provides small, stateless helpers for writing HTTP responses from a
// handler: JSON/JSONStream, HTML (html/template), Templ (a-h/templ components, via a
// structural interface — no dependency), Components (several components in one body),
// Text, Blob, CSV, Stream, Attachment, File/FileFS, Redirect, and NoContent.
//
// The helpers are free functions — there is no constructor, options, or global state.
// The caller owns its *template.Template and handles the returned error.
//
// JSON, HTML, and Templ are transactional: they encode into a pooled buffer first, so
// an encode/template error returns with nothing written and the caller can still send
// a clean error status. The streaming writers (JSONStream, CSV, Stream, Attachment)
// write directly, so a mid-stream error may leave a partial body and the returned
// error is only useful for logging. Content-Type is set only when the caller has not
// already set one.
//
// render does not negotiate content (the handler picks the function) and never fetches
// remote URLs: serve an S3 object with Redirect, or fetch it in the handler and pass
// the body to Stream/Attachment.
//
// # Usage
//
//	func handle(w http.ResponseWriter, r *http.Request) {
//		if err := render.JSON(w, http.StatusOK, user); err != nil {
//			// log err; the response may be incomplete
//		}
//	}
package render
