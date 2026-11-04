package jd

import "context"

const (
	pathExtQueue    = "/extraction/getQueue"
	pathExtStart    = "/extraction/startExtractionNow"
	pathExtCancel   = "/extraction/cancelExtraction"
	pathExtInfo     = "/extraction/getArchiveInfo"
	pathExtGetSet   = "/extraction/getArchiveSettings"
	pathExtSetSet   = "/extraction/setArchiveSettings"
	pathExtPassword = "/extraction/addArchivePassword"
)

// ArchiveStatus is an archive and its extraction state.
type ArchiveStatus struct {
	ArchiveID        string            `json:"archiveId"`
	ArchiveName      string            `json:"archiveName"`
	Type             string            `json:"type"`             // for example ZIP_SINGLE, RAR_MULTI
	ControllerID     int64             `json:"controllerId"`     // -1 without a running controller
	ControllerStatus string            `json:"controllerStatus"` // RUNNING, QUEUED, NA
	States           map[string]string `json:"states"`           // part file name to COMPLETE, INCOMPLETE, MISSING
}

// ArchiveSettings are the settings of one archive. Nil booleans keep the
// default. Empty ExtractPath is not sent and keeps the stored value; a nil FinalPassword
// keeps it, an empty one clears it (sent explicitly). Passwords
// replaces the whole list, an empty list clears it.
type ArchiveSettings struct {
	ArchiveID                       string   `json:"archiveId"`
	ExtractPath                     string   `json:"extractPath,omitempty"`
	FinalPassword                   *string  `json:"finalPassword,omitempty"`
	Passwords                       []string `json:"passwords"`
	AutoExtract                     *bool    `json:"autoExtract,omitempty"`
	RemoveDownloadLinksAfterExtract *bool    `json:"removeDownloadLinksAfterExtraction,omitempty"`
	RemoveFilesAfterExtraction      *bool    `json:"removeFilesAfterExtraction,omitempty"`
}

// ExtractionQueue returns the archives being extracted or waiting for it.
func (c *Client) ExtractionQueue(ctx context.Context) (out []ArchiveStatus, err error) {
	return out, c.Call(ctx, pathExtQueue, &out)
}

// ExtractionStart extracts the archives of links and packages now. The result
// maps a controller id to whether it started.
func (c *Client) ExtractionStart(ctx context.Context, linkIDs, pkgIDs []int64) (out map[string]bool, err error) {
	return out, c.Call(ctx, pathExtStart, &out, ids(linkIDs), ids(pkgIDs))
}

// ExtractionCancel cancels the extraction of a controller. ok is false if
// there is no such controller.
func (c *Client) ExtractionCancel(ctx context.Context, controllerID int64) (ok bool, err error) {
	return ok, c.Call(ctx, pathExtCancel, &ok, controllerID)
}

// Archives returns the archives of links and packages.
func (c *Client) Archives(ctx context.Context, linkIDs, pkgIDs []int64) (out []ArchiveStatus, err error) {
	return out, c.Call(ctx, pathExtInfo, &out, ids(linkIDs), ids(pkgIDs))
}

// ArchiveSettings returns the settings of archives by archive id.
func (c *Client) ArchiveSettings(ctx context.Context, archiveIDs []string) (out []ArchiveSettings, err error) {
	if archiveIDs == nil {
		archiveIDs = []string{}
	}
	return out, c.Call(ctx, pathExtGetSet, &out, archiveIDs)
}

// SetArchiveSettings changes the settings of an archive.
func (c *Client) SetArchiveSettings(ctx context.Context, s ArchiveSettings) error {
	if s.Passwords == nil {
		s.Passwords = []string{}
	}
	var ok bool
	if err := c.Call(ctx, pathExtSetSet, &ok, s.ArchiveID, s); err != nil {
		return err
	}
	if !ok {
		return ErrRejected
	}
	return nil
}

// AddArchivePassword adds a password to the list JDownloader tries.
func (c *Client) AddArchivePassword(ctx context.Context, password string) error {
	return c.Call(ctx, pathExtPassword, nil, password)
}
