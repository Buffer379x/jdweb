// Package state is the mirror of the two JDownloader lists in memory. Readers
// get consistent copies, event deltas change single fields, and the changes
// leave as bundled patches. The package does no I/O: the sync engine feeds it.
package state

import (
	"encoding/json"
	"slices"
	"strings"
	"sync"

	"github.com/Buffer379x/jdweb/internal/jd"
)

// maxMissing caps the remembered unknown ids. When it overflows they are
// forgotten, which costs at most one more reload per id.
const maxMissing = 1024

// linkKey is where a link sits in the list: reordering or moving it to another
// package is a structure change.
type linkKey struct{ id, pkg int64 }

// mirror is one list. P and L are the DTOs of its packages and links. The DTOs
// are stored as values in list order, which keeps a snapshot a single copy and
// a delta free of allocations for the entry.
type mirror[P, L any] struct {
	pkgs     []P
	links    []L
	pkgOrder []int64
	lnkOrder []linkKey
	pkgIdx   map[int64]int
	linkIdx  map[int64]int
	missing  map[int64]struct{} // ids that asked for a reload and are still unknown
	pend     pending
	stale    bool
}

func newMirror[P, L any]() *mirror[P, L] {
	return &mirror[P, L]{
		pkgIdx: map[int64]int{}, linkIdx: map[int64]int{}, missing: map[int64]struct{}{}, pend: newPending(),
	}
}

// replace swaps in a full load. Without a structure change the differences go
// into the pending patch. With one the browsers reload the list anyway, so what
// is pending is dropped.
func (m *mirror[P, L]) replace(pkgs []P, links []L, pkgOrder []int64, lnkOrder []linkKey, pkgFields, linkFields []fieldInfo) bool {
	structure := !slices.Equal(m.pkgOrder, pkgOrder) || !slices.Equal(m.lnkOrder, lnkOrder)
	if structure {
		m.pend.reset()
	} else {
		for i := range pkgs {
			s := sink{all: m.pend.packages, id: pkgOrder[i]}
			diff(pkgFields, &m.pkgs[i], &pkgs[i], &s)
		}
		for i := range links {
			s := sink{all: m.pend.links, id: lnkOrder[i].id}
			diff(linkFields, &m.links[i], &links[i], &s)
		}
	}
	m.pkgs, m.links, m.pkgOrder, m.lnkOrder = pkgs, links, pkgOrder, lnkOrder
	clear(m.pkgIdx)
	for i, id := range pkgOrder {
		m.pkgIdx[id] = i
	}
	clear(m.linkIdx)
	for i, k := range lnkOrder {
		m.linkIdx[k.id] = i
	}
	for id := range m.missing {
		_, isLink := m.linkIdx[id]
		_, isPkg := m.pkgIdx[id]
		if isLink || isPkg {
			delete(m.missing, id)
		}
	}
	m.stale = false
	return structure
}

// noteMissing reports whether an unknown id is news. Each id asks for one
// reload only: a phantom id must not keep the list reloading for ever.
func (m *mirror[P, L]) noteMissing(id int64) bool {
	if _, seen := m.missing[id]; seen {
		return false
	}
	if len(m.missing) >= maxMissing {
		clear(m.missing)
	}
	m.missing[id] = struct{}{}
	return true
}

func (m *mirror[P, L]) clear() {
	*m = *newMirror[P, L]()
}

// Store is the mirror of both lists. It is safe for concurrent use.
type Store struct {
	mu sync.RWMutex
	dl *mirror[DlPackage, DlLink]
	lg *mirror[LgPackage, LgLink]
}

// New returns an empty store.
func New() *Store {
	return &Store{dl: newMirror[DlPackage, DlLink](), lg: newMirror[LgPackage, LgLink]()}
}

// ListSnapshot is a consistent copy of one list. The fields of the other list
// stay nil. Stale says the values are from before the connection was lost.
type ListSnapshot struct {
	List       List
	Stale      bool
	DlPackages []DlPackage
	DlLinks    []DlLink
	LgPackages []LgPackage
	LgLinks    []LgLink
}

// MarshalJSON writes the answer of GET /downloads or GET /linkgrabber.
func (s ListSnapshot) MarshalJSON() ([]byte, error) {
	if s.List == Linkgrabber {
		return json.Marshal(struct {
			Packages []LgPackage `json:"packages"`
			Links    []LgLink    `json:"links"`
		}{s.LgPackages, s.LgLinks})
	}
	return json.Marshal(struct {
		Packages []DlPackage `json:"packages"`
		Links    []DlLink    `json:"links"`
	}{s.DlPackages, s.DlLinks})
}

func cloneOrEmpty[T any](s []T) []T {
	out := make([]T, len(s))
	copy(out, s)
	return out
}

