package jd

import "context"

const (
	pathDLPackages    = "/downloadsV2/queryPackages"
	pathDLLinks       = "/downloadsV2/queryLinks"
	pathDLStructure   = "/downloadsV2/getStructureChangeCounter"
	pathDLForce       = "/downloadsV2/forceDownload"
	pathDLResume      = "/downloadsV2/resumeLinks"
	pathDLReset       = "/downloadsV2/resetLinks"
	pathDLUnskip      = "/downloadsV2/unskip"
	pathDLSetStopMark = "/downloadsV2/setStopMark"
	pathDLGetStopMark = "/downloadsV2/getStopMarkedLink"
	pathDLClearStop   = "/downloadsV2/removeStopMark"
	pathCtrlStart     = "/downloadcontroller/start"
	pathCtrlStop      = "/downloadcontroller/stop"
	pathCtrlPause     = "/downloadcontroller/pause"
	pathCtrlState     = "/downloadcontroller/getCurrentState"
	pathCtrlSpeed     = "/downloadcontroller/getSpeedInBps"
	dlNamespace       = "/downloadsV2"
)

// DownloadPackage is a package in the download list.
type DownloadPackage struct {
	UUID          int64    `json:"uuid"`
	Name          string   `json:"name"`
	Hosts         []string `json:"hosts"`
	ChildCount    int      `json:"childCount"`
	BytesTotal    int64    `json:"bytesTotal"`
	BytesLoaded   int64    `json:"bytesLoaded"`
	Speed         int64    `json:"speed"`
	ETA           int64    `json:"eta"` // see ETASeconds
	Enabled       bool     `json:"enabled"`
	Finished      bool     `json:"finished"`
	Running       bool     `json:"running"`
	SaveTo        string   `json:"saveTo"`
	Status        string   `json:"status"` // display text, never evaluate it
	StatusIconKey string   `json:"statusIconKey"`
	Priority      Priority `json:"priority"`
	Comment       string   `json:"comment"`
	Password      string   `json:"downloadPassword"`
	ActiveTask    string   `json:"activeTask"`
}

// DownloadLink is a link in the download list.
type DownloadLink struct {
	UUID             int64          `json:"uuid"`
	PackageUUID      int64          `json:"packageUUID"`
	JobUUID          int64          `json:"jobUUID"`
	Name             string         `json:"name"`
	Host             string         `json:"host"`
	URL              string         `json:"url"`
	BytesTotal       int64          `json:"bytesTotal"`
	BytesLoaded      int64          `json:"bytesLoaded"`
	Speed            int64          `json:"speed"`
	ETA              int64          `json:"eta"` // see ETASeconds
	Enabled          bool           `json:"enabled"`
	Finished         bool           `json:"finished"`
	Running          bool           `json:"running"`
	Skipped          bool           `json:"skipped"`
	ExtractionStatus string         `json:"extractionStatus"`
	Status           string         `json:"status"` // display text, never evaluate it
	StatusIconKey    string         `json:"statusIconKey"`
	Priority         Priority       `json:"priority"`
	Comment          string         `json:"comment"`
	Password         string         `json:"downloadPassword"`
	AddedDate        int64          `json:"addedDate"`
	FinishedDate     int64          `json:"finishedDate"`
	AdvancedStatus   AdvancedStatus `json:"advancedStatus"`
}

// DownloadPackageQuery selects packages and the fields to return. Without any
// field JDownloader returns only uuid and name. Zero values are not sent.
type DownloadPackageQuery struct {
	BytesLoaded bool    `json:"bytesLoaded,omitempty"`
	BytesTotal  bool    `json:"bytesTotal,omitempty"`
	ChildCount  bool    `json:"childCount,omitempty"`
	Comment     bool    `json:"comment,omitempty"`
	Enabled     bool    `json:"enabled,omitempty"`
	ETA         bool    `json:"eta,omitempty"`
	Finished    bool    `json:"finished,omitempty"`
	Hosts       bool    `json:"hosts,omitempty"`
	Priority    bool    `json:"priority,omitempty"`
	Running     bool    `json:"running,omitempty"`
	SaveTo      bool    `json:"saveTo,omitempty"`
	Speed       bool    `json:"speed,omitempty"`
	Status      bool    `json:"status,omitempty"`
	PackageIDs  []int64 `json:"packageUUIDs,omitempty"`
	MaxResults  int     `json:"maxResults,omitempty"`
	StartAt     int     `json:"startAt,omitempty"`
}

