package api

import (
	"context"
	"encoding/base64"
	"net/http"
	"path"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/Buffer379x/jdweb/internal/jd"
	"github.com/Buffer379x/jdweb/internal/state"
)

// actionBody is the body of every list action. Which fields an action reads
// is in docs/api.md; a field the action does not read is ignored.
type actionBody struct {
	LinkIDs        []ID    `json:"linkIds"`
	PackageIDs     []ID    `json:"packageIds"`
	LinkID         *ID     `json:"linkId"`
	PackageID      *ID     `json:"packageId"`
	DestPackageID  *ID     `json:"destPackageId"`
	AfterLinkID    *string `json:"afterLinkId"`
	AfterPackageID *string `json:"afterPackageId"`
	VariantID      string  `json:"variantId"`
	Priority       string  `json:"priority"`
	Name           string  `json:"name"`
	Directory      string  `json:"directory"`
	Password       *string `json:"password"`
	Action         string  `json:"action"`
	Mode           string  `json:"mode"`
	Selection      string  `json:"selection"`
	Reason         string  `json:"reason"`
	Start          bool    `json:"start"`
}

// action is one entry of the table behind /downloads/actions/{action} and
// /linkgrabber/actions/{action}.
type action struct {
	run func(ctx context.Context, s *Server, c *jd.Client, list state.List, b *actionBody) (any, error)
	// lists says where the action exists.
	downloads, grabber bool
	// read actions change nothing and need no resync.
	read bool
}

func ops(c *jd.Client, list state.List) jd.ListOps {
	if list == state.Linkgrabber {
		return c.Linkgrabber()
	}
	return c.Downloads()
}

var priorities = []string{"HIGHEST", "HIGHER", "HIGH", "DEFAULT", "LOW", "LOWER", "LOWEST"}

func validPriority(p string) bool { return slices.Contains(priorities, p) }

func (b *actionBody) links() []int64    { return ints(b.LinkIDs) }
func (b *actionBody) packages() []int64 { return ints(b.PackageIDs) }

func (b *actionBody) needSelection() error {
	if len(b.LinkIDs)+len(b.PackageIDs) == 0 {
		return badRequest("Select at least one link or package (linkIds, packageIds).")
	}
	if len(b.LinkIDs) > maxIDs || len(b.PackageIDs) > maxIDs {
		return badRequest("Too many ids in one request (at most %d).", maxIDs)
	}
	return nil
}

func (b *actionBody) needLink() (int64, error) {
	if b.LinkID == nil {
		return 0, badRequest("linkId is required.")
	}
	return int64(*b.LinkID), nil
}

func (b *actionBody) needPackage() (int64, error) {
	if b.PackageID == nil {
		return 0, badRequest("packageId is required.")
	}
	return int64(*b.PackageID), nil
}

// needText checks a name or a directory: not empty, no control characters.
func needText(field, v string, maxLen int) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", badRequest("%s is required.", field)
	}
	if len(v) > maxLen || strings.ContainsFunc(v, unicode.IsControl) {
		return "", badRequest("%s is too long or holds control characters.", field)
	}
	return v, nil
}

// selectionAction runs fn on the selected links and packages.
func selectionAction(fn func(ctx context.Context, o jd.ListOps, links, pkgs []int64) error) func(context.Context, *Server, *jd.Client, state.List, *actionBody) (any, error) {
	return func(ctx context.Context, _ *Server, c *jd.Client, list state.List, b *actionBody) (any, error) {
		if err := b.needSelection(); err != nil {
			return nil, err
		}
		return nil, fn(ctx, ops(c, list), b.links(), b.packages())
	}
}

