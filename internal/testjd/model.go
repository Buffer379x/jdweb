package testjd

import (
	"sort"
	"sync/atomic"
)

// List names one of the two lists of JDownloader.
type List int

const (
	Downloads   List = iota // the download list (downloadsV2)
	Linkgrabber             // the linkgrabber (linkgrabberv2)
)

func (l List) String() string {
	if l == Downloads {
		return "downloads"
	}
	return "linkgrabber"
}

// Package is a package in one of the lists. The zero value is an enabled
// package with default priority; counts, sizes and speed are derived from its links.
type Package struct {
	UUID     int64 // assigned when 0
	Name     string
	SaveTo   string
	Comment  string
	Password string
	Priority string // "" means DEFAULT and is not reported
	Disabled bool
}

// Link is a link in one of the lists. The zero value is an enabled, online link.
// Like JDownloader the fake leaves out false, zero and empty fields in its JSON.
type Link struct {
	UUID             int64 // assigned when 0
	PackageUUID      int64 // set by AddPackage / AddLink
	Name             string
	URL              string
	Host             string // "http links" when empty
	BytesTotal       int64
	BytesLoaded      int64
	Speed            int64
	ETA              int64
	Disabled         bool
	Finished         bool
	Running          bool
	Skipped          bool
	SkipReason       string // for example DISK_FULL, reported in advancedStatus
	Availability     string // linkgrabber: ONLINE (default), OFFLINE, UNKNOWN, TEMP_UNKNOWN
	Priority         string
	Comment          string
	Password         string
	Status           string
	ExtractionStatus string
	AddedDate        int64
	FinishedDate     int64
}

type pkg struct {
	Package
	links []*Link
}

type listState struct {
	pkgs    []*pkg
	counter int64 // structure change counter
}

// idBase makes ids look like the ones of JDownloader: milliseconds since 1970 plus a counter.
const idBase = 1793562910000

func (s *Server) nextID() int64 { return atomic.AddInt64(&s.idSeq, 1) + idBase }

func (s *Server) list(l List) *listState { return &s.lists[l] }

// bump records a structure change of a list.
func (ls *listState) bump(s *Server) { ls.counter = s.nextID() }

func (ls *listState) findPkg(id int64) (int, *pkg) {
	for i, p := range ls.pkgs {
		if p.UUID == id {
			return i, p
		}
	}
	return -1, nil
}

func (ls *listState) findLink(id int64) (*pkg, int, *Link) {
	for _, p := range ls.pkgs {
		for i, l := range p.links {
			if l.UUID == id {
				return p, i, l
			}
		}
	}
	return nil, -1, nil
}

// AddPackage appends a package with its links to a list and returns the ids.
// It changes the list like JDownloader does by itself: the structure counter
// moves, but no event is sent. Call Emit for that.
func (s *Server) AddPackage(l List, p Package, links ...Link) (pkgID int64, linkIDs []int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addPackageLocked(l, p, links)
}

func (s *Server) addPackageLocked(l List, p Package, links []Link) (int64, []int64) {
	if p.UUID == 0 {
		p.UUID = s.nextID()
	}
	np := &pkg{Package: p}
	var out []int64
	for _, lk := range links {
		lk := lk
		s.fillLink(&lk)
		lk.PackageUUID = p.UUID
		np.links = append(np.links, &lk)
		out = append(out, lk.UUID)
	}
	ls := s.list(l)
	ls.pkgs = append(ls.pkgs, np)
	ls.bump(s)
	return p.UUID, out
}

func (s *Server) fillLink(l *Link) {
	if l.UUID == 0 {
		l.UUID = s.nextID()
	}
	if l.Host == "" {
		l.Host = "http links"
	}
	if l.AddedDate == 0 {
		l.AddedDate = s.nextID()
	}
}

// AddLink appends a link to an existing package and returns its id, or 0 if the package does not exist.
func (s *Server) AddLink(l List, pkgID int64, lk Link) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	ls := s.list(l)
	_, p := ls.findPkg(pkgID)
	if p == nil {
		return 0
	}
	s.fillLink(&lk)
	lk.PackageUUID = pkgID
	p.links = append(p.links, &lk)
	ls.bump(s)
	return lk.UUID
}

// UpdateLink changes a link as JDownloader does while downloading. It reports
// whether the link exists. The structure counter does not move.
func (s *Server) UpdateLink(l List, id int64, fn func(*Link)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _, lk := s.list(l).findLink(id)
	if lk == nil {
		return false
	}
	pkgID := lk.PackageUUID
	fn(lk)
	lk.UUID, lk.PackageUUID = id, pkgID
	return true
}

// UpdatePackage changes a package. It reports whether the package exists.
func (s *Server) UpdatePackage(l List, id int64, fn func(*Package)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, p := s.list(l).findPkg(id)
	if p == nil {
		return false
	}
	fn(&p.Package)
	p.UUID = id
	return true
}

// RemoveLink removes a link (and its package when empty) like a removal in
// the UI of JDownloader. The structure counter moves, no event is sent.
func (s *Server) RemoveLink(l List, id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.removeLinkLocked(l, id)
}

func (s *Server) removeLinkLocked(l List, id int64) bool {
	ls := s.list(l)
	p, i, _ := ls.findLink(id)
	if p == nil {
		return false
	}
	p.links = append(p.links[:i], p.links[i+1:]...)
	if len(p.links) == 0 {
		pi, _ := ls.findPkg(p.UUID)
		ls.pkgs = append(ls.pkgs[:pi], ls.pkgs[pi+1:]...)
	}
	ls.bump(s)
	return true
}

// Packages returns a copy of the packages of a list, in list order.
func (s *Server) Packages(l List) []Package {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Package
	for _, p := range s.list(l).pkgs {
		out = append(out, p.Package)
	}
	return out
}

// Links returns a copy of the links of a list, in list order.
func (s *Server) Links(l List) []Link {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Link
	for _, p := range s.list(l).pkgs {
		for _, lk := range p.links {
			out = append(out, *lk)
		}
	}
	return out
}

// Reset empties both lists, the captchas and the dialogs, and forgets the
// call log and the pending failures. Subscriptions and the controller stay.
func (s *Server) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for l := range s.lists {
		s.lists[l].pkgs = nil
		s.lists[l].bump(s)
	}
	s.captchas, s.dialogs = nil, nil
	s.calls, s.failures = nil, nil
}

// derived values of a package from its links.
type pkgSums struct {
	total, loaded, speed                  int64
	running, finished                     bool
	hosts                                 []string
	online, offline, unknown, tempUnknown int
}

func (p *pkg) sums() pkgSums {
	var s pkgSums
	seen := map[string]bool{}
	s.finished = len(p.links) > 0
	for _, l := range p.links {
		s.total += l.BytesTotal
		s.loaded += l.BytesLoaded
		s.speed += l.Speed
		s.running = s.running || l.Running
		s.finished = s.finished && l.Finished
		if !seen[l.Host] {
			seen[l.Host] = true
			s.hosts = append(s.hosts, l.Host)
		}
		switch l.Availability {
		case "OFFLINE":
			s.offline++
		case "UNKNOWN":
			s.unknown++
		case "TEMP_UNKNOWN":
			s.tempUnknown++
		default:
			s.online++
		}
	}
	sort.Strings(s.hosts)
	return s
}