// AllDownloadPackageFields returns a query for all packages with every field.
func AllDownloadPackageFields() DownloadPackageQuery {
	return DownloadPackageQuery{
		BytesLoaded: true, BytesTotal: true, ChildCount: true, Comment: true, Enabled: true,
		ETA: true, Finished: true, Hosts: true, Priority: true, Running: true, SaveTo: true,
		Speed: true, Status: true,
	}
}

// DownloadLinkQuery selects links and the fields to return. Without any field
// JDownloader returns only uuid, packageUUID and name. Zero values are not sent.
type DownloadLinkQuery struct {
	AddedDate        bool    `json:"addedDate,omitempty"`
	AdvancedStatus   bool    `json:"advancedStatus,omitempty"`
	BytesLoaded      bool    `json:"bytesLoaded,omitempty"`
	BytesTotal       bool    `json:"bytesTotal,omitempty"`
	Comment          bool    `json:"comment,omitempty"`
	Enabled          bool    `json:"enabled,omitempty"`
	ETA              bool    `json:"eta,omitempty"`
	ExtractionStatus bool    `json:"extractionStatus,omitempty"`
	Finished         bool    `json:"finished,omitempty"`
	FinishedDate     bool    `json:"finishedDate,omitempty"`
	Host             bool    `json:"host,omitempty"`
	JobUUID          bool    `json:"jobUUID,omitempty"`
	Password         bool    `json:"password,omitempty"`
	Priority         bool    `json:"priority,omitempty"`
	Running          bool    `json:"running,omitempty"`
	Skipped          bool    `json:"skipped,omitempty"`
	Speed            bool    `json:"speed,omitempty"`
	Status           bool    `json:"status,omitempty"`
	URL              bool    `json:"url,omitempty"`
	LinkIDs          []int64 `json:"linkUUIDs,omitempty"`
	PackageIDs       []int64 `json:"packageUUIDs,omitempty"`
	MaxResults       int     `json:"maxResults,omitempty"`
	StartAt          int     `json:"startAt,omitempty"`
}

// AllDownloadLinkFields returns a query for all links with every field. It
// asks for AdvancedStatus too, which is the only place for the skip reason.
func AllDownloadLinkFields() DownloadLinkQuery {
	return DownloadLinkQuery{
		AddedDate: true, AdvancedStatus: true, BytesLoaded: true, BytesTotal: true, Comment: true,
		Enabled: true, ETA: true, ExtractionStatus: true, Finished: true, FinishedDate: true,
		Host: true, JobUUID: true, Password: true, Priority: true, Running: true, Skipped: true,
		Speed: true, Status: true, URL: true,
	}
}

// DownloadPackages queries the packages of the download list, in list order.
func (c *Client) DownloadPackages(ctx context.Context, q DownloadPackageQuery) (out []DownloadPackage, err error) {
	return out, c.Call(ctx, pathDLPackages, &out, q)
}

// DownloadLinks queries the links of the download list, in list order.
func (c *Client) DownloadLinks(ctx context.Context, q DownloadLinkQuery) (out []DownloadLink, err error) {
	return out, c.Call(ctx, pathDLLinks, &out, q)
}

// DownloadsStructureChanged compares the structure counter of the download
// list with the last one seen (0 on the first call). changed is false while
// nothing was added, removed or moved; counter is then the old value.
func (c *Client) DownloadsStructureChanged(ctx context.Context, last int64) (counter int64, changed bool, err error) {
	return structureChanged(ctx, c, pathDLStructure, last)
}