var actions = map[string]action{
	"enable": {downloads: true, grabber: true, run: selectionAction(func(ctx context.Context, o jd.ListOps, l, p []int64) error {
		return o.SetEnabled(ctx, true, l, p)
	})},
	"disable": {downloads: true, grabber: true, run: selectionAction(func(ctx context.Context, o jd.ListOps, l, p []int64) error {
		return o.SetEnabled(ctx, false, l, p)
	})},
	"remove": {downloads: true, grabber: true, run: selectionAction(func(ctx context.Context, o jd.ListOps, l, p []int64) error {
		return o.Remove(ctx, l, p)
	})},
	"check-online": {downloads: true, grabber: true, run: selectionAction(func(ctx context.Context, o jd.ListOps, l, p []int64) error {
		return o.CheckOnline(ctx, l, p)
	})},
	"split-by-hoster": {downloads: true, grabber: true, run: selectionAction(func(ctx context.Context, o jd.ListOps, l, p []int64) error {
		return o.SplitByHoster(ctx, l, p)
	})},
	"priority": {downloads: true, grabber: true, run: func(ctx context.Context, _ *Server, c *jd.Client, list state.List, b *actionBody) (any, error) {
		if err := b.needSelection(); err != nil {
			return nil, err
		}
		if !validPriority(b.Priority) {
			return nil, badRequest("priority must be one of %s.", strings.Join(priorities, ", "))
		}
		return nil, ops(c, list).SetPriority(ctx, jd.Priority(b.Priority), b.links(), b.packages())
	}},
	"rename-link": {downloads: true, grabber: true, run: func(ctx context.Context, _ *Server, c *jd.Client, list state.List, b *actionBody) (any, error) {
		id, err := b.needLink()
		if err != nil {
			return nil, err
		}
		name, err := needText("name", b.Name, 1024)
		if err != nil {
			return nil, err
		}
		return nil, ops(c, list).RenameLink(ctx, id, name)
	}},
	"rename-package": {downloads: true, grabber: true, run: func(ctx context.Context, _ *Server, c *jd.Client, list state.List, b *actionBody) (any, error) {
		id, err := b.needPackage()
		if err != nil {
			return nil, err
		}
		name, err := needText("name", b.Name, 1024)
		if err != nil {
			return nil, err
		}
		return nil, ops(c, list).RenamePackage(ctx, id, name)
	}},
	"directory": {downloads: true, grabber: true, run: func(ctx context.Context, _ *Server, c *jd.Client, list state.List, b *actionBody) (any, error) {
		if len(b.PackageIDs) == 0 {
			return nil, badRequest("Select at least one package (packageIds).")
		}
		dir, err := needText("directory", b.Directory, 4096)
		if err != nil {
			return nil, err
		}
		return nil, ops(c, list).SetDirectory(ctx, dir, b.packages())
	}},
	"password": {downloads: true, grabber: true, run: func(ctx context.Context, _ *Server, c *jd.Client, list state.List, b *actionBody) (any, error) {
		if err := b.needSelection(); err != nil {
			return nil, err
		}
		if b.Password == nil {
			return nil, badRequest("password is required (an empty one clears it).")
		}
		if len(*b.Password) > 4096 {
			return nil, badRequest("password is too long.")
		}
		return nil, ops(c, list).SetPassword(ctx, *b.Password, b.links(), b.packages())
	}},
	"new-package": {downloads: true, grabber: true, run: func(ctx context.Context, _ *Server, c *jd.Client, list state.List, b *actionBody) (any, error) {
		if err := b.needSelection(); err != nil {
			return nil, err
		}
		name, err := needText("name", b.Name, 1024)
		if err != nil {
			return nil, err
		}
		dir := strings.TrimSpace(b.Directory)
		if len(dir) > 4096 || strings.ContainsFunc(dir, unicode.IsControl) {
			return nil, badRequest("directory is too long or holds control characters.")
		}
		return nil, ops(c, list).MoveToNewPackage(ctx, b.links(), b.packages(), name, dir)
	}},
	"move-links": {downloads: true, grabber: true, run: func(ctx context.Context, _ *Server, c *jd.Client, list state.List, b *actionBody) (any, error) {
		if len(b.LinkIDs) == 0 {
			return nil, badRequest("Select at least one link (linkIds).")
		}
		if b.DestPackageID == nil {
			return nil, badRequest("destPackageId is required.")
		}
		after, err := parseAfter(b.AfterLinkID)
		if err != nil {
			return nil, err
		}
		return nil, ops(c, list).MoveLinks(ctx, b.links(), after, int64(*b.DestPackageID))
	}},
	"move-packages": {downloads: true, grabber: true, run: func(ctx context.Context, _ *Server, c *jd.Client, list state.List, b *actionBody) (any, error) {
		if len(b.PackageIDs) == 0 {
			return nil, badRequest("Select at least one package (packageIds).")
		}
		after, err := parseAfter(b.AfterPackageID)
		if err != nil {
			return nil, err
		}
		return nil, ops(c, list).MovePackages(ctx, b.packages(), after)
	}},
	"cleanup": {downloads: true, grabber: true, run: runCleanup},
	"urls": {downloads: true, grabber: true, read: true, run: func(ctx context.Context, _ *Server, c *jd.Client, list state.List, b *actionBody) (any, error) {
		if err := b.needSelection(); err != nil {
			return nil, err
		}
		byURL, err := ops(c, list).URLs(ctx, b.links(), b.packages())
		if err != nil {
			return nil, err
		}
		type item struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		}
		type pair struct {
			id  int64
			url string
		}
		var flat []pair
		for u, ids := range byURL {
			for _, id := range ids {
				flat = append(flat, pair{id, u})
			}
		}
		sort.Slice(flat, func(i, j int) bool { return flat[i].id < flat[j].id })
		out := make([]item, len(flat))
		for i, f := range flat {
			out[i] = item{idString(f.id), f.url}
		}
		return map[string]any{"links": out}, nil
	}},

	// Download list only.
	"force": {downloads: true, run: func(ctx context.Context, _ *Server, c *jd.Client, _ state.List, b *actionBody) (any, error) {
		if err := b.needSelection(); err != nil {
			return nil, err
		}
		return nil, c.DownloadsForce(ctx, b.links(), b.packages())
	}},
	"resume": {downloads: true, run: func(ctx context.Context, _ *Server, c *jd.Client, _ state.List, b *actionBody) (any, error) {
		if err := b.needSelection(); err != nil {
			return nil, err
		}
		return nil, c.DownloadsResume(ctx, b.links(), b.packages())
	}},
	"reset": {downloads: true, run: func(ctx context.Context, _ *Server, c *jd.Client, _ state.List, b *actionBody) (any, error) {
		if err := b.needSelection(); err != nil {
			return nil, err
		}
		return nil, c.DownloadsReset(ctx, b.links(), b.packages())
	}},
	"unskip": {downloads: true, run: func(ctx context.Context, _ *Server, c *jd.Client, _ state.List, b *actionBody) (any, error) {
		if err := b.needSelection(); err != nil {
			return nil, err
		}
		reason := jd.SkipReason(b.Reason)
		if reason != "" && !slices.Contains(skipReasons, reason) {
			return nil, badRequest("reason must be empty or one of the skip reasons of JDownloader.")
		}
		return nil, c.DownloadsUnskip(ctx, b.packages(), b.links(), reason)
	}},
	"stopmark-set": {downloads: true, run: func(ctx context.Context, _ *Server, c *jd.Client, _ state.List, b *actionBody) (any, error) {
		if (b.LinkID == nil) == (b.PackageID == nil) {
			return nil, badRequest("Give exactly one of linkId and packageId.")
		}
		if b.LinkID != nil {
			return nil, c.SetStopMark(ctx, int64(*b.LinkID), -1)
		}
		return nil, c.SetStopMark(ctx, -1, int64(*b.PackageID))
	}},
	"stopmark-remove": {downloads: true, run: func(ctx context.Context, _ *Server, c *jd.Client, _ state.List, _ *actionBody) (any, error) {
		return nil, c.RemoveStopMark(ctx)
	}},

	// Linkgrabber only.
	"to-downloads": {grabber: true, run: func(ctx context.Context, s *Server, c *jd.Client, _ state.List, b *actionBody) (any, error) {
		if err := b.needSelection(); err != nil {
			return nil, err
		}
		if err := c.MoveToDownloads(ctx, b.links(), b.packages()); err != nil {
			return nil, err
		}
		s.engine.Resync(state.Downloads)
		if b.Start {
			return nil, c.ControllerStart(ctx)
		}
		return nil, nil
	}},
	"abort": {grabber: true, run: func(ctx context.Context, _ *Server, c *jd.Client, _ state.List, _ *actionBody) (any, error) {
		return nil, c.AbortCollecting(ctx)
	}},
	"clear": {grabber: true, run: func(ctx context.Context, _ *Server, c *jd.Client, _ state.List, _ *actionBody) (any, error) {
		return nil, c.ClearLinkgrabber(ctx)
	}},
	"set-variant": {grabber: true, run: func(ctx context.Context, _ *Server, c *jd.Client, _ state.List, b *actionBody) (any, error) {
		id, err := b.needLink()
		if err != nil {
			return nil, err
		}
		v, err := needText("variantId", b.VariantID, 256)
		if err != nil {
			return nil, err
		}
		return nil, c.SetVariant(ctx, id, v)
	}},
	"add-variant": {grabber: true, run: runAddVariant},
}

