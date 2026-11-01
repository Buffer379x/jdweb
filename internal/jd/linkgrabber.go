package jd

import (
	"context"
	"encoding/base64"
	"strings"
)

const (
	lgNamespace        = "/linkgrabberv2"
	pathLGPackages     = "/linkgrabberv2/queryPackages"
	pathLGLinks        = "/linkgrabberv2/queryLinks"
	pathLGStructure    = "/linkgrabberv2/getChildrenChanged"
	pathLGAddLinks     = "/linkgrabberv2/addLinks"
	pathLGAddContainer = "/linkgrabberv2/addContainer"
	pathLGToDownloads  = "/linkgrabberv2/moveToDownloadlist"
	pathLGClear        = "/linkgrabberv2/clearList"
	pathLGAbort        = "/linkgrabberv2/abort"
	pathLGIsCollecting = "/linkgrabberv2/isCollecting"
	pathLGCrawlerJobs  = "/linkgrabberv2/queryLinkCrawlerJobs"
	pathLGVariants     = "/linkgrabberv2/getVariants"
	pathLGSetVariant   = "/linkgrabberv2/setVariant"
	pathLGAddVariant   = "/linkgrabberv2/addVariantCopy"
	pathLGFolders      = "/linkgrabberv2/getDownloadFolderHistorySelectionBase"
	pathLinkCrawling   = "/linkcrawler/isCrawling"
)

// GrabberPackage is a package in the linkgrabber.
type GrabberPackage struct {
	UUID             int64    `json:"uuid"`
	Name             string   `json:"name"`
	Hosts            []string `json:"hosts"`
	ChildCount       int      `json:"childCount"`
	BytesTotal       int64    `json:"bytesTotal"`
	SaveTo           string   `json:"saveTo"`
	OnlineCount      int      `json:"onlineCount"`
	OfflineCount     int      `json:"offlineCount"`
	UnknownCount     int      `json:"unknownCount"`
	TempUnknownCount int      `json:"tempUnknownCount"`
	Enabled          bool     `json:"enabled"`
	Priority         Priority `json:"priority"`
	Comment          string   `json:"comment"`
	Password         string   `json:"downloadPassword"`
	Status           string   `json:"status"`
}

// GrabberLink is a link in the linkgrabber.
type GrabberLink struct {
	UUID           int64          `json:"uuid"`
	PackageUUID    int64          `json:"packageUUID"`
	JobUUID        int64          `json:"jobUUID"`
	Name           string         `json:"name"`
	Host           string         `json:"host"`
	URL            string         `json:"url"`
	BytesTotal     int64          `json:"bytesTotal"`
	Availability   Availability   `json:"availability"`
	Enabled        bool           `json:"enabled"`
	Priority       Priority       `json:"priority"`
	Variants       bool           `json:"variants"` // more than one variant to choose from
	Variant        *LinkVariant   `json:"variant"`  // the chosen variant, if any
	Comment        string         `json:"comment"`
	Password       string         `json:"downloadPassword"`
	AddedDate      int64          `json:"addedDate"`
	AdvancedStatus AdvancedStatus `json:"advancedStatus"`
}

// LinkVariant is one variant of a link, for example an audio or video quality.
type LinkVariant struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	IconKey string `json:"iconKey"`
}

// GrabberPackageQuery selects linkgrabber packages and the fields to return.
// Without any field JDownloader returns only uuid and name.
type GrabberPackageQuery struct {
	BytesTotal       bool    `json:"bytesTotal,omitempty"`
	ChildCount       bool    `json:"childCount,omitempty"`
	Comment          bool    `json:"comment,omitempty"`
	Enabled          bool    `json:"enabled,omitempty"`
	Hosts            bool    `json:"hosts,omitempty"`
	Priority         bool    `json:"priority,omitempty"`
	SaveTo           bool    `json:"saveTo,omitempty"`
	Status           bool    `json:"status,omitempty"`
	OfflineCount     bool    `json:"availableOfflineCount,omitempty"`
	OnlineCount      bool    `json:"availableOnlineCount,omitempty"`
	TempUnknownCount bool    `json:"availableTempUnknownCount,omitempty"`
	UnknownCount     bool    `json:"availableUnknownCount,omitempty"`
	PackageIDs       []int64 `json:"packageUUIDs,omitempty"`
	MaxResults       int     `json:"maxResults,omitempty"`
	StartAt          int     `json:"startAt,omitempty"`
}