// structureChanged calls a "has the structure changed since" method: it
// answers -1 when the counter equals last and the new counter otherwise.
func structureChanged(ctx context.Context, c *Client, path string, last int64) (int64, bool, error) {
	var n int64
	if err := c.Call(ctx, path, &n, last); err != nil {
		return last, false, err
	}
	if n == -1 {
		return last, false, nil
	}
	return n, true, nil
}

// Downloads returns the operations the download list and the linkgrabber have in common.
func (c *Client) Downloads() ListOps { return ListOps{c, dlNamespace} }

// ListOps are the calls that work the same way on the download list and on
// the linkgrabber. Ids that no longer exist are ignored by JDownloader.
type ListOps struct {
	c  *Client
	ns string
}

func ids(s []int64) []int64 {
	if s == nil {
		return []int64{}
	}
	return s
}

// SetEnabled enables or disables links and packages.
func (l ListOps) SetEnabled(ctx context.Context, enabled bool, linkIDs, pkgIDs []int64) error {
	return l.c.Call(ctx, l.ns+"/setEnabled", nil, enabled, ids(linkIDs), ids(pkgIDs))
}

// Remove removes links and packages from the list.
func (l ListOps) Remove(ctx context.Context, linkIDs, pkgIDs []int64) error {
	return l.c.Call(ctx, l.ns+"/removeLinks", nil, ids(linkIDs), ids(pkgIDs))
}

// SetPriority sets the priority of links and packages.
func (l ListOps) SetPriority(ctx context.Context, p Priority, linkIDs, pkgIDs []int64) error {
	return l.c.Call(ctx, l.ns+"/setPriority", nil, p, ids(linkIDs), ids(pkgIDs))
}

// RenameLink renames a link.
func (l ListOps) RenameLink(ctx context.Context, linkID int64, name string) error {
	return l.c.Call(ctx, l.ns+"/renameLink", nil, linkID, name)
}

// RenamePackage renames a package.
func (l ListOps) RenamePackage(ctx context.Context, pkgID int64, name string) error {
	return l.c.Call(ctx, l.ns+"/renamePackage", nil, pkgID, name)
}

// SetDirectory sets the save-to directory of packages.
func (l ListOps) SetDirectory(ctx context.Context, dir string, pkgIDs []int64) error {
	return l.c.Call(ctx, l.ns+"/setDownloadDirectory", nil, dir, ids(pkgIDs))
}

// SetPassword sets the download password of links and packages.
func (l ListOps) SetPassword(ctx context.Context, password string, linkIDs, pkgIDs []int64) error {
	return l.c.Call(ctx, l.ns+"/setDownloadPassword", nil, ids(linkIDs), ids(pkgIDs), password)
}

// MoveToNewPackage moves links (and the links of packages) into a new package.
func (l ListOps) MoveToNewPackage(ctx context.Context, linkIDs, pkgIDs []int64, name, dir string) error {
	return l.c.Call(ctx, l.ns+"/movetoNewPackage", nil, ids(linkIDs), ids(pkgIDs), name, dir)
}

// MoveLinks moves links behind afterLinkID into package destPkgID. afterLinkID
// -1 puts them at the top.
func (l ListOps) MoveLinks(ctx context.Context, linkIDs []int64, afterLinkID, destPkgID int64) error {
	return l.c.Call(ctx, l.ns+"/moveLinks", nil, ids(linkIDs), afterLinkID, destPkgID)
}

// MovePackages moves packages behind afterPkgID. afterPkgID -1 puts them at the top.
func (l ListOps) MovePackages(ctx context.Context, pkgIDs []int64, afterPkgID int64) error {
	return l.c.Call(ctx, l.ns+"/movePackages", nil, ids(pkgIDs), afterPkgID)
}

// SplitByHoster splits packages into one package per host.
func (l ListOps) SplitByHoster(ctx context.Context, linkIDs, pkgIDs []int64) error {
	return l.c.Call(ctx, l.ns+"/splitPackageByHoster", nil, ids(linkIDs), ids(pkgIDs))
}

// CheckOnline starts an online check of links and packages.
func (l ListOps) CheckOnline(ctx context.Context, linkIDs, pkgIDs []int64) error {
	return l.c.Call(ctx, l.ns+"/startOnlineStatusCheck", nil, ids(linkIDs), ids(pkgIDs))
}