var skipReasons = []jd.SkipReason{
	jd.SkipReasonConnectionUnavailable, jd.SkipReasonTooManyRetries, jd.SkipReasonCaptcha, jd.SkipReasonManual,
	jd.SkipReasonDiskFull, jd.SkipReasonNoAccount, jd.SkipReasonInvalidDestination, jd.SkipReasonFileExists,
	jd.SkipReasonUpdateRestartRequired, jd.SkipReasonFFmpegMissing, jd.SkipReasonFFprobeMissing,
}

var (
	cleanupActions = []jd.CleanupAction{jd.CleanupDeleteAll, jd.CleanupDeleteDisabled, jd.CleanupDeleteFailed,
		jd.CleanupDeleteFinished, jd.CleanupDeleteOffline, jd.CleanupDeleteDupe, jd.CleanupDeleteMode}
	cleanupModes = []jd.CleanupMode{jd.CleanupRemoveLinksOnly, jd.CleanupRemoveLinksAndRecycleFiles, jd.CleanupRemoveLinksAndDeleteFiles}
	selections   = []jd.SelectionType{jd.SelectionSelected, jd.SelectionUnselected, jd.SelectionAll, jd.SelectionNone}
)

// runCleanup validates all three enums before JDownloader sees them. The mode
// has no default on purpose: whether files on disk are deleted must be said.
func runCleanup(ctx context.Context, _ *Server, c *jd.Client, list state.List, b *actionBody) (any, error) {
	a, m, sel := jd.CleanupAction(b.Action), jd.CleanupMode(b.Mode), jd.SelectionType(b.Selection)
	if b.Mode == "" {
		return nil, badRequest("mode is required. It decides what happens to files on disk: %s.", joinEnum(cleanupModes))
	}
	switch {
	case !slices.Contains(cleanupActions, a):
		return nil, badRequest("action must be one of %s.", joinEnum(cleanupActions))
	case !slices.Contains(cleanupModes, m):
		return nil, badRequest("mode must be one of %s.", joinEnum(cleanupModes))
	case !slices.Contains(selections, sel):
		return nil, badRequest("selection must be one of %s.", joinEnum(selections))
	}
	// Only ALL may go without ids. JDownloader reads UNSELECTED and NONE with an
	// empty list as "everything", which nobody asks for by accident.
	if sel != jd.SelectionAll && len(b.LinkIDs)+len(b.PackageIDs) == 0 {
		return nil, badRequest("selection %s needs at least one link or package. To clean up everything, say selection ALL.", sel)
	}
	if len(b.LinkIDs) > maxIDs || len(b.PackageIDs) > maxIDs {
		return nil, badRequest("Too many ids in one request (at most %d).", maxIDs)
	}
	return nil, ops(c, list).Cleanup(ctx, b.links(), b.packages(), a, m, sel)
}

