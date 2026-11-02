package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

// noWebApp is what a build without the web app answers.
const noWebApp = "JDWeb: the web interface is not part of this build (web/dist/index.html is missing).\n" +
	"The API is available under /api/v1.\n"

// handleFallback serves everything that is not an API route: the files of the
// web app, and index.html for the paths the app routes itself. Unknown API
// paths are a JSON 404, never the app.
func (s *Server) handleFallback(w http.ResponseWriter, r *http.Request) {
	if isAPI(r.URL.Path) {
		writeError(w, notFound("There is no such API route."))
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
		return
	}
	if s.static == nil {
		s.plain(w, http.StatusNotFound, noWebApp)
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" {
		name = "index.html"
	}
	// Dotfiles and dot folders (.vite/, .gitkeep, .env) are never served.
	if hasDotSegment(name) {
		s.plain(w, http.StatusNotFound, "Not found.\n")
		return
	}
	if data, ok := s.readFile(name); ok {
		s.serveFile(w, r, name, data)
		return
	}
	// Files that do not exist are 404, the app only takes over page paths:
	// an asset that is missing must not turn into HTML.
	if name != "index.html" && (name == "assets" || strings.HasPrefix(name, "assets/") || strings.Contains(path.Base(name), ".")) {
		s.plain(w, http.StatusNotFound, "Not found.\n")
		return
	}
	if data, ok := s.readFile("index.html"); ok {
		s.serveFile(w, r, "index.html", data)
		return
	}
	s.plain(w, http.StatusNotFound, noWebApp)
}

func (s *Server) plain(w http.ResponseWriter, status int, text string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, text)
}

// readFile returns a regular file of the app.
func (s *Server) readFile(name string) ([]byte, bool) {
	f, err := s.static.Open(name)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		return nil, false
	}
	data, err := io.ReadAll(io.LimitReader(f, 32<<20))
	if err != nil {
		return nil, false
	}
	return data, true
}

func hasDotSegment(name string) bool {
	for _, seg := range strings.Split(name, "/") {
		if strings.HasPrefix(seg, ".") {
			return true
		}
	}
	return false
}

// contentTypes are set by hand: the table of the operating system may lack
// them or differ (a scratch image has none).
var contentTypes = map[string]string{
	".html": "text/html; charset=utf-8", ".js": "text/javascript; charset=utf-8", ".mjs": "text/javascript; charset=utf-8",
	".css": "text/css; charset=utf-8", ".svg": "image/svg+xml", ".webmanifest": "application/manifest+json",
	".woff2": "font/woff2", ".json": "application/json", ".png": "image/png", ".ico": "image/x-icon",
	".txt": "text/plain; charset=utf-8", ".map": "application/json",
}

func contentType(name string) string {
	ext := strings.ToLower(path.Ext(name))
	if ct, ok := contentTypes[ext]; ok {
		return ct
	}
	return mime.TypeByExtension(ext)
}

// serveFile sends a file. Names under /assets/ carry a hash of their content
// and are cached for a year; everything else must be checked every time, or
// an update would not reach the browser.
func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, name string, data []byte) {
	h := w.Header()
	if ct := contentType(name); ct != "" {
		h.Set("Content-Type", ct)
	}
	if strings.HasPrefix(name, "assets/") {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		// The hash of the content is the ETag: an unchanged index.html costs a 304.
		sum := sha256.Sum256(data)
		h.Set("ETag", `"`+hex.EncodeToString(sum[:12])+`"`)
		h.Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}