// URLs returns the urls of links and packages, mapped to the ids of the links.
func (l ListOps) URLs(ctx context.Context, linkIDs, pkgIDs []int64, types ...UrlDisplayType) (out map[string][]int64, err error) {
	if len(types) == 0 {
		types = []UrlDisplayType{UrlContent}
	}
	return out, l.c.Call(ctx, l.ns+"/getDownloadUrls", &out, ids(linkIDs), ids(pkgIDs), types)
}

// Cleanup removes entries by action. With SelectionAll the ids are ignored.
func (l ListOps) Cleanup(ctx context.Context, linkIDs, pkgIDs []int64, a CleanupAction, m CleanupMode, s SelectionType) error {
	return l.c.Call(ctx, l.ns+"/cleanup", nil, ids(linkIDs), ids(pkgIDs), a, m, s)
}

// DownloadsForce forces the download of links and packages, ignoring limits.
func (c *Client) DownloadsForce(ctx context.Context, linkIDs, pkgIDs []int64) error {
	return c.Call(ctx, pathDLForce, nil, ids(linkIDs), ids(pkgIDs))
}

// DownloadsResume resumes links and packages.
func (c *Client) DownloadsResume(ctx context.Context, linkIDs, pkgIDs []int64) error {
	return c.Call(ctx, pathDLResume, nil, ids(linkIDs), ids(pkgIDs))
}

// DownloadsReset resets links and packages, which deletes what was loaded.
func (c *Client) DownloadsReset(ctx context.Context, linkIDs, pkgIDs []int64) error {
	return c.Call(ctx, pathDLReset, nil, ids(linkIDs), ids(pkgIDs))
}

// DownloadsUnskip clears the skipped state. An empty reason matches every
// reason. Mind the parameter order: package ids first, then link ids, the
// reverse of the other calls. It is the order of JDownloader's unskip.
func (c *Client) DownloadsUnskip(ctx context.Context, pkgIDs, linkIDs []int64, reason SkipReason) error {
	var r any
	if reason != "" {
		r = reason
	}
	return c.Call(ctx, pathDLUnskip, nil, ids(pkgIDs), ids(linkIDs), r)
}

// StopMarkedLink returns the link the downloads stop after, or nil. Only uuid,
// packageUUID and name are filled.
func (c *Client) StopMarkedLink(ctx context.Context) (*DownloadLink, error) {
	var l DownloadLink
	if err := c.Call(ctx, pathDLGetStopMark, &l); err != nil || l.UUID == 0 {
		return nil, err
	}
	return &l, nil
}

// SetStopMark makes the downloads stop after a link, or after a package if
// linkID is -1.
func (c *Client) SetStopMark(ctx context.Context, linkID, pkgID int64) error {
	return c.Call(ctx, pathDLSetStopMark, nil, linkID, pkgID)
}

// RemoveStopMark removes the stop mark.
func (c *Client) RemoveStopMark(ctx context.Context) error {
	return c.Call(ctx, pathDLClearStop, nil)
}

// ControllerStart starts the downloads.
func (c *Client) ControllerStart(ctx context.Context) error {
	return c.Call(ctx, pathCtrlStart, nil)
}

// ControllerStop stops the downloads.
func (c *Client) ControllerStop(ctx context.Context) error {
	return c.Call(ctx, pathCtrlStop, nil)
}

// ControllerPause pauses (true) or resumes (false) the downloads.
func (c *Client) ControllerPause(ctx context.Context, pause bool) error {
	return c.Call(ctx, pathCtrlPause, nil, pause)
}

// ControllerState returns the state of the download controller.
func (c *Client) ControllerState(ctx context.Context) (s ControllerState, err error) {
	return s, c.Call(ctx, pathCtrlState, &s)
}

// ControllerSpeed returns the current download speed in bytes per second.
func (c *Client) ControllerSpeed(ctx context.Context) (n int64, err error) {
	return n, c.Call(ctx, pathCtrlSpeed, &n)
}
