package testjd

import (
	"context"
	"net/url"
	"path"
	"strings"
)

func init() {
	for _, ns := range []struct {
		prefix string
		list   List
	}{{"/downloadsV2", Downloads}, {"/linkgrabberv2", Linkgrabber}} {
		registerListHandlers(ns.prefix, ns.list)
	}

	register("/downloadsV2/getStructureChangeCounter", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		return s.structureCounter(Downloads, a)
	})
	register("/linkgrabberv2/getChildrenChanged", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		return s.structureCounter(Linkgrabber, a)
	})
	register("/downloadsV2/packageCount", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		return len(s.list(Downloads).pkgs), nil
	})
	register("/linkgrabberv2/getPackageCount", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		return len(s.list(Linkgrabber).pkgs), nil
	})

	// Download list only.
	linkOp := func(name string, fn func(*Link)) {
		register("/downloadsV2/"+name, func(_ context.Context, s *Server, a args) (any, *apiErr) {
			var linkIDs, pkgIDs []int64
			if err := a.scan(&linkIDs, &pkgIDs); err != nil {
				return nil, err
			}
			s.eachLink(Downloads, linkIDs, pkgIDs, fn)
			return "", nil
		})
	}
	linkOp("forceDownload", func(l *Link) { l.Running, l.Skipped = true, false })
	linkOp("resumeLinks", func(l *Link) { l.Skipped, l.SkipReason = false, "" })
	linkOp("resetLinks", func(l *Link) {
		l.BytesLoaded, l.Finished, l.Running, l.Speed, l.ExtractionStatus = 0, false, false, 0, ""
	})
	register("/downloadsV2/unskip", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var pkgIDs, linkIDs []int64
		var reason *string
		if err := a.scan(&pkgIDs, &linkIDs, &reason); err != nil {
			return nil, err
		}
		s.eachLink(Downloads, linkIDs, pkgIDs, func(l *Link) {
			if reason == nil || *reason == l.SkipReason {
				l.Skipped, l.SkipReason = false, ""
			}
		})
		return true, nil
	})
	register("/downloadsV2/setStopMark", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var linkID, pkgID int64
		if err := a.scan(&linkID, &pkgID); err != nil {
			return nil, err
		}
		s.stopMark = linkID
		if linkID == -1 {
			s.stopMark = pkgID
		}
		return "", nil
	})
	register("/downloadsV2/removeStopMark", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		s.stopMark = -1
		return "", nil
	})
	register("/downloadsV2/getStopMark", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		return s.stopMark, nil
	})
	register("/downloadsV2/getStopMarkedLink", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		if _, _, lk := s.list(Downloads).findLink(s.stopMark); lk != nil {
			return s.renderLink(Downloads, lk, query{}), nil
		}
		return nil, nil
	})

	// Linkgrabber only.
	register("/linkgrabberv2/addLinks", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var q struct {
			Links             string `json:"links"`
			PackageName       string `json:"packageName"`
			DestinationFolder string `json:"destinationFolder"`
			Priority          string `json:"priority"`
			Comment           string `json:"comment"`
			DownloadPassword  string `json:"downloadPassword"`
			Autostart         bool   `json:"autostart"`
		}
		if err := a.scan(&q); err != nil {
			return nil, err
		}
		return s.addLinks(strings.Fields(q.Links), q.PackageName, q.DestinationFolder, q.Priority, q.Comment, q.DownloadPassword, q.Autostart), nil
	})
	register("/linkgrabberv2/addContainer", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var typ, content string
		if err := a.scan(&typ, &content); err != nil {
			return nil, err
		}
		links, ok := decodeContainer(content)
		if !ok {
			return nil, &apiErr{500, "INTERNAL_SERVER_ERROR", nil}
		}
		return s.addLinks(links, "", "", "", "", "", false), nil
	})
	register("/linkgrabberv2/moveToDownloadlist", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var linkIDs, pkgIDs []int64
		if err := a.scan(&linkIDs, &pkgIDs); err != nil {
			return nil, err
		}
		s.moveToDownloads(linkIDs, pkgIDs)
		return "", nil
	})
	register("/linkgrabberv2/clearList", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		ls := s.list(Linkgrabber)
		ls.pkgs = nil
		ls.bump(s)
		s.structureEvents(Linkgrabber)
		return true, nil
	})
	register("/linkgrabberv2/abort", func(_ context.Context, s *Server, a args) (any, *apiErr) { return true, nil })
	register("/linkgrabberv2/isCollecting", func(_ context.Context, s *Server, a args) (any, *apiErr) { return false, nil })
	register("/linkcrawler/isCrawling", func(_ context.Context, s *Server, a args) (any, *apiErr) { return false, nil })
	register("/linkgrabberv2/queryLinkCrawlerJobs", func(_ context.Context, s *Server, a args) (any, *apiErr) { return []any{}, nil })
	register("/linkgrabberv2/getVariants", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var id int64
		if err := a.scan(&id); err != nil {
			return nil, err
		}
		if _, _, lk := s.list(Linkgrabber).findLink(id); lk != nil && len(lk.Variants) > 0 {
			return lk.Variants, nil
		}
		return []any{}, nil
	})
	register("/linkgrabberv2/getDownloadFolderHistorySelectionBase", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		seen := map[string]bool{}
		out := []string{}
		for _, ls := range s.lists {
			for _, p := range ls.pkgs {
				if d := path.Dir(p.SaveTo); p.SaveTo != "" && !seen[d] {
					seen[d] = true
					out = append(out, d)
				}
			}
		}
		return out, nil
	})
}

