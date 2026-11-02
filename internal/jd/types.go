package jd

import (
	"encoding/json"
	"strings"
)

// Priority of a link or package.
type Priority string

const (
	PriorityHighest Priority = "HIGHEST"
	PriorityHigher  Priority = "HIGHER"
	PriorityHigh    Priority = "HIGH"
	PriorityDefault Priority = "DEFAULT"
	PriorityLow     Priority = "LOW"
	PriorityLower   Priority = "LOWER"
	PriorityLowest  Priority = "LOWEST"
)

// CleanupAction selects what cleanup removes.
type CleanupAction string

const (
	CleanupDeleteAll      CleanupAction = "DELETE_ALL"
	CleanupDeleteDisabled CleanupAction = "DELETE_DISABLED"
	CleanupDeleteFailed   CleanupAction = "DELETE_FAILED"
	CleanupDeleteFinished CleanupAction = "DELETE_FINISHED"
	CleanupDeleteOffline  CleanupAction = "DELETE_OFFLINE"
	CleanupDeleteDupe     CleanupAction = "DELETE_DUPE"
	CleanupDeleteMode     CleanupAction = "DELETE_MODE"
)

// CleanupMode says what happens to files on disk.
type CleanupMode string

const (
	CleanupRemoveLinksOnly            CleanupMode = "REMOVE_LINKS_ONLY"
	CleanupRemoveLinksAndRecycleFiles CleanupMode = "REMOVE_LINKS_AND_RECYCLE_FILES"
	CleanupRemoveLinksAndDeleteFiles  CleanupMode = "REMOVE_LINKS_AND_DELETE_FILES"
)

// SelectionType says which of the given ids cleanup works on.
type SelectionType string

const (
	SelectionSelected   SelectionType = "SELECTED"
	SelectionUnselected SelectionType = "UNSELECTED"
	SelectionAll        SelectionType = "ALL"
	SelectionNone       SelectionType = "NONE"
)

// SkipRequest is how a captcha is skipped.
type SkipRequest string

const (
	SkipSingle            SkipRequest = "SINGLE"
	SkipBlockHoster       SkipRequest = "BLOCK_HOSTER"
	SkipBlockAllCaptchas  SkipRequest = "BLOCK_ALL_CAPTCHAS"
	SkipBlockPackage      SkipRequest = "BLOCK_PACKAGE"
	SkipRefresh           SkipRequest = "REFRESH"
	SkipStopCurrentAction SkipRequest = "STOP_CURRENT_ACTION"
	SkipTimeout           SkipRequest = "TIMEOUT"
)

// SkipReason is why a link was skipped. JDownloader reports it for a skipped
// link in AdvancedStatus["SkipReason"].ID.
type SkipReason string

const (
	SkipReasonConnectionUnavailable SkipReason = "CONNECTION_UNAVAILABLE"
	SkipReasonTooManyRetries        SkipReason = "TOO_MANY_RETRIES"
	SkipReasonCaptcha               SkipReason = "CAPTCHA"
	SkipReasonManual                SkipReason = "MANUAL"
	SkipReasonDiskFull              SkipReason = "DISK_FULL"
	SkipReasonNoAccount             SkipReason = "NO_ACCOUNT"
	SkipReasonInvalidDestination    SkipReason = "INVALID_DESTINATION"
	SkipReasonFileExists            SkipReason = "FILE_EXISTS"
	SkipReasonUpdateRestartRequired SkipReason = "UPDATE_RESTART_REQUIRED"
	SkipReasonFFmpegMissing         SkipReason = "FFMPEG_MISSING"
	SkipReasonFFprobeMissing        SkipReason = "FFPROBE_MISSING"
)

// Availability of a link in the linkgrabber.
type Availability string

const (
	AvailabilityOnline      Availability = "ONLINE"
	AvailabilityOffline     Availability = "OFFLINE"
	AvailabilityUnknown     Availability = "UNKNOWN"
	AvailabilityTempUnknown Availability = "TEMP_UNKNOWN"
)

// ControllerState is the raw state of the download controller as JDownloader
// reports it (downloadcontroller/getCurrentState and toolbar/getStatus).
//
// docs/api.md knows only IDLE, RUNNING, PAUSE and STOPPING. JDownloader also
// answers STOPPED_STATE after a stop (observed in revision 50639; a fresh
// start says IDLE, a stop in progress STOPPING). The API layer has to map the
// raw state; ControllerStates lists every value to map. This package does no
// mapping.
type ControllerState string

