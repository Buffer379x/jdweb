package state

import (
	"bytes"
	"encoding/json"
	"strconv"

	"github.com/Buffer379x/jdweb/internal/jd"
)

// List names one of the two lists of JDownloader.
type List string

// The two lists, spelled as in the patch and structure events of docs/api.md.
const (
	Downloads   List = "downloads"
	Linkgrabber List = "linkgrabber"
)

// ETA is the remaining time in seconds. In JSON ETAUnknown is null.
type ETA int64

// ETAUnknown is an eta nobody can tell.
const ETAUnknown ETA = -1

// NewETA turns the value of JDownloader into an ETA: every negative value
// (-1, -2147483648) means unknown.
func NewETA(raw int64) ETA {
	if s, ok := jd.ETASeconds(raw); ok {
		return ETA(s)
	}
	return ETAUnknown
}

// MarshalJSON writes a number, or null when unknown.
func (e ETA) MarshalJSON() ([]byte, error) {
	if e < 0 {
		return []byte("null"), nil
	}
	return strconv.AppendInt(nil, int64(e), 10), nil
}

// UnmarshalJSON reads what MarshalJSON writes.
func (e *ETA) UnmarshalJSON(b []byte) error {
	if bytes.Equal(b, []byte("null")) {
		*e = ETAUnknown
		return nil
	}
	var n int64
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*e = NewETA(n)
	return nil
}

// LinkState is the state derived from the other fields of a link, see derive.go.
type LinkState string

// The link states of docs/api.md.
const (
	LinkDisabled     LinkState = "disabled"
	LinkExtractError LinkState = "extractError"
	LinkFailed       LinkState = "failed"
	LinkFinished     LinkState = "finished"
	LinkSkipped      LinkState = "skipped"
	LinkRunning      LinkState = "running"
	LinkWaiting      LinkState = "waiting"
)

// DlPackage is a package of the download list.
type DlPackage struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Hosts            []string `json:"hosts"`
	ChildCount       int      `json:"childCount"`
	BytesTotal       int64    `json:"bytesTotal"`
	BytesLoaded      int64    `json:"bytesLoaded"`
	Speed            int64    `json:"speed"`
	ETA              ETA      `json:"eta"`
	Enabled          bool     `json:"enabled"`
	Finished         bool     `json:"finished"`
	Running          bool     `json:"running"`
	SaveTo           string   `json:"saveTo"`
	Status           string   `json:"status"`
	Priority         string   `json:"priority"`
	Comment          string   `json:"comment"`
	DownloadPassword string   `json:"downloadPassword"`
}

// DlLink is a link of the download list.
type DlLink struct {
	ID               string    `json:"id"`
	PackageID        string    `json:"packageId"`
	Name             string    `json:"name"`
	Host             string    `json:"host"`
	URL              string    `json:"url"`
	BytesTotal       int64     `json:"bytesTotal"`
	BytesLoaded      int64     `json:"bytesLoaded"`
	Speed            int64     `json:"speed"`
	ETA              ETA       `json:"eta"`
	Enabled          bool      `json:"enabled"`
	Finished         bool      `json:"finished"`
	Running          bool      `json:"running"`
	Skipped          bool      `json:"skipped"`
	SkipReason       string    `json:"skipReason"`
	FinalState       string    `json:"finalState"`
	ExtractionStatus string    `json:"extractionStatus"`
	Status           string    `json:"status"`
	Priority         string    `json:"priority"`
	Comment          string    `json:"comment"`
	DownloadPassword string    `json:"downloadPassword"`
	AddedDate        int64     `json:"addedDate"`
	FinishedDate     int64     `json:"finishedDate"`
	State            LinkState `json:"state"`
}

// LgPackage is a package of the linkgrabber.
type LgPackage struct {
	ID               string   `json:"id"`
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
	Priority         string   `json:"priority"`
	Comment          string   `json:"comment"`
	DownloadPassword string   `json:"downloadPassword"`
}

// LgLink is a link of the linkgrabber.
type LgLink struct {
	ID               string          `json:"id"`
	PackageID        string          `json:"packageId"`
	Name             string          `json:"name"`
	Host             string          `json:"host"`
	URL              string          `json:"url"`
	BytesTotal       int64           `json:"bytesTotal"`
	Availability     string          `json:"availability"`
	Enabled          bool            `json:"enabled"`
	Priority         string          `json:"priority"`
	Variants         bool            `json:"variants"`
	Variant          *jd.LinkVariant `json:"variant"`
	Comment          string          `json:"comment"`
	DownloadPassword string          `json:"downloadPassword"`
}

// Connection is the state of the connection to JDownloader.
type Connection string

// The connection states of docs/api.md.
const (
	ConnConnected     Connection = "connected"
	ConnSyncing       Connection = "syncing"
	ConnOffline       Connection = "offline"
	ConnMisconfigured Connection = "misconfigured"
)

// Status is the answer of GET /status and the payload of the status event.
// It is comparable, so a change is found with !=.
type Status struct {
	Connection       Connection     `json:"connection"`
	ConnectionDetail string         `json:"connectionDetail"`
	Controller       string         `json:"controller"`
	Speed            int64          `json:"speed"`
	Downloads        DownloadTotals `json:"downloads"`
	Linkgrabber      GrabberTotals  `json:"linkgrabber"`
	Captchas         int            `json:"captchas"`
	Dialogs          int            `json:"dialogs"`
	Toggles          Toggles        `json:"toggles"`
	JD               JDInfo         `json:"jd"`
}