func (s *Server) structureCounter(l List, a args) (any, *apiErr) {
	var last int64
	if err := a.scan(&last); err != nil {
		return nil, err
	}
	if c := s.list(l).counter; c != last {
		return c, nil
	}
	return -1, nil
}

func registerListHandlers(prefix string, l List) {
	reg := func(name string, fn func(s *Server, a args) (any, *apiErr)) {
		register(prefix+"/"+name, func(_ context.Context, s *Server, a args) (any, *apiErr) { return fn(s, a) })
	}
	reg("queryPackages", func(s *Server, a args) (any, *apiErr) {
		q, err := a.query()
		if err != nil {
			return nil, err
		}
		var sel []*pkg
		for _, p := range s.list(l).pkgs {
			if q.packageIDs == nil || q.packageIDs[p.UUID] {
				sel = append(sel, p)
			}
		}
		from, to := q.page(len(sel))
		out := []any{}
		for _, p := range sel[from:to] {
			out = append(out, s.renderPackage(l, p, q))
		}
		return out, nil
	})
	reg("queryLinks", func(s *Server, a args) (any, *apiErr) {
		q, err := a.query()
		if err != nil {
			return nil, err
		}
		var sel []*Link
		for _, p := range s.list(l).pkgs {
			if q.packageIDs != nil && !q.packageIDs[p.UUID] {
				continue
			}
			for _, lk := range p.links {
				if q.linkIDs == nil || q.linkIDs[lk.UUID] {
					sel = append(sel, lk)
				}
			}
		}
		from, to := q.page(len(sel))
		out := []any{}
		for _, lk := range sel[from:to] {
			out = append(out, s.renderLink(l, lk, q))
		}
		return out, nil
	})
	reg("removeLinks", func(s *Server, a args) (any, *apiErr) {
		var linkIDs, pkgIDs []int64
		if err := a.scan(&linkIDs, &pkgIDs); err != nil {
			return nil, err
		}
		s.removeItems(l, linkIDs, pkgIDs)
		return "", nil
	})
	reg("setEnabled", func(s *Server, a args) (any, *apiErr) {
		var enabled bool
		var linkIDs, pkgIDs []int64
		if err := a.scan(&enabled, &linkIDs, &pkgIDs); err != nil {
			return nil, err
		}
		s.eachLink(l, linkIDs, pkgIDs, func(lk *Link) { lk.Disabled = !enabled })
		for _, id := range pkgIDs {
			if _, p := s.list(l).findPkg(id); p != nil {
				p.Disabled = !enabled
			}
		}
		return "", nil
	})
	reg("setPriority", func(s *Server, a args) (any, *apiErr) {
		var prio string
		var linkIDs, pkgIDs []int64
		if err := a.scan(&prio, &linkIDs, &pkgIDs); err != nil {
			return nil, err
		}
		if !validPriority[prio] {
			return nil, badParams(string(mustMarshal(prio)))
		}
		if prio == "DEFAULT" {
			prio = ""
		}
		s.eachLink(l, linkIDs, pkgIDs, func(lk *Link) { lk.Priority = prio })
		for _, id := range pkgIDs {
			if _, p := s.list(l).findPkg(id); p != nil {
				p.Priority = prio
			}
		}
		return "", nil
	})
	reg("renameLink", func(s *Server, a args) (any, *apiErr) {
		var id int64
		var name string
		if err := a.scan(&id, &name); err != nil {
			return nil, err
		}
		if _, _, lk := s.list(l).findLink(id); lk != nil {
			lk.Name = name
		}
		return "", nil
	})
	reg("renamePackage", func(s *Server, a args) (any, *apiErr) {
		var id int64
		var name string
		if err := a.scan(&id, &name); err != nil {
			return nil, err
		}
		if _, p := s.list(l).findPkg(id); p != nil {
			p.Name = name
		}
		return "", nil
	})
	reg("setDownloadDirectory", func(s *Server, a args) (any, *apiErr) {
		var dir string
		var pkgIDs []int64
		if err := a.scan(&dir, &pkgIDs); err != nil {
			return nil, err
		}
		for _, id := range pkgIDs {
			if _, p := s.list(l).findPkg(id); p != nil {
				p.SaveTo = dir
			}
		}
		return "", nil
	})
	reg("setDownloadPassword", func(s *Server, a args) (any, *apiErr) {
		var linkIDs, pkgIDs []int64
		var pass string
		if err := a.scan(&linkIDs, &pkgIDs, &pass); err != nil {
			return nil, err
		}
		s.eachLink(l, linkIDs, pkgIDs, func(lk *Link) { lk.Password = pass })
		for _, id := range pkgIDs {
			if _, p := s.list(l).findPkg(id); p != nil {
				p.Password = pass
			}
		}
		return true, nil
	})
	reg("setComment", func(s *Server, a args) (any, *apiErr) {
		var linkIDs, pkgIDs []int64
		var all bool
		var comment string
		if err := a.scan(&linkIDs, &pkgIDs, &all, &comment); err != nil {
			return nil, err
		}
		s.eachLink(l, linkIDs, nil, func(lk *Link) { lk.Comment = comment })
		for _, id := range pkgIDs {
			if _, p := s.list(l).findPkg(id); p != nil {
				p.Comment = comment
			}
		}
		return "", nil
	})
	reg("movetoNewPackage", func(s *Server, a args) (any, *apiErr) {
		var linkIDs, pkgIDs []int64
		var name, dir string
		if err := a.scan(&linkIDs, &pkgIDs, &name, &dir); err != nil {
			return nil, err
		}
		s.moveToNewPackage(l, linkIDs, pkgIDs, name, dir)
		return "", nil
	})
	reg("moveLinks", func(s *Server, a args) (any, *apiErr) {
		var linkIDs []int64
		var after, dest int64
		if err := a.scan(&linkIDs, &after, &dest); err != nil {
			return nil, err
		}
		s.moveLinks(l, linkIDs, after, dest)
		return "", nil
	})
	reg("movePackages", func(s *Server, a args) (any, *apiErr) {
		var pkgIDs []int64
		var after int64
		if err := a.scan(&pkgIDs, &after); err != nil {
			return nil, err
		}
		s.movePackages(l, pkgIDs, after)
		return "", nil
	})
	reg("splitPackageByHoster", func(s *Server, a args) (any, *apiErr) {
		var linkIDs, pkgIDs []int64
		if err := a.scan(&linkIDs, &pkgIDs); err != nil {
			return nil, err
		}
		s.splitByHoster(l, linkIDs, pkgIDs)
		return "", nil
	})
	reg("startOnlineStatusCheck", func(s *Server, a args) (any, *apiErr) {
		var linkIDs, pkgIDs []int64
		if err := a.scan(&linkIDs, &pkgIDs); err != nil {
			return nil, err
		}
		return "", nil
	})
	reg("getDownloadUrls", func(s *Server, a args) (any, *apiErr) {
		var linkIDs, pkgIDs []int64
		var types []string
		if err := a.scan(&linkIDs, &pkgIDs, &types); err != nil {
			return nil, err
		}
		out := map[string][]int64{}
		s.eachLink(l, linkIDs, pkgIDs, func(lk *Link) { out[lk.URL] = append(out[lk.URL], lk.UUID) })
		return out, nil
	})
	reg("cleanup", func(s *Server, a args) (any, *apiErr) {
		var linkIDs, pkgIDs []int64
		var action, mode, sel string
		if err := a.scan(&linkIDs, &pkgIDs, &action, &mode, &sel); err != nil {
			return nil, err
		}
		if !validCleanup[action] || !validMode[mode] || !validSelection[sel] {
			return nil, badParams(action + " " + mode + " " + sel)
		}
		if sel == "UNSELECTED" { // like revision 50639
			return nil, badParams("SelectionType UNSELECTED not yet supported")
		}
		s.cleanup(l, linkIDs, pkgIDs, action, sel)
		return "", nil
	})
}

