package render

import (
	"io/fs"
	"net/http"
)

// File serves a single local file by path via http.ServeFile, which handles Range
// requests, If-Modified-Since, and content-type sniffing. path is server-trusted — do
// NOT pass an unsanitized user-supplied path; use FileFS with a rooted fs.FS for
// user-influenced names. Status and error handling are owned by http.ServeFile.
func File(w http.ResponseWriter, r *http.Request, path string) {
	http.ServeFile(w, r, path)
}

// FileFS serves name from fsys via http.ServeFileFS — the safe, rooted form. Pass
// os.DirFS("/var/www") for a directory root, or an embed.FS for bundled assets; name
// resolution is constrained to fsys.
func FileFS(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) {
	http.ServeFileFS(w, r, fsys, name)
}