const (
	ControllerIdle     ControllerState = "IDLE"
	ControllerRunning  ControllerState = "RUNNING"
	ControllerPause    ControllerState = "PAUSE"
	ControllerStopping ControllerState = "STOPPING"
	ControllerStopped  ControllerState = "STOPPED_STATE"
)

// ControllerStates returns every raw controller state the client knows, in the
// order IDLE, RUNNING, PAUSE, STOPPING, STOPPED_STATE. A state outside this
// list is new in JDownloader and has to be treated as unknown.
func ControllerStates() []ControllerState {
	return []ControllerState{ControllerIdle, ControllerRunning, ControllerPause, ControllerStopping, ControllerStopped}
}

// UrlDisplayType selects which url of a link getDownloadUrls returns.
type UrlDisplayType string

const (
	UrlCustom    UrlDisplayType = "CUSTOM"
	UrlReferrer  UrlDisplayType = "REFERRER"
	UrlOrigin    UrlDisplayType = "ORIGIN"
	UrlContainer UrlDisplayType = "CONTAINER"
	UrlContent   UrlDisplayType = "CONTENT"
)

// BasicAuthType is the kind of a basic authentication entry.
type BasicAuthType string

const (
	BasicAuthFTP  BasicAuthType = "FTP"
	BasicAuthHTTP BasicAuthType = "HTTP"
)

// The event publishers JDWeb subscribes to. These and no others: linkgrabberv2
// is not a publisher, although subscribe accepts the name without complaint.
const (
	PublisherDownloads     = "downloads"
	PublisherLinkCollector = "linkcollector"
	PublisherLinkCrawler   = "linkcrawler"
	PublisherWatchdog      = "downloadwatchdog"
	PublisherCaptchas      = "captchas"
	PublisherDialogs       = "dialogs"
)

// Publishers returns the publishers to subscribe to.
func Publishers() []string {
	return []string{
		PublisherDownloads, PublisherLinkCollector, PublisherLinkCrawler,
		PublisherWatchdog, PublisherCaptchas, PublisherDialogs,
	}
}

// ETAUnknown is the eta JDownloader reports for packages when it does not know.
const ETAUnknown int64 = -2147483648

// ETASeconds returns the eta in seconds. ok is false when it is unknown: the
// value is negative (ETAUnknown for packages, -1 for finished links).
func ETASeconds(eta int64) (seconds int64, ok bool) {
	if eta < 0 {
		return 0, false
	}
	return eta, true
}

// StatusItem is one entry of AdvancedStatus, for example "SkipReason",
// "FinalLinkState", "ExtractionStatus" or "PluginProgress". Current, Total and
// ETA are only set for PluginProgress.
type StatusItem struct {
	ID      string `json:"id,omitempty"`
	Label   string `json:"label,omitempty"`
	IconKey string `json:"iconKey,omitempty"`
	Current int64  `json:"current,omitempty"`
	Total   int64  `json:"total,omitempty"`
	ETA     int64  `json:"eta,omitempty"`
}

// AdvancedStatus is the machine readable status of a link, by item name.
type AdvancedStatus map[string]StatusItem

// FinalLinkState returns how the download of a link ended, as JDownloader names it
// (FINISHED, OFFLINE, ...), or "" while it has not ended. It is the machine
// readable verdict; the text of Status is for people and is never evaluated.
func (a AdvancedStatus) FinalLinkState() string { return a["FinalLinkState"].ID }

// FinalLinkStateFinished reports whether a final state is one of the successful ones.
func FinalLinkStateFinished(id string) bool { return strings.HasPrefix(id, "FINISHED") }

// SkipReason returns the reason of a skipped link, or "".
func (a AdvancedStatus) SkipReason() SkipReason { return SkipReason(a["SkipReason"].ID) }

// Event is one item of events/listen. EventData is any JSON object or null.
type Event struct {
	Publisher string          `json:"publisher"`
	EventID   string          `json:"eventid"`
	EventData json.RawMessage `json:"eventdata,omitempty"`
}
