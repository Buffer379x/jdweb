package testjd

import (
	"context"
	"encoding/json"
	"fmt"
)

// apiErr is an error object of JDownloader.
type apiErr struct {
	status int
	typ    string
	data   any
}

func badParams(data any) *apiErr { return &apiErr{400, "BAD_PARAMETERS", data} }

// handler answers one JDownloader path. Unless blocking is set it runs with s.mu held.
type handler struct {
	fn       func(ctx context.Context, s *Server, a args) (any, *apiErr)
	blocking bool
}

var handlers = map[string]handler{}

func register(path string, fn func(ctx context.Context, s *Server, a args) (any, *apiErr)) {
	handlers[path] = handler{fn: fn}
}

// args are the params of a call.
type args []json.RawMessage

// scan decodes the params into the given pointers, strictly: a missing param
// or one of the wrong type is BAD_PARAMETERS, like in JDownloader.
func (a args) scan(ptrs ...any) *apiErr {
	if len(a) < len(ptrs) {
		return badParams(fmt.Sprintf("expected %d parameters, got %d", len(ptrs), len(a)))
	}
	for i, p := range ptrs {
		if err := json.Unmarshal(a[i], p); err != nil {
			return badParams(string(a[i]))
		}
	}
	return nil
}

// query is the object of a queryLinks or queryPackages call.
type query struct {
	flags      map[string]bool
	packageIDs map[int64]bool
	linkIDs    map[int64]bool
	maxResults int
	startAt    int
}

func (a args) query() (query, *apiErr) {
	q := query{maxResults: -1}
	var raw map[string]json.RawMessage
	if err := a.scan(&raw); err != nil {
		return q, err
	}
	q.flags = map[string]bool{}
	for k, v := range raw {
		var b bool
		if json.Unmarshal(v, &b) == nil {
			q.flags[k] = b
		}
	}
	for k, dst := range map[string]*map[int64]bool{"packageUUIDs": &q.packageIDs, "linkUUIDs": &q.linkIDs} {
		if v, ok := raw[k]; ok {
			var l []int64
			if err := json.Unmarshal(v, &l); err != nil {
				return q, badParams(string(mustMarshal(raw)))
			}
			if len(l) > 0 {
				*dst = map[int64]bool{}
				for _, id := range l {
					(*dst)[id] = true
				}
			}
		}
	}
	for k, dst := range map[string]*int{"maxResults": &q.maxResults, "startAt": &q.startAt} {
		if v, ok := raw[k]; ok {
			if err := json.Unmarshal(v, dst); err != nil {
				return q, badParams(string(mustMarshal(raw)))
			}
		}
	}
	return q, nil
}

// page applies startAt and maxResults to n entries and returns the range.
func (q query) page(n int) (from, to int) {
	from = q.startAt
	if from < 0 {
		from = 0
	}
	if from > n {
		from = n
	}
	to = n
	if q.maxResults >= 0 && from+q.maxResults < n {
		to = from + q.maxResults
	}
	return from, to
}

func idSet(ids ...[]int64) map[int64]bool {
	m := map[int64]bool{}
	for _, l := range ids {
		for _, id := range l {
			m[id] = true
		}
	}
	return m
}