// AllGrabberPackageFields returns a query for all packages with every field.
func AllGrabberPackageFields() GrabberPackageQuery {
	return GrabberPackageQuery{
		BytesTotal: true, ChildCount: true, Comment: true, Enabled: true, Hosts: true, Priority: true,
		SaveTo: true, Status: true, OfflineCount: true, OnlineCount: true, TempUnknownCount: true,
		UnknownCount: true,
	}
}

// GrabberLinkQuery selects linkgrabber links and the fields to return.
// Without any field JDownloader returns only uuid, packageUUID and name.
type GrabberLinkQuery struct {
	AddedDate      bool    `json:"addedDate,omitempty"`
	AdvancedStatus bool    `json:"advancedStatus,omitempty"`
	Availability   bool    `json:"availability,omitempty"`
	BytesTotal     bool    `json:"bytesTotal,omitempty"`
	Comment        bool    `json:"comment,omitempty"`
	Enabled        bool    `json:"enabled,omitempty"`
	Host           bool    `json:"host,omitempty"`
	JobUUID        bool    `json:"jobUUID,omitempty"`
	Password       bool    `json:"password,omitempty"`
	Priority       bool    `json:"priority,omitempty"`
	Status         bool    `json:"status,omitempty"`
	URL            bool    `json:"url,omitempty"`
	Variants       bool    `json:"variants,omitempty"`
	VariantID      bool    `json:"variantID,omitempty"`
	VariantName    bool    `json:"variantName,omitempty"`
	VariantIcon    bool    `json:"variantIcon,omitempty"`
	LinkIDs        []int64 `json:"linkUUIDs,omitempty"`
	PackageIDs     []int64 `json:"packageUUIDs,omitempty"`
	JobIDs         []int64 `json:"jobUUIDs,omitempty"`
	MaxResults     int     `json:"maxResults,omitempty"`
	StartAt        int     `json:"startAt,omitempty"`
}

// AllGrabberLinkFields returns a query for all links with every field.
func AllGrabberLinkFields() GrabberLinkQuery {
	return GrabberLinkQuery{
		AddedDate: true, AdvancedStatus: true, Availability: true, BytesTotal: true, Comment: true,
		Enabled: true, Host: true, JobUUID: true, Password: true, Priority: true, Status: true,
		URL: true, Variants: true, VariantID: true, VariantName: true, VariantIcon: true,
	}
}

// AddLinksRequest is a request to collect links. Nil booleans keep the
// JDownloader default.
type AddLinksRequest struct {
	Links                    []string
	PackageName              string
	DestinationFolder        string
	ExtractPassword          string
	DownloadPassword         string
	Comment                  string
	Priority                 Priority
	Autostart                *bool
	AutoExtract              *bool
	DeepDecrypt              *bool
	OverwritePackagizerRules *bool
}

// addLinksQuery is the wire form of AddLinksRequest.
type addLinksQuery struct {
	Links                    string   `json:"links"`
	PackageName              string   `json:"packageName,omitempty"`
	DestinationFolder        string   `json:"destinationFolder,omitempty"`
	ExtractPassword          string   `json:"extractPassword,omitempty"`
	DownloadPassword         string   `json:"downloadPassword,omitempty"`
	Comment                  string   `json:"comment,omitempty"`
	Priority                 Priority `json:"priority,omitempty"`
	Autostart                *bool    `json:"autostart,omitempty"`
	AutoExtract              *bool    `json:"autoExtract,omitempty"`
	DeepDecrypt              *bool    `json:"deepDecrypt,omitempty"`
	OverwritePackagizerRules *bool    `json:"overwritePackagizerRules,omitempty"`
}

// CrawlerJob is a running or finished link crawler job.
type CrawlerJob struct {
	JobID     int64 `json:"jobId"`
	CrawlerID int64 `json:"crawlerId"`
	Crawling  bool  `json:"crawling"`
	Checking  bool  `json:"checking"`
	Crawled   int   `json:"crawled"`
	Filtered  int   `json:"filtered"`
	Broken    int   `json:"broken"`
	Unhandled int   `json:"unhandled"`
}

// Linkgrabber returns the operations the linkgrabber shares with the download list.
func (c *Client) Linkgrabber() ListOps { return ListOps{c, lgNamespace} }

// GrabberPackages queries the packages of the linkgrabber, in list order.
func (c *Client) GrabberPackages(ctx context.Context, q GrabberPackageQuery) (out []GrabberPackage, err error) {
	return out, c.Call(ctx, pathLGPackages, &out, q)
}

