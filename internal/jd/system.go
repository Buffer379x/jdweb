package jd

import "context"

const (
	pathPing          = "/device/ping"
	pathCoreRevision  = "/jd/getCoreRevision"
	pathUptime        = "/jd/uptime"
	pathSystemInfos   = "/system/getSystemInfos"
	pathStorageInfos  = "/system/getStorageInfos"
	pathRestartJD     = "/system/restartJD"
	pathExitJD        = "/system/exitJD"
	pathShutdownOS    = "/system/shutdownOS"
	pathHibernateOS   = "/system/hibernateOS"
	pathStandbyOS     = "/system/standbyOS"
	pathReconnect     = "/reconnect/doReconnect"
	pathUpdateAvail   = "/update/isUpdateAvailable"
	pathUpdateCheck   = "/update/runUpdateCheck"
	pathUpdateRestart = "/update/restartAndUpdate"
	pathToolbarStatus = "/toolbar/getStatus"
	pathTogglePremium = "/toolbar/togglePremium"
	pathToggleClip    = "/toolbar/toggleClipboardMonitoring"
	pathToggleRecon   = "/toolbar/toggleAutomaticReconnect"
	pathToggleStop    = "/toolbar/toggleStopAfterCurrentDownload"
	pathToggleLimit   = "/toolbar/toggleDownloadSpeedLimit"
)

// SystemInfo describes the machine and the Java runtime of JDownloader.
type SystemInfo struct {
	Docker           bool   `json:"docker"`
	Headless         bool   `json:"headless"`
	Snap             bool   `json:"snap"`
	OSFamily         string `json:"osFamily"`
	OSString         string `json:"osString"`
	OperatingSystem  string `json:"operatingSystem"`
	ArchString       string `json:"archString"`
	JavaVersion      string `json:"javaVersionString"`
	JavaName         string `json:"javaName"`
	JavaVendor       string `json:"javaVendor"`
	HeapUsed         int64  `json:"heapUsed"`
	HeapCommitted    int64  `json:"heapCommitted"`
	HeapMax          int64  `json:"heapMax"`
	StartupTimestamp int64  `json:"startupTimeStamp"` // Unix milliseconds
}

// StorageInfo is the free and total space of a path. Error is set when the path cannot be read.
type StorageInfo struct {
	Path  string `json:"path"`
	Free  int64  `json:"free"`
	Size  int64  `json:"size"`
	Error string `json:"error"`
}

// ToolbarStatus is the compact status JDownloader shows in its toolbar.
type ToolbarStatus struct {
	State   ControllerState `json:"state"`
	Running bool            `json:"running"`
	Pause   bool            `json:"pause"`
	Speed   int64           `json:"speed"`
	Limit   bool            `json:"limit"`
	// LimitSpeed is in bytes per second in revision 50639 (not KiB/s), the same
	// number as the config DownloadSpeedLimit while the limit is on. It is 0
	// while the limit is off. During a pause it shows the pause speed, so it
	// is not the configured limit: read that with SpeedLimit.
	LimitSpeed       int64 `json:"limitspeed"`
	Premium          bool  `json:"premium"`
	Reconnect        bool  `json:"reconnect"`
	Clipboard        bool  `json:"clipboard"`
	StopAfter        bool  `json:"stopafter"`
	DownloadCurrent  int64 `json:"download_current"`
	DownloadComplete int64 `json:"download_complete"`
}

// Ping returns true when JDownloader answers.
func (c *Client) Ping(ctx context.Context) (ok bool, err error) {
	return ok, c.Call(ctx, pathPing, &ok)
}

// CoreRevision returns the revision of the JDownloader core, for example 50639.
func (c *Client) CoreRevision(ctx context.Context) (rev int64, err error) {
	return rev, c.Call(ctx, pathCoreRevision, &rev)
}

// Uptime returns the milliseconds JDownloader has been running.
func (c *Client) Uptime(ctx context.Context) (ms int64, err error) {
	return ms, c.Call(ctx, pathUptime, &ms)
}

// SystemInfos returns information about the machine and Java.
func (c *Client) SystemInfos(ctx context.Context) (s SystemInfo, err error) {
	return s, c.Call(ctx, pathSystemInfos, &s)
}

// StorageInfos returns the free space of a path.
func (c *Client) StorageInfos(ctx context.Context, path string) (out []StorageInfo, err error) {
	return out, c.Call(ctx, pathStorageInfos, &out, path)
}

// RestartJD restarts JDownloader. The connection may drop before the answer.
func (c *Client) RestartJD(ctx context.Context) error { return c.Call(ctx, pathRestartJD, nil) }

// ExitJD quits JDownloader. The connection may drop before the answer.
func (c *Client) ExitJD(ctx context.Context) error { return c.Call(ctx, pathExitJD, nil) }

// ShutdownOS shuts the machine of JDownloader down. Never use it when SystemInfo.Docker is set.
func (c *Client) ShutdownOS(ctx context.Context, force bool) error {
	return c.Call(ctx, pathShutdownOS, nil, force)
}

// HibernateOS hibernates the machine of JDownloader.
func (c *Client) HibernateOS(ctx context.Context) error { return c.Call(ctx, pathHibernateOS, nil) }

// StandbyOS puts the machine of JDownloader into standby.
func (c *Client) StandbyOS(ctx context.Context) error { return c.Call(ctx, pathStandbyOS, nil) }

// Reconnect triggers a reconnect of the router.
func (c *Client) Reconnect(ctx context.Context) error { return c.Call(ctx, pathReconnect, nil) }

// UpdateAvailable reports whether a JDownloader update is waiting.
func (c *Client) UpdateAvailable(ctx context.Context) (b bool, err error) {
	return b, c.Call(ctx, pathUpdateAvail, &b)
}

// CheckForUpdate starts an update check. Ask UpdateAvailable a little later.
func (c *Client) CheckForUpdate(ctx context.Context) error { return c.Call(ctx, pathUpdateCheck, nil) }

// RestartAndUpdate restarts JDownloader and installs the waiting update.
func (c *Client) RestartAndUpdate(ctx context.Context) error {
	return c.Call(ctx, pathUpdateRestart, nil)
}

// Toolbar returns the toolbar status.
func (c *Client) Toolbar(ctx context.Context) (s ToolbarStatus, err error) {
	return s, c.Call(ctx, pathToolbarStatus, &s)
}

// The toggles return the new state of the switch.
//
// TogglePremium: switching premium off makes JDownloader ask for confirmation
// in a dialog and the call does not answer until the dialog is answered, so it
// runs into the time limit. Answer the dialog (see DialogAnswer) or avoid
// switching premium off from here.

// TogglePremium switches the use of premium accounts.
func (c *Client) TogglePremium(ctx context.Context) (on bool, err error) {
	return on, c.Call(ctx, pathTogglePremium, &on)
}

// ToggleClipboard switches the clipboard monitoring.
func (c *Client) ToggleClipboard(ctx context.Context) (on bool, err error) {
	return on, c.Call(ctx, pathToggleClip, &on)
}

// ToggleReconnect switches the automatic reconnect.
func (c *Client) ToggleReconnect(ctx context.Context) (on bool, err error) {
	return on, c.Call(ctx, pathToggleRecon, &on)
}

// ToggleStopAfter switches "stop after the current download".
func (c *Client) ToggleStopAfter(ctx context.Context) (on bool, err error) {
	return on, c.Call(ctx, pathToggleStop, &on)
}

// ToggleSpeedLimit switches the speed limit.
func (c *Client) ToggleSpeedLimit(ctx context.Context) (on bool, err error) {
	return on, c.Call(ctx, pathToggleLimit, &on)
}
