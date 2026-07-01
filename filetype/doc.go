// Package filetype detects a file's real MIME type from magic-byte signatures
// rather than trusting a client-supplied filename extension or Content-Type
// header — the guard against a renamed .exe uploaded as .png. It matches a
// curated signature table (images, pdf, zip, gzip, tar, common audio/video)
// and falls back to net/http.DetectContentType for everything else.
//
// What this is NOT: it is not a container inspector. OOXML files (docx/xlsx/
// pptx) share the ZIP "PK" signature, so Detect reports application/zip for
// them; telling those sub-types apart requires reading the archive directory
// and is out of scope. It does not sniff text encodings beyond what
// net/http.DetectContentType provides, and it performs no I/O in the table
// path.
//
// For hashing uploaded bytes see digest; for size-limited stream reads see
// iox; for filename canonicalization see sanitize.
//
//	t, ok := filetype.Detect(head)          // ok=false ⇒ genuinely unknown
//	t, r, err := filetype.DetectReader(src) // r replays the full stream
package filetype
