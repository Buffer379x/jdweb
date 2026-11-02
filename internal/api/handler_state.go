package api

import (
	"net/http"

	"github.com/Buffer379x/jdweb/internal/state"
)

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, r, http.StatusOK, s.engine.Status())
}

func (s *Server) handleDownloads(w http.ResponseWriter, r *http.Request) {
	s.writeJSONGzip(w, r, s.mirror.Snapshot(state.Downloads))
}

func (s *Server) handleLinkgrabber(w http.ResponseWriter, r *http.Request) {
	s.writeJSONGzip(w, r, s.mirror.Snapshot(state.Linkgrabber))
}
