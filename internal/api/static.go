package api

import (
	"bytes"
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

// serveFile sends a file. Names under /assets/ carry a hash of their content
// and are cached for a year; everything else must be checked every time, or
// an update would not reach the browser.
func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, name string, data []byte) {
	h := w.Header()
	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
		h.Set("Content-Type", ct)
	}
	if strings.HasPrefix(name, "assets/") {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}