// DownloadTotals are the sums over the download list.
type DownloadTotals struct {
	Packages    int   `json:"packages"`
	Links       int   `json:"links"`
	BytesTotal  int64 `json:"bytesTotal"`
	BytesLoaded int64 `json:"bytesLoaded"`
	Running     int   `json:"running"`
	ETA         ETA   `json:"eta"`
}

// GrabberTotals are the sums over the linkgrabber.
type GrabberTotals struct {
	Packages   int   `json:"packages"`
	Links      int   `json:"links"`
	BytesTotal int64 `json:"bytesTotal"`
	Crawling   bool  `json:"crawling"`
}

// Toggles are the switches of JDownloader. LimitBytes is the configured limit
// in bytes per second, whether the limit is on or not.
type Toggles struct {
	Limit      bool  `json:"limit"`
	LimitBytes int64 `json:"limitBytes"`
	Reconnect  bool  `json:"reconnect"`
	Premium    bool  `json:"premium"`
	Clipboard  bool  `json:"clipboard"`
	StopAfter  bool  `json:"stopAfter"`
}

// JDInfo says which JDownloader this is.
type JDInfo struct {
	Revision int64  `json:"revision"`
	Docker   bool   `json:"docker"`
	URL      string `json:"url"`
}

// NormalizeController maps the raw controller state of JDownloader to the four
// states of docs/api.md: STOPPED_STATE is IDLE, and so is a state nobody knows yet.
func NormalizeController(raw jd.ControllerState) string {
	switch raw {
	case jd.ControllerRunning, jd.ControllerPause, jd.ControllerStopping:
		return string(raw)
	}
	return string(jd.ControllerIdle)
}

// Connection details shared by the sync engine and the setup check.

// DetailOtherService is the hint when something answers that is not JDownloader.
const DetailOtherService = "another service answers here"

// DetailNoURL is the hint before a JDownloader address was set.
const DetailNoURL = "no JDownloader address is set"

// DetailNotReachable is the hint when nothing answers at an address that never
// worked: most often the API is still off in JDownloader, or the port is wrong.
func DetailNotReachable(url string) string {
	return "nothing answers at " + url + ". In the advanced settings of JDownloader set " +
		"RemoteAPIConfig DeprecatedApiEnabled to true and DeprecatedApiLocalhostOnly to false, " +
		"restart JDownloader and check the address and port."
}

// DetailOffline is the hint when an address that worked stopped answering.
func DetailOffline(url string) string {
	return "JDownloader at " + url + " does not answer, retrying"
}

// DetailFailing is the hint when JDownloader answers, but with errors.
func DetailFailing(url string) string {
	return "JDownloader at " + url + " reports errors, retrying"
}

func id(n int64) string { return strconv.FormatInt(n, 10) }

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func newDlPackage(p jd.DownloadPackage) DlPackage {
	out := DlPackage{
		ID: id(p.UUID), Name: p.Name, Hosts: orEmpty(p.Hosts), ChildCount: p.ChildCount, BytesTotal: p.BytesTotal,
		BytesLoaded: p.BytesLoaded, Speed: max(p.Speed, 0), ETA: NewETA(p.ETA), Enabled: p.Enabled, Finished: p.Finished,
		Running: p.Running, SaveTo: p.SaveTo, Status: p.Status, Priority: string(p.Priority), Comment: p.Comment,
		DownloadPassword: p.Password,
	}
	if !out.Running {
		out.Speed, out.ETA = 0, ETAUnknown
	}
	return out
}

func newDlLink(l jd.DownloadLink) DlLink {
	out := DlLink{
		ID: id(l.UUID), PackageID: id(l.PackageUUID), Name: l.Name, Host: l.Host, URL: l.URL, BytesTotal: l.BytesTotal,
		BytesLoaded: l.BytesLoaded, Speed: max(l.Speed, 0), ETA: NewETA(l.ETA), Enabled: l.Enabled, Finished: l.Finished,
		Running: l.Running, Skipped: l.Skipped, SkipReason: string(l.AdvancedStatus.SkipReason()), FinalState: l.AdvancedStatus.FinalLinkState(),
		ExtractionStatus: l.ExtractionStatus, Status: l.Status, Priority: string(l.Priority), Comment: l.Comment,
		DownloadPassword: l.Password, AddedDate: l.AddedDate, FinishedDate: l.FinishedDate,
	}
	if !out.Running {
		out.Speed, out.ETA = 0, ETAUnknown
	}
	out.State = deriveLinkState(out)
	return out
}

func newLgPackage(p jd.GrabberPackage) LgPackage {
	return LgPackage{
		ID: id(p.UUID), Name: p.Name, Hosts: orEmpty(p.Hosts), ChildCount: p.ChildCount, BytesTotal: p.BytesTotal,
		SaveTo: p.SaveTo, OnlineCount: p.OnlineCount, OfflineCount: p.OfflineCount, UnknownCount: p.UnknownCount,
		TempUnknownCount: p.TempUnknownCount, Enabled: p.Enabled, Priority: string(p.Priority), Comment: p.Comment,
		DownloadPassword: p.Password,
	}
}

func newLgLink(l jd.GrabberLink) LgLink {
	return LgLink{
		ID: id(l.UUID), PackageID: id(l.PackageUUID), Name: l.Name, Host: l.Host, URL: l.URL, BytesTotal: l.BytesTotal,
		Availability: string(l.Availability), Enabled: l.Enabled, Priority: string(l.Priority), Variants: l.Variants,
		Variant: l.Variant, Comment: l.Comment, DownloadPassword: l.Password,
	}
}