var (
	validPriority  = map[string]bool{"HIGHEST": true, "HIGHER": true, "HIGH": true, "DEFAULT": true, "LOW": true, "LOWER": true, "LOWEST": true}
	validCleanup   = map[string]bool{"DELETE_ALL": true, "DELETE_DISABLED": true, "DELETE_FAILED": true, "DELETE_FINISHED": true, "DELETE_OFFLINE": true, "DELETE_DUPE": true, "DELETE_MODE": true}
	validMode      = map[string]bool{"REMOVE_LINKS_ONLY": true, "REMOVE_LINKS_AND_RECYCLE_FILES": true, "REMOVE_LINKS_AND_DELETE_FILES": true}
	validSelection = map[string]bool{"SELECTED": true, "UNSELECTED": true, "ALL": true, "NONE": true}
)

// eachLink runs fn for the given links and for all links of the given packages.
func (s *Server) eachLink(l List, linkIDs, pkgIDs []int64, fn func(*Link)) {
	want := idSet(linkIDs)
	pk := idSet(pkgIDs)
	for _, p := range s.list(l).pkgs {
		for _, lk := range p.links {
			if want[lk.UUID] || pk[p.UUID] {
				fn(lk)
			}
		}
	}
}

// structureEvents announces a structure change the way JDownloader does.
func (s *Server) structureEvents(l List) {
	if l == Downloads {
		s.emit("downloads", "REFRESH_STRUCTURE", nil)
		return
	}
	s.emit("linkcollector", "CONTENT_REMOVED", nil)
	s.emit("linkcollector", "STRUCTURE_REFRESH", nil)
}