func joinEnum[T ~string](vs []T) string {
	s := make([]string, len(vs))
	for i, v := range vs {
		s[i] = string(v)
	}
	return strings.Join(s, ", ")
}

// runAddVariant adds a variant of a link as a new link. Without afterLinkId and
// destPackageId the copy goes right behind the original, in its package.
func runAddVariant(ctx context.Context, s *Server, c *jd.Client, _ state.List, b *actionBody) (any, error) {
	id, err := b.needLink()
	if err != nil {
		return nil, err
	}
	v, err := needText("variantId", b.VariantID, 256)
	if err != nil {
		return nil, err
	}
	after, dest := id, parseOptionalID(b.DestPackageID)
	if b.AfterLinkID != nil {
		if after, err = parseAfter(b.AfterLinkID); err != nil {
			return nil, err
		}
	}
	if dest == 0 {
		for _, l := range s.mirror.Snapshot(state.Linkgrabber).LgLinks {
			if l.ID == idString(id) {
				dest, _ = parseID(l.PackageID)
				break
			}
		}
		if dest == 0 {
			return nil, &apiError{Status: http.StatusNotFound, Code: codeJDError, Message: msgGone}
		}
	}
	return nil, c.AddVariantCopy(ctx, id, after, dest, v)
}

// handleListAction is POST /{downloads,linkgrabber}/actions/{action}.
func (s *Server) handleListAction(list state.List) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("action")
		a, ok := actions[name]
		if !ok || (list == state.Downloads && !a.downloads) || (list == state.Linkgrabber && !a.grabber) {
			s.fail(w, r, notFound("Unknown action %q.", clip(name, 40)))
			return
		}
		var b actionBody
		if err := decode(r, &b, true); err != nil {
			s.fail(w, r, err)
			return
		}
		c, err := s.client()
		if err != nil {
			s.fail(w, r, err)
			return
		}
		res, err := a.run(r.Context(), s, c, list, &b)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		// JDownloader answers an action on an entry it dropped meanwhile with
		// success and sends no event. A reload lets the browser converge.
		if !a.read {
			s.engine.Resync(list)
		}
		if res == nil {
			noContent(w)
			return
		}
		s.writeJSON(w, r, http.StatusOK, res)
	}
}

