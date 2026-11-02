package api

import (
	"container/list"
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/Buffer379x/jdweb/internal/jd"
)

// The icon cache holds at most iconCacheEntries entries and iconCacheBytes
// bytes. Hoster and file icons are 16x16 pictures of a few hundred bytes.
const (
	iconCacheEntries = 512
	iconCacheBytes   = 32 << 20
)

// maxIconBytes is the largest icon kept in the cache and passed on.
const maxIconBytes = 256 << 10

// iconFailureTTL is how long a failed lookup is remembered, so that a hostile
// name asked for again and again does not hit JDownloader each time.
const iconFailureTTL = 5 * time.Minute

// iconTypes are the picture types passed on. Anything else (SVG, HTML, ...) is
// refused, whatever JDownloader says it is.
var iconTypes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true,
	"image/x-icon": true, "image/vnd.microsoft.icon": true,
}

// iconType returns the media type without parameters, lower case, if allowed.
func iconType(contentType string) (string, bool) {
	mt, _, _ := strings.Cut(contentType, ";")
	mt = strings.ToLower(strings.TrimSpace(mt))
	return mt, iconTypes[mt]
}

type icon struct {
	data        []byte
	contentType string
	failure     *apiError // set for a remembered failure
	until       time.Time // when a remembered failure ends
}

func (i icon) size() int64 { return int64(len(i.data)) + int64(len(i.contentType)) + 64 }

type iconCache struct {
	mu       sync.Mutex
	max      int
	maxBytes int64
	bytes    int64
	now      func() time.Time
	order    *list.List // front = most recently used
	items    map[string]*list.Element
}

type iconEntry struct {
	key string
	icon
}

func newIconCache(max int, maxBytes int64) *iconCache {
	return &iconCache{max: max, maxBytes: maxBytes, now: time.Now, order: list.New(), items: map[string]*list.Element{}}
}

func (c *iconCache) get(key string) (icon, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return icon{}, false
	}
	e := el.Value.(*iconEntry)
	if e.failure != nil && !c.now().Before(e.until) {
		c.remove(el)
		return icon{}, false
	}
	c.order.MoveToFront(el)
	return e.icon, true
}

func (c *iconCache) remove(el *list.Element) {
	e := el.Value.(*iconEntry)
	c.order.Remove(el)
	delete(c.items, e.key)
	c.bytes -= e.size()
}

func (c *iconCache) put(key string, ic icon) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.remove(el)
	}
	c.items[key] = c.order.PushFront(&iconEntry{key, ic})
	c.bytes += ic.size()
	for c.order.Len() > c.max || c.bytes > c.maxBytes {
		c.remove(c.order.Back())
	}
}

func (c *iconCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}

func (c *iconCache) size() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bytes
}

// handleIcon proxies an icon of JDownloader through the cache. The browser
// never talks to JDownloader.
func (s *Server) handleIcon(kind string, fetch func(*jd.Client, context.Context, string) ([]byte, string, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" || len(name) > 255 || strings.ContainsFunc(name, unicode.IsControl) {
			s.fail(w, r, badRequest("The icon name is empty, too long or holds control characters."))
			return
		}
		key := kind + ":" + name
		ic, ok := s.icons.get(key)
		if !ok {
			ic = s.loadIcon(r, fetch, name)
			// Offline and internal failures are transient: not remembered.
			if ic.failure == nil || !ic.until.IsZero() {
				s.icons.put(key, ic)
			}
		}
		if ic.failure != nil {
			writeError(w, ic.failure)
			return
		}
		h := w.Header()
		h.Set("Content-Type", ic.contentType)
		// private: the icon is behind the session, a shared cache must not keep it.
		h.Set("Cache-Control", "private, max-age=86400")
		_, _ = w.Write(ic.data)
	}
}

// loadIcon asks JDownloader. A failure that says something about this name
// (not about the connection) is remembered for a while.
func (s *Server) loadIcon(r *http.Request, fetch func(*jd.Client, context.Context, string) ([]byte, string, error), name string) icon {
	c, err := s.client()
	var data []byte
	var ct string
	if err == nil {
		data, ct, err = fetch(c, r.Context(), name)
	}
	if err != nil {
		ae, _ := toAPIError(err)
		if ae.Code == codeJDOffline || ae.Code == codeInternal {
			return icon{failure: ae} // no `until`: not remembered
		}
		return icon{failure: ae, until: s.icons.now().Add(iconFailureTTL)}
	}
	mt, ok := iconType(ct)
	switch {
	case !ok:
		return icon{failure: newError(http.StatusBadGateway, codeJDError, "JDownloader sent an icon of a type that is not passed on."),
			until: s.icons.now().Add(iconFailureTTL)}
	case len(data) > maxIconBytes:
		return icon{failure: newError(http.StatusBadGateway, codeJDError, "JDownloader sent an icon that is too large."),
			until: s.icons.now().Add(iconFailureTTL)}
	}
	return icon{data: data, contentType: mt}
}

func hosterIcon(c *jd.Client, ctx context.Context, name string) ([]byte, string, error) {
	return c.HosterIcon(ctx, name)
}

func fileIcon(c *jd.Client, ctx context.Context, name string) ([]byte, string, error) {
	return c.FileIcon(ctx, name)
}