// Snapshot returns a copy of a list. Strings and host lists are shared with the
// store, but the store never changes them in place.
func (s *Store) Snapshot(list List) ListSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if list == Linkgrabber {
		return ListSnapshot{List: list, Stale: s.lg.stale, LgPackages: cloneOrEmpty(s.lg.pkgs), LgLinks: cloneOrEmpty(s.lg.links)}
	}
	return ListSnapshot{List: Downloads, Stale: s.dl.stale, DlPackages: cloneOrEmpty(s.dl.pkgs), DlLinks: cloneOrEmpty(s.dl.links)}
}

// ReplaceDownloads swaps in a full load of the download list. It reports
// whether the structure changed: another set or order of ids, or a link in
// another package. Otherwise the differences arrive as a patch.
func (s *Store) ReplaceDownloads(pkgs []jd.DownloadPackage, links []jd.DownloadLink) bool {
	dp := make([]DlPackage, len(pkgs))
	po := make([]int64, len(pkgs))
	for i := range pkgs {
		dp[i], po[i] = newDlPackage(pkgs[i]), pkgs[i].UUID
	}
	dl := make([]DlLink, len(links))
	lo := make([]linkKey, len(links))
	for i := range links {
		dl[i], lo[i] = newDlLink(links[i]), linkKey{links[i].UUID, links[i].PackageUUID}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dl.replace(dp, dl, po, lo, dlPackageFields, dlLinkFields)
}

// ReplaceLinkgrabber is ReplaceDownloads for the linkgrabber.
func (s *Store) ReplaceLinkgrabber(pkgs []jd.GrabberPackage, links []jd.GrabberLink) bool {
	lp := make([]LgPackage, len(pkgs))
	po := make([]int64, len(pkgs))
	for i := range pkgs {
		lp[i], po[i] = newLgPackage(pkgs[i]), pkgs[i].UUID
	}
	ll := make([]LgLink, len(links))
	lo := make([]linkKey, len(links))
	for i := range links {
		ll[i], lo[i] = newLgLink(links[i]), linkKey{links[i].UUID, links[i].PackageUUID}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lg.replace(lp, ll, po, lo, lgPackageFields, lgLinkFields)
}

// Effect says what an event asks of the engine beyond the change in the mirror.
type Effect struct {
	Reload  List // this list has to be loaded again, debounced
	Poll    bool // the status is out of date, ask JDownloader soon
	Unknown bool // the event or a field of it means nothing to this version
}

// ApplyEvent applies one event of JDownloader. Whatever the event holds, it
// never fails: what cannot be understood is ignored and reported in Effect.
func (s *Store) ApplyEvent(ev jd.Event) Effect {
	switch ev.Publisher {
	case jd.PublisherDownloads:
		switch ev.EventID {
		case "ADD_CONTENT", "ADD_LINK", "ADD_PACKAGE", "REMOVE_CONTENT", "REMOVE_LINK", "REMOVE_PACKAGE", "REFRESH_STRUCTURE",
			// Password and directory changes announce themselves only this way
			// (recorded in revision 50639), and it does not fire during plain progress.
			"REFRESH_CONTENT":
			return Effect{Reload: Downloads}
		}
		return s.applyDelta(ev, Downloads)
	case jd.PublisherLinkCollector:
		switch ev.EventID {
		case "ABORT", "CONTENT_ADDED", "CONTENT_REMOVED", "DATA_REFRESH", "DUPE_ADDED", "LINK_ADDED", "LIST_LOADED", "STRUCTURE_REFRESH":
			return Effect{Reload: Linkgrabber}
		case "FILTERED_LINKS_AVAILABLE", "FILTERED_LINKS_EMPTY":
			return Effect{}
		}
		return s.applyDelta(ev, Linkgrabber)
	case jd.PublisherLinkCrawler:
		switch ev.EventID {
		case "STARTED", "STOPPED", "FINISHED":
			return Effect{Poll: true}
		}
	case jd.PublisherWatchdog:
		switch ev.EventID {
		case "RUNNING", "PAUSED", "STOPPED":
			return Effect{Poll: true}
		case "UPDATE":
			return Effect{}
		}
	case jd.PublisherCaptchas:
		switch ev.EventID {
		case "NEW", "DONE":
			return Effect{Poll: true}
		}
	case jd.PublisherDialogs:
		switch ev.EventID {
		case "NEW", "EXPIRED":
			return Effect{Poll: true}
		}
	}
	return Effect{Unknown: true}
}

func (s *Store) applyDelta(ev jd.Event, list List) Effect {
	kind, field, hasField := strings.Cut(ev.EventID, ".")
	isLink := kind == "LINK_UPDATE"
	if !isLink && kind != "PACKAGE_UPDATE" {
		return Effect{Unknown: true}
	}
	if field == "reset" {
		// A reset (recorded in revision 50639) changes several fields at once
		// without naming them: the list is loaded again.
		return Effect{Reload: list}
	}
	d, clean, ok := decodeDelta(ev.EventData)
	if !ok || d.UUID == nil {
		return Effect{Unknown: true}
	}
	var eff Effect
	switch {
	case !clean:
		// The named field may be the wrong one: that is no value, not a zero.
		eff.Unknown = true
	case hasField:
		if zero, ok := named[field]; ok {
			zero(d)
		} else if !notMirrored[field] {
			eff.Unknown = true
		}
	}
	id := *d.UUID

	s.mu.Lock()
	defer s.mu.Unlock()
	known := false
	if list == Downloads {
		known = applyTo(s.dl, id, isLink, func(l *DlLink, s *sink) { d.applyDlLink(l, s) }, func(p *DlPackage, s *sink) { d.applyDlPackage(p, s) })
		if !known && s.dl.noteMissing(id) {
			eff.Reload = list
		}
	} else {
		known = applyTo(s.lg, id, isLink, func(l *LgLink, s *sink) { d.applyLgLink(l, s) }, func(p *LgPackage, s *sink) { d.applyLgPackage(p, s) })
		if !known && s.lg.noteMissing(id) {
			eff.Reload = list
		}
	}
	return eff
}

// applyTo runs the matching apply function and reports whether the id is known.
func applyTo[P, L any](m *mirror[P, L], id int64, isLink bool, link func(*L, *sink), pkg func(*P, *sink)) bool {
	if isLink {
		i, ok := m.linkIdx[id]
		if ok {
			link(&m.links[i], &sink{all: m.pend.links, id: id})
		}
		return ok
	}
	i, ok := m.pkgIdx[id]
	if ok {
		pkg(&m.pkgs[i], &sink{all: m.pend.packages, id: id})
	}
	return ok
}

// Drain returns what changed since the last Drain, one patch per list with
// changes, the download list first.
func (s *Store) Drain() []Patch {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Patch
	if !s.dl.pend.empty() {
		out = append(out, s.dl.pend.patch(Downloads))
		s.dl.pend = newPending()
	}
	if !s.lg.pend.empty() {
		out = append(out, s.lg.pend.patch(Linkgrabber))
		s.lg.pend = newPending()
	}
	return out
}

// MarkStale records that the connection was lost. The last known values stay
// visible, but nothing may keep looking as if it were running: every link and
// package stops, with speed 0, no eta and without a status text like "Download"
// that only held while it ran. The next full load of a list makes it fresh
// again. The changes leave as normal patches.
func (s *Store) MarkStale() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.dl.links {
		l := &s.dl.links[i]
		sk := sink{all: s.dl.pend.links, id: s.dl.lnkOrder[i].id}
		if l.Running && !l.Finished {
			set(&sk, "status", &l.Status, "")
		}
		set(&sk, "running", &l.Running, false)
		settleDlLink(l, &sk)
	}
	for i := range s.dl.pkgs {
		p := &s.dl.pkgs[i]
		sk := sink{all: s.dl.pend.packages, id: s.dl.pkgOrder[i]}
		if p.Running && !p.Finished {
			set(&sk, "status", &p.Status, "")
		}
		set(&sk, "running", &p.Running, false)
		settleDlPackage(p, &sk)
	}
	s.dl.stale, s.lg.stale = true, true
}

// Stale reports whether a list still holds values from before a lost connection.
func (s *Store) Stale() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dl.stale || s.lg.stale
}

// Clear empties the store, for a change of the JDownloader address.
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dl.clear()
	s.lg.clear()
}

// Totals returns the sums over both lists. Crawling is not part of the mirror
// and stays false here. The eta is what is left to load of the links that will
// load (enabled, not finished, not skipped) at the current speed of all links.
func (s *Store) Totals() (DownloadTotals, GrabberTotals) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	dl := DownloadTotals{Packages: len(s.dl.pkgs), Links: len(s.dl.links), ETA: ETAUnknown}
	var speed, left int64
	for i := range s.dl.links {
		l := &s.dl.links[i]
		dl.BytesTotal += l.BytesTotal
		dl.BytesLoaded += l.BytesLoaded
		speed += l.Speed
		if l.Running {
			dl.Running++
		}
		if l.Enabled && !l.Finished && !l.Skipped && l.BytesTotal > l.BytesLoaded {
			left += l.BytesTotal - l.BytesLoaded
		}
	}
	if speed > 0 {
		dl.ETA = ETA(left / speed)
	}
	lg := GrabberTotals{Packages: len(s.lg.pkgs), Links: len(s.lg.links)}
	for i := range s.lg.links {
		lg.BytesTotal += s.lg.links[i].BytesTotal
	}
	return dl, lg
}