func (s *Server) removeItems(l List, linkIDs, pkgIDs []int64) {
	ls := s.list(l)
	for _, id := range pkgIDs {
		if i, p := ls.findPkg(id); p != nil {
			ls.pkgs = append(ls.pkgs[:i], ls.pkgs[i+1:]...)
			if l == Downloads {
				s.emit("downloads", "REMOVE_PACKAGE", map[string]any{"uuid": id})
			}
		}
	}
	for _, id := range linkIDs {
		if s.removeLinkLocked(l, id) && l == Downloads {
			s.emit("downloads", "REMOVE_LINK", map[string]any{"uuid": id})
		}
	}
	ls.bump(s)
	s.structureEvents(l)
}

// takeLinks removes the given links and all links of the given packages from
// a list and returns them in list order.
// A package that ends up empty is dropped, except the one with the id keep.
func (s *Server) takeLinks(l List, linkIDs, pkgIDs []int64, keep int64) []*Link {
	want, pk := idSet(linkIDs), idSet(pkgIDs)
	ls := s.list(l)
	var taken []*Link
	var kept []*pkg
	for _, p := range ls.pkgs {
		var rest []*Link
		for _, lk := range p.links {
			if want[lk.UUID] || pk[p.UUID] {
				taken = append(taken, lk)
			} else {
				rest = append(rest, lk)
			}
		}
		p.links = rest
		if len(rest) > 0 || p.UUID == keep {
			kept = append(kept, p)
		}
	}
	ls.pkgs = kept
	return taken
}

