// Package assets serves a fingerprinted static file tree from an fs.FS with the
// caching and correctness a production app needs: right content types, Range
// requests, ETag/304, and content-fingerprinted URLs that carry a far-future
// immutable Cache-Control header.
//
// One *Assets is one fs.FS mounted at one URL prefix (default "/static/"). It is
// an http.Handler and also resolves logical asset names to fingerprinted URLs
// (URL) and Subresource Integrity hashes (Integrity), so templates reference
// "app.css" and get "/static/app.a1b2c3d4.css".
//
// The fingerprint table is built once at New by one of three paths:
//
//   - a custom manifest Reader (WithReader), for a bundler forge does not read;
//   - a flat manifest.json in the fs.FS ({"app.css":"app.a1b2c3d4.css"} or
//     {"app.css":{"file":"…","integrity":"…"}}), emitted by a build tool;
//   - runtime fingerprinting — walk and hash the fs.FS at startup, no build step.
//
// A missing manifest.json falls back to runtime fingerprinting; a malformed one
// fails New with ErrManifest. WithDev(true) skips the table entirely: unhashed
// URLs, no-cache, and per-request re-reads so edits to an os.DirFS show live.
//
// Serving resolves each request under the prefix to a fingerprinted file
// (immutable), a plain file (no-cache, revalidated by ETag), an opportunistic
// precompressed sibling (WithPrecompressed, serving a build-emitted app.<h>.css.br
// when present and accepted), an SPA index (WithSPA), or 404.
//
// # Not a bundler
//
// assets never transpiles, minifies, tree-shakes, or resolves an import graph,
// and runtime mode does not rewrite intra-asset references (url(...) in CSS,
// import paths in JS). If assets reference each other by hashed name, use an
// external manifest whose bundler already rewrote them. Dynamic compression is
// web/compress; assets only serves precompressed siblings that already exist.
//
// # Usage
//
//	//go:embed static
//	var staticFS embed.FS
//
//	sub, _ := fs.Sub(staticFS, "static")
//	a, err := assets.New(sub, assets.WithSPA("index.html"))
//	if err != nil {
//		log.Fatal(err)
//	}
//	mux.Handle(a.Prefix(), a)
//	tpl.Funcs(a.FuncMap()) // {{ asset "app.css" }} / {{ sri "app.css" }}
package assets
