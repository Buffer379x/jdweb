package api

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// writeJSON sends v with the status. A value that cannot be encoded is a bug
// and becomes a 500 before anything was sent.
func (s *Server) writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(b, '\n'))
}

// noContent answers a success without a payload.
func noContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }

var gzipPool = sync.Pool{New: func() any {
	zw, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed)
	return zw
}}

// acceptsGzip reports whether the client takes gzip.
func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(name), "gzip") {
			continue
		}
		// "gzip;q=0" refuses it.
		if q, ok := strings.CutPrefix(strings.ReplaceAll(strings.TrimSpace(params), " ", ""), "q="); ok {
			if f, err := strconv.ParseFloat(q, 64); err == nil && f == 0 {
				return false
			}
		}
		return true
	}
	return false
}

// writeJSONGzip is writeJSON for the large lists: compressed when the client
// accepts it. It is not used for server-sent events, which must not be buffered.
func (s *Server) writeJSONGzip(w http.ResponseWriter, r *http.Request, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	b = append(b, '\n')
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Add("Vary", "Accept-Encoding")
	if !acceptsGzip(r) {
		_, _ = w.Write(b)
		return
	}
	var buf bytes.Buffer
	zw := gzipPool.Get().(*gzip.Writer)
	zw.Reset(&buf)
	_, _ = zw.Write(b)
	_ = zw.Close()
	gzipPool.Put(zw)
	h.Set("Content-Encoding", "gzip")
	h.Set("Content-Length", strconv.Itoa(buf.Len()))
	_, _ = w.Write(buf.Bytes())
}

// decode reads the JSON body into v. An empty body leaves v alone when
// optional is set. Unknown fields are ignored.
func decode(r *http.Request, v any, optional bool) error {
	dec := json.NewDecoder(r.Body)
	err := dec.Decode(v)
	switch {
	case err == nil:
		if _, err := dec.Token(); !errors.Is(err, io.EOF) {
			if err == nil {
				return badRequest("The request body must hold exactly one JSON value.")
			}
			return bodyError(err)
		}
		return nil
	case errors.Is(err, io.EOF):
		if optional {
			return nil
		}
		return badRequest("The request body is empty.")
	}
	return bodyError(err)
}

func bodyError(err error) error {
	var mb *http.MaxBytesError
	if errors.As(err, &mb) {
		return err
	}
	var te *json.UnmarshalTypeError
	if errors.As(err, &te) && te.Field != "" {
		return badRequest("The field %q has the wrong type.", te.Field)
	}
	var ae *apiError
	if errors.As(err, &ae) {
		return ae
	}
	return badRequest("The request body is not valid JSON.")
}

// ID is a JDownloader id. It is a string in JSON, because JavaScript would
// round a 64-bit number.
type ID int64

// UnmarshalJSON reads a string of digits. Numbers are refused on purpose: a
// number may already have been rounded by the sender.
func (id *ID) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return badRequest("Ids must be strings of digits.")
	}
	n, err := parseID(s)
	if err != nil {
		return err
	}
	*id = ID(n)
	return nil
}

// maxIDs bounds the ids of one request.
const maxIDs = 200_000

func parseID(s string) (int64, error) {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 || s != strconv.FormatInt(n, 10) {
		return 0, badRequest("%q is not a valid id.", clip(s, 24))
	}
	return n, nil
}

// parseOptionalID reads an id that may be absent.
func parseOptionalID(p *ID) int64 {
	if p == nil {
		return 0
	}
	return int64(*p)
}

// ints turns ids into the numbers the client wants.
func ints(ids []ID) []int64 {
	out := make([]int64, len(ids))
	for i, id := range ids {
		out[i] = int64(id)
	}
	return out
}

// parseAfter reads an id that names the place behind which something goes. Absent,
// empty and "-1" mean the top of the list.
func parseAfter(s *string) (int64, error) {
	if s == nil || *s == "" || *s == "-1" {
		return -1, nil
	}
	return parseID(*s)
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n] + "..."
}

func idString(n int64) string { return strconv.FormatInt(n, 10) }