// GrabberLinks queries the links of the linkgrabber, in list order.
func (c *Client) GrabberLinks(ctx context.Context, q GrabberLinkQuery) (out []GrabberLink, err error) {
	return out, c.Call(ctx, pathLGLinks, &out, q)
}

// GrabberStructureChanged compares the structure counter of the linkgrabber
// with the last one seen (0 on the first call), see DownloadsStructureChanged.
func (c *Client) GrabberStructureChanged(ctx context.Context, last int64) (counter int64, changed bool, err error) {
	return structureChanged(ctx, c, pathLGStructure, last)
}

// AddLinks adds links to the linkgrabber and returns the id of the collecting job.
func (c *Client) AddLinks(ctx context.Context, r AddLinksRequest) (jobID int64, err error) {
	q := addLinksQuery{
		Links: strings.Join(r.Links, "\n"), PackageName: r.PackageName, DestinationFolder: r.DestinationFolder,
		ExtractPassword: r.ExtractPassword, DownloadPassword: r.DownloadPassword, Comment: r.Comment,
		Priority: r.Priority, Autostart: r.Autostart, AutoExtract: r.AutoExtract, DeepDecrypt: r.DeepDecrypt,
		OverwritePackagizerRules: r.OverwritePackagizerRules,
	}
	var job struct {
		ID int64 `json:"id"`
	}
	err = c.Call(ctx, pathLGAddLinks, &job, q)
	return job.ID, err
}

// AddContainer adds a container file (extension "DLC", "CCF", "RSDF", ...) and
// returns the id of the collecting job. JDownloader reads the content as a data URL.
func (c *Client) AddContainer(ctx context.Context, ext string, content []byte) (jobID int64, err error) {
	dataURL := "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(content)
	var job struct {
		ID int64 `json:"id"`
	}
	err = c.Call(ctx, pathLGAddContainer, &job, ext, dataURL)
	return job.ID, err
}

// MoveToDownloads moves links and packages from the linkgrabber to the download list.
func (c *Client) MoveToDownloads(ctx context.Context, linkIDs, pkgIDs []int64) error {
	return c.Call(ctx, pathLGToDownloads, nil, ids(linkIDs), ids(pkgIDs))
}

// ClearLinkgrabber removes everything from the linkgrabber.
func (c *Client) ClearLinkgrabber(ctx context.Context) error {
	return c.Call(ctx, pathLGClear, nil)
}

// AbortCollecting aborts all running collecting jobs.
func (c *Client) AbortCollecting(ctx context.Context) error {
	return c.Call(ctx, pathLGAbort, nil)
}

// IsCollecting reports whether the linkgrabber is collecting links.
func (c *Client) IsCollecting(ctx context.Context) (b bool, err error) {
	return b, c.Call(ctx, pathLGIsCollecting, &b)
}

// IsCrawling reports whether a link crawler is running.
func (c *Client) IsCrawling(ctx context.Context) (b bool, err error) {
	return b, c.Call(ctx, pathLinkCrawling, &b)
}

// CrawlerJobs returns the state of the given collecting jobs. JDownloader
// answers nothing for a job that does not exist (any more).
func (c *Client) CrawlerJobs(ctx context.Context, jobIDs []int64) (out []CrawlerJob, err error) {
	q := struct {
		CollectorInfo bool    `json:"collectorInfo"`
		JobIDs        []int64 `json:"jobIds"`
	}{true, ids(jobIDs)}
	return out, c.Call(ctx, pathLGCrawlerJobs, &out, q)
}

// Variants returns the variants of a link, empty if it has none.
func (c *Client) Variants(ctx context.Context, linkID int64) (out []LinkVariant, err error) {
	return out, c.Call(ctx, pathLGVariants, &out, linkID)
}

// SetVariant switches a link to a variant.
func (c *Client) SetVariant(ctx context.Context, linkID int64, variantID string) error {
	return c.Call(ctx, pathLGSetVariant, nil, linkID, variantID)
}

// AddVariantCopy adds a variant of a link as a new link behind afterLinkID in package destPkgID.
func (c *Client) AddVariantCopy(ctx context.Context, linkID, afterLinkID, destPkgID int64, variantID string) error {
	return c.Call(ctx, pathLGAddVariant, nil, linkID, afterLinkID, destPkgID, variantID)
}

// DownloadFolderHistory returns the directories used recently.
func (c *Client) DownloadFolderHistory(ctx context.Context) (out []string, err error) {
	return out, c.Call(ctx, pathLGFolders, &out)
}