func (s *Server) moveToDownloads(linkIDs, pkgIDs []int64) {
	src, dst := s.list(Linkgrabber), s.list(Downloads)
	want, pk := idSet(linkIDs), idSet(pkgIDs)
	var moved []*pkg
	var kept []*pkg
	for _, p := range src.pkgs {
		var take, rest []*Link
		for _, lk := range p.links {
			if want[lk.UUID] || pk[p.UUID] {
				take = append(take, lk)
			} else {
				rest = append(rest, lk)
			}
		}
		if len(take) > 0 {
			np := &pkg{Package: p.Package}
			np.UUID = s.nextID() // the package gets a new id, the links keep theirs
			for _, lk := range take {
				lk.PackageUUID = np.UUID
				np.links = append(np.links, lk)
			}
			moved = append(moved, np)
		}
		p.links = rest
		if len(rest) > 0 {
			kept = append(kept, p)
		}
	}
	if len(moved) == 0 {
		return
	}
	src.pkgs = kept
	for _, np := range moved {
		after := int64(-1)
		if n := len(dst.pkgs); n > 0 {
			after = dst.pkgs[n-1].UUID
		}
		dst.pkgs = append(dst.pkgs, np)
		s.emit("downloads", "ADD_PACKAGE", map[string]any{"uuid": np.UUID, "afterUuid": after})
	}
	src.bump(s)
	dst.bump(s)
	s.emit("linkcollector", "CONTENT_REMOVED", nil)
	s.emit("linkcollector", "STRUCTURE_REFRESH", nil)
	s.emit("downloads", "REFRESH_STRUCTURE", nil)
}

func (s *Server) moveToNewPackage(l List, linkIDs, pkgIDs []int64, name, dir string) {
	taken := s.takeLinks(l, linkIDs, pkgIDs, 0)
	if len(taken) == 0 {
		return
	}
	np := &pkg{Package: Package{UUID: s.nextID(), Name: name, SaveTo: dir}}
	for _, lk := range taken {
		lk.PackageUUID = np.UUID
		np.links = append(np.links, lk)
	}
	ls := s.list(l)
	ls.pkgs = append(ls.pkgs, np)
	ls.bump(s)
	s.structureEvents(l)
}

func (s *Server) moveLinks(l List, linkIDs []int64, after, dest int64) {
	ls := s.list(l)
	if _, d := ls.findPkg(dest); d == nil {
		return
	}
	taken := s.takeLinks(l, linkIDs, nil, dest)
	_, d := ls.findPkg(dest)
	at := 0
	if after != -1 {
		at = len(d.links)
		for i, lk := range d.links {
			if lk.UUID == after {
				at = i + 1
			}
		}
	}
	for _, lk := range taken {
		lk.PackageUUID = dest
	}
	d.links = append(d.links[:at], append(taken, d.links[at:]...)...)
	ls.bump(s)
	s.structureEvents(l)
}

func (s *Server) movePackages(l List, pkgIDs []int64, after int64) {
	ls := s.list(l)
	pk := idSet(pkgIDs)
	var moving, rest []*pkg
	for _, p := range ls.pkgs {
		if pk[p.UUID] {
			moving = append(moving, p)
		} else {
			rest = append(rest, p)
		}
	}
	at := 0
	if after != -1 {
		at = len(rest)
		for i, p := range rest {
			if p.UUID == after {
				at = i + 1
			}
		}
	}
	ls.pkgs = append(rest[:at], append(moving, rest[at:]...)...)
	ls.bump(s)
	s.structureEvents(l)
}