func (s *Server) handleStopMark(w http.ResponseWriter, r *http.Request) {
	c, err := s.client()
	var l *jd.DownloadLink
	if err == nil {
		l, err = c.StopMarkedLink(r.Context())
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if l == nil {
		s.writeJSON(w, r, http.StatusOK, map[string]any{"marked": false})
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"marked": true, "linkId": idString(l.UUID), "packageId": idString(l.PackageUUID), "name": l.Name,
	})
}

// addLinksBody is the body of POST /linkgrabber/add.
type addLinksBody struct {
	Links                    []string `json:"links"`
	PackageName              string   `json:"packageName"`
	DestinationFolder        string   `json:"destinationFolder"`
	ExtractPassword          string   `json:"extractPassword"`
	DownloadPassword         string   `json:"downloadPassword"`
	Autostart                *bool    `json:"autostart"`
	AutoExtract              *bool    `json:"autoExtract"`
	DeepDecrypt              *bool    `json:"deepDecrypt"`
	OverwritePackagizerRules *bool    `json:"overwritePackagizerRules"`
	Priority                 string   `json:"priority"`
}

func (s *Server) handleAddLinks(w http.ResponseWriter, r *http.Request) {
	var b addLinksBody
	if err := decode(r, &b, false); err != nil {
		s.fail(w, r, err)
		return
	}
	var links []string
	for _, l := range b.Links {
		if l = strings.TrimSpace(l); l != "" {
			links = append(links, l)
		}
	}
	if len(links) == 0 {
		s.fail(w, r, badRequest("links must hold at least one link."))
		return
	}
	if b.Priority != "" && !validPriority(b.Priority) {
		s.fail(w, r, badRequest("priority must be one of %s.", strings.Join(priorities, ", ")))
		return
	}
	for _, f := range []struct{ name, v string }{{"packageName", b.PackageName}, {"destinationFolder", b.DestinationFolder}} {
		if len(f.v) > 4096 || strings.ContainsFunc(f.v, unicode.IsControl) {
			s.fail(w, r, badRequest("%s is too long or holds control characters.", f.name))
			return
		}
	}
	c, err := s.client()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	job, err := c.AddLinks(r.Context(), jd.AddLinksRequest{
		Links: links, PackageName: strings.TrimSpace(b.PackageName), DestinationFolder: strings.TrimSpace(b.DestinationFolder),
		ExtractPassword: b.ExtractPassword, DownloadPassword: b.DownloadPassword, Priority: jd.Priority(b.Priority),
		Autostart: b.Autostart, AutoExtract: b.AutoExtract, DeepDecrypt: b.DeepDecrypt, OverwritePackagizerRules: b.OverwritePackagizerRules,
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.engine.Resync(state.Linkgrabber)
	s.writeJSON(w, r, http.StatusOK, map[string]string{"jobId": idString(job)})
}

// maxContainer is the largest container file accepted, decoded.
const maxContainer = 40 << 20

func (s *Server) handleAddContainer(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Name          string `json:"name"`
		ContentBase64 string `json:"contentBase64"`
	}
	if err := decode(r, &b, false); err != nil {
		s.fail(w, r, err)
		return
	}
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(strings.TrimSpace(b.Name)), "."))
	if ext == "" || len(ext) > 12 || strings.ContainsFunc(ext, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		s.fail(w, r, badRequest("name must be a file name with the extension of the container, for example links.dlc."))
		return
	}
	content, err := base64.StdEncoding.DecodeString(b.ContentBase64)
	if err != nil || len(content) == 0 {
		s.fail(w, r, badRequest("contentBase64 must be the container file, base64 encoded."))
		return
	}
	if len(content) > maxContainer {
		s.fail(w, r, badRequest("The container is larger than %d bytes.", maxContainer))
		return
	}
	c, err := s.client()
	var job int64
	if err == nil {
		job, err = c.AddContainer(r.Context(), ext, content)
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.engine.Resync(state.Linkgrabber)
	s.writeJSON(w, r, http.StatusOK, map[string]string{"jobId": idString(job)})
}

func (s *Server) handleVariants(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("linkId"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	c, err := s.client()
	var vs []jd.LinkVariant
	if err == nil {
		vs, err = c.Variants(r.Context(), id)
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if vs == nil {
		vs = []jd.LinkVariant{}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"variants": vs})
}

// maxFolders bounds the storage lookups behind GET /linkgrabber/folders.
const maxFolders = 20

func (s *Server) handleFolders(w http.ResponseWriter, r *http.Request) {
	c, err := s.client()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	paths, err := c.DownloadFolderHistory(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	type folder struct {
		Path string `json:"path"`
		Free int64  `json:"free"`
		Size int64  `json:"size"`
	}
	out := []folder{}
	for _, p := range paths[:min(len(paths), maxFolders)] {
		f := folder{Path: p}
		infos, err := c.StorageInfos(r.Context(), p)
		switch {
		case err == nil && len(infos) > 0 && infos[0].Error == "":
			f.Free, f.Size = infos[0].Free, infos[0].Size
		case err != nil && (jd.IsConnection(err) || r.Context().Err() != nil):
			s.fail(w, r, err)
			return
		}
		out = append(out, f)
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"folders": out})
}
