package api

import (
	"container/list"
	"context"
	"net/http"
	"strings"
	"sync"
	"unicode"

	"github.com/Buffer379x/jdweb/internal/jd"
)

// iconCacheEntries is the size of the icon cache. Hoster and file icons are
// 16x16 pictures of a few hundred bytes.
const iconCacheEntries = 512

// maxIconBytes is the largest icon kept in the cache and passed on.
const maxIconBytes = 256 << 10

type icon struct {
	data        []byte
	contentType string
}

type iconCache struct {
	mu    sync.Mutex
	max   int
	order *list.List // front = most recently used
	items map[string]*list.Element
}

type iconEntry struct {
	key string
	icon
}

func newIconCache(max int) *iconCache {
	return &iconCache{max: max, order: list.New(), items: map[string]*list.Element{}}
}

func (c *iconCache) get(key string) (icon, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return icon{}, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*iconEntry).icon, true
}

func (c *iconCache) put(key string, ic icon) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		el.Value.(*iconEntry).icon = ic
		c.order.MoveToFront(el)
		return
	}
	c.items[key] = c.order.PushFront(&iconEntry{key, ic})
	for c.order.Len() > c.max {
		last := c.order.Back()
		c.order.Remove(last)
		delete(c.items, last.Value.(*iconEntry).key)
	}
}

func (c *iconCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
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
			c, err := s.client()
			if err == nil {
				ic.data, ic.contentType, err = fetch(c, r.Context(), name)
			}
			if err != nil {
				s.fail(w, r, err)
				return
			}
			if len(ic.data) > maxIconBytes {
				s.fail(w, r, newError(http.StatusBadGateway, codeJDError, "JDownloader sent an icon that is too large."))
				return
			}
			s.icons.put(key, ic)
		}
		h := w.Header()
		h.Set("Content-Type", ic.contentType)
		h.Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write(ic.data)
	}
}

func hosterIcon(c *jd.Client, ctx context.Context, name string) ([]byte, string, error) {
	return c.HosterIcon(ctx, name)
}

func fileIcon(c *jd.Client, ctx context.Context, name string) ([]byte, string, error) {
	return c.FileIcon(ctx, name)
}