func (s *Server) splitByHoster(l List, linkIDs, pkgIDs []int64) {
	ls := s.list(l)
	target := idSet(pkgIDs)
	want := idSet(linkIDs)
	var out []*pkg
	for _, p := range ls.pkgs {
		hit := target[p.UUID]
		for _, lk := range p.links {
			hit = hit || want[lk.UUID]
		}
		if !hit {
			out = append(out, p)
			continue
		}
		var order []string
		groups := map[string]*pkg{}
		for _, lk := range p.links {
			g := groups[lk.Host]
			if g == nil {
				g = &pkg{Package: p.Package}
				g.UUID = s.nextID()
				g.Name = p.Name + "-" + lk.Host
				groups[lk.Host] = g
				order = append(order, lk.Host)
			}
			lk.PackageUUID = g.UUID
			g.links = append(g.links, lk)
		}
		for _, h := range order {
			out = append(out, groups[h])
		}
	}
	ls.pkgs = out
	ls.bump(s)
	s.structureEvents(l)
}

func (s *Server) cleanup(l List, linkIDs, pkgIDs []int64, action, sel string) {
	want, pk := idSet(linkIDs), idSet(pkgIDs)
	var del []int64
	for _, p := range s.list(l).pkgs {
		for _, lk := range p.links {
			inSel := want[lk.UUID] || pk[p.UUID]
			switch sel {
			case "NONE":
				inSel = false
			case "ALL":
				inSel = true
			case "UNSELECTED":
				inSel = !inSel
			}
			if !inSel {
				continue
			}
			var match bool
			switch action {
			case "DELETE_ALL", "DELETE_MODE":
				match = true
			case "DELETE_FINISHED":
				match = lk.Finished
			case "DELETE_DISABLED":
				match = lk.Disabled
			case "DELETE_OFFLINE":
				match = lk.Availability == "OFFLINE"
			case "DELETE_FAILED":
				match = lk.Skipped
			}
			if match {
				del = append(del, lk.UUID)
			}
		}
	}
	s.removeItems(l, del, nil)
}

// addLinks adds one package with a link per url to the linkgrabber, as the
// link crawler of JDownloader would, and returns the collecting job.
func (s *Server) addLinks(urls []string, pkgName, dir, prio, comment, password string, autostart bool) map[string]any {
	job := s.nextID()
	crawler := s.nextID()
	s.emit("linkcrawler", "STARTED", map[string]any{"jobId": job, "crawlerId": crawler})
	var links []Link
	for _, u := range urls {
		name := path.Base(u)
		if pu, err := url.Parse(u); err == nil {
			name = path.Base(pu.Path)
			u = pu.Scheme + "://" + pu.Host + pu.Path
		}
		links = append(links, Link{Name: name, URL: u, BytesTotal: 1 << 20, Availability: "ONLINE", Priority: prio, Comment: comment, Password: password})
	}
	if len(links) > 0 {
		if pkgName == "" {
			pkgName = links[0].Name
		}
		saveTo := dir
		if saveTo == "" {
			saveTo = "/output"
		}
		_, linkIDs := s.addPackageLocked(Linkgrabber, Package{Name: pkgName, SaveTo: path.Join(saveTo, pkgName), Priority: prio}, links)
		s.emit("linkcollector", "LINK_ADDED", nil)
		s.emit("linkcollector", "CONTENT_ADDED", nil)
		s.emit("linkcollector", "STRUCTURE_REFRESH", nil)
		if autostart {
			s.moveToDownloads(linkIDs, nil)
			s.setController("RUNNING")
		}
	}
	s.emit("linkcrawler", "STOPPED", map[string]any{"jobId": job, "crawlerId": crawler, "links": len(links), "packages": 1})
	s.emit("linkcrawler", "FINISHED", map[string]any{"jobId": job, "crawlerId": crawler})
	return map[string]any{"id": job}
}
