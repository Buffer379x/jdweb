package jd_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Buffer379x/jdweb/internal/jd"
)

// TestMethodsSendDocumentedParams checks, for every typed method that takes
// parameters, the path and the order and types of the params. The order is
// the one of the JDownloader reference (rev 50639).
func TestMethodsSendDocumentedParams(t *testing.T) {
	ctx := context.Background()
	yes := true
	tests := []struct {
		name   string
		call   func(c *jd.Client) error
		path   string
		params string
	}{
		// download list
		{"DownloadPackages", func(c *jd.Client) error {
			_, err := c.DownloadPackages(ctx, jd.DownloadPackageQuery{BytesTotal: true, Speed: true, PackageIDs: []int64{7}, MaxResults: 5, StartAt: 2})
			return err
		}, "/downloadsV2/queryPackages", `[{"bytesTotal":true,"speed":true,"packageUUIDs":[7],"maxResults":5,"startAt":2}]`},
		{"DownloadLinks", func(c *jd.Client) error {
			_, err := c.DownloadLinks(ctx, jd.DownloadLinkQuery{URL: true, ETA: true, LinkIDs: []int64{1, 2}})
			return err
		}, "/downloadsV2/queryLinks", `[{"eta":true,"url":true,"linkUUIDs":[1,2]}]`},
		{"DownloadsStructureChanged", func(c *jd.Client) error { _, _, err := c.DownloadsStructureChanged(ctx, 42); return err }, "/downloadsV2/getStructureChangeCounter", `[42]`},
		{"Downloads.SetEnabled", func(c *jd.Client) error { return c.Downloads().SetEnabled(ctx, false, []int64{1}, []int64{2}) }, "/downloadsV2/setEnabled", `[false,[1],[2]]`},
		{"Downloads.Remove", func(c *jd.Client) error { return c.Downloads().Remove(ctx, []int64{1}, nil) }, "/downloadsV2/removeLinks", `[[1],[]]`},
		{"Downloads.SetPriority", func(c *jd.Client) error {
			return c.Downloads().SetPriority(ctx, jd.PriorityHighest, []int64{1}, []int64{2})
		}, "/downloadsV2/setPriority", `["HIGHEST",[1],[2]]`},
		{"Downloads.RenameLink", func(c *jd.Client) error { return c.Downloads().RenameLink(ctx, 5, "n") }, "/downloadsV2/renameLink", `[5,"n"]`},
		{"Downloads.RenamePackage", func(c *jd.Client) error { return c.Downloads().RenamePackage(ctx, 6, "p") }, "/downloadsV2/renamePackage", `[6,"p"]`},
		{"Downloads.SetDirectory", func(c *jd.Client) error { return c.Downloads().SetDirectory(ctx, "/d", []int64{6}) }, "/downloadsV2/setDownloadDirectory", `["/d",[6]]`},
		{"Downloads.SetPassword", func(c *jd.Client) error { return c.Downloads().SetPassword(ctx, "pw", []int64{1}, []int64{2}) }, "/downloadsV2/setDownloadPassword", `[[1],[2],"pw"]`},
		{"Downloads.MoveToNewPackage", func(c *jd.Client) error {
			return c.Downloads().MoveToNewPackage(ctx, []int64{1}, []int64{2}, "new", "/dir")
		}, "/downloadsV2/movetoNewPackage", `[[1],[2],"new","/dir"]`},
		{"Downloads.MoveLinks", func(c *jd.Client) error { return c.Downloads().MoveLinks(ctx, []int64{1, 2}, -1, 9) }, "/downloadsV2/moveLinks", `[[1,2],-1,9]`},
		{"Downloads.MovePackages", func(c *jd.Client) error { return c.Downloads().MovePackages(ctx, []int64{3}, 4) }, "/downloadsV2/movePackages", `[[3],4]`},
		{"Downloads.SplitByHoster", func(c *jd.Client) error { return c.Downloads().SplitByHoster(ctx, nil, []int64{2}) }, "/downloadsV2/splitPackageByHoster", `[[],[2]]`},
		{"Downloads.CheckOnline", func(c *jd.Client) error { return c.Downloads().CheckOnline(ctx, []int64{1}, nil) }, "/downloadsV2/startOnlineStatusCheck", `[[1],[]]`},
		{"Downloads.URLs", func(c *jd.Client) error {
			_, err := c.Downloads().URLs(ctx, []int64{1}, nil, jd.UrlContent, jd.UrlOrigin)
			return err
		}, "/downloadsV2/getDownloadUrls", `[[1],[],["CONTENT","ORIGIN"]]`},
		{"Downloads.Cleanup", func(c *jd.Client) error {
			return c.Downloads().Cleanup(ctx, []int64{1}, []int64{2}, jd.CleanupDeleteFinished, jd.CleanupRemoveLinksAndDeleteFiles, jd.SelectionSelected)
		}, "/downloadsV2/cleanup", `[[1],[2],"DELETE_FINISHED","REMOVE_LINKS_AND_DELETE_FILES","SELECTED"]`},
		{"DownloadsForce", func(c *jd.Client) error { return c.DownloadsForce(ctx, []int64{1}, nil) }, "/downloadsV2/forceDownload", `[[1],[]]`},
		{"DownloadsResume", func(c *jd.Client) error { return c.DownloadsResume(ctx, nil, []int64{2}) }, "/downloadsV2/resumeLinks", `[[],[2]]`},
		{"DownloadsReset", func(c *jd.Client) error { return c.DownloadsReset(ctx, []int64{1}, nil) }, "/downloadsV2/resetLinks", `[[1],[]]`},
		{"DownloadsUnskip", func(c *jd.Client) error { return c.DownloadsUnskip(ctx, []int64{2}, []int64{1}, jd.SkipReasonManual) }, "/downloadsV2/unskip", `[[2],[1],"MANUAL"]`},
		{"DownloadsUnskip any reason", func(c *jd.Client) error { return c.DownloadsUnskip(ctx, nil, []int64{1}, "") }, "/downloadsV2/unskip", `[[],[1],null]`},
		{"SetStopMark", func(c *jd.Client) error { return c.SetStopMark(ctx, 5, -1) }, "/downloadsV2/setStopMark", `[5,-1]`},
		{"RemoveStopMark", func(c *jd.Client) error { return c.RemoveStopMark(ctx) }, "/downloadsV2/removeStopMark", `[]`},
		{"StopMarkedLink", func(c *jd.Client) error { _, err := c.StopMarkedLink(ctx); return err }, "/downloadsV2/getStopMarkedLink", `[]`},
		{"ControllerStart", func(c *jd.Client) error { return c.ControllerStart(ctx) }, "/downloadcontroller/start", `[]`},
		{"ControllerStop", func(c *jd.Client) error { return c.ControllerStop(ctx) }, "/downloadcontroller/stop", `[]`},
		{"ControllerPause", func(c *jd.Client) error { return c.ControllerPause(ctx, true) }, "/downloadcontroller/pause", `[true]`},
		{"ControllerState", func(c *jd.Client) error { _, err := c.ControllerState(ctx); return err }, "/downloadcontroller/getCurrentState", `[]`},
		{"ControllerSpeed", func(c *jd.Client) error { _, err := c.ControllerSpeed(ctx); return err }, "/downloadcontroller/getSpeedInBps", `[]`},

		// linkgrabber
		{"GrabberPackages", func(c *jd.Client) error {
			_, err := c.GrabberPackages(ctx, jd.GrabberPackageQuery{OnlineCount: true, OfflineCount: true, UnknownCount: true, TempUnknownCount: true})
			return err
		}, "/linkgrabberv2/queryPackages", `[{"availableOfflineCount":true,"availableOnlineCount":true,"availableTempUnknownCount":true,"availableUnknownCount":true}]`},
		{"GrabberLinks", func(c *jd.Client) error {
			_, err := c.GrabberLinks(ctx, jd.GrabberLinkQuery{Availability: true, VariantID: true, PackageIDs: []int64{3}})
			return err
		}, "/linkgrabberv2/queryLinks", `[{"availability":true,"variantID":true,"packageUUIDs":[3]}]`},
		{"GrabberStructureChanged", func(c *jd.Client) error { _, _, err := c.GrabberStructureChanged(ctx, 0); return err }, "/linkgrabberv2/getChildrenChanged", `[0]`},
		{"Linkgrabber.SetEnabled", func(c *jd.Client) error { return c.Linkgrabber().SetEnabled(ctx, true, []int64{1}, nil) }, "/linkgrabberv2/setEnabled", `[true,[1],[]]`},
		{"Linkgrabber.Remove", func(c *jd.Client) error { return c.Linkgrabber().Remove(ctx, nil, []int64{2}) }, "/linkgrabberv2/removeLinks", `[[],[2]]`},
		{"Linkgrabber.RenameLink", func(c *jd.Client) error { return c.Linkgrabber().RenameLink(ctx, 5, "n") }, "/linkgrabberv2/renameLink", `[5,"n"]`},
		{"Linkgrabber.MoveLinks", func(c *jd.Client) error { return c.Linkgrabber().MoveLinks(ctx, []int64{1}, 8, 9) }, "/linkgrabberv2/moveLinks", `[[1],8,9]`},
		{"Linkgrabber.Cleanup", func(c *jd.Client) error {
			return c.Linkgrabber().Cleanup(ctx, nil, nil, jd.CleanupDeleteOffline, jd.CleanupRemoveLinksOnly, jd.SelectionAll)
		}, "/linkgrabberv2/cleanup", `[[],[],"DELETE_OFFLINE","REMOVE_LINKS_ONLY","ALL"]`},
		{"AddLinks", func(c *jd.Client) error {
			_, err := c.AddLinks(ctx, jd.AddLinksRequest{
				Links: []string{"http://a/1", "http://a/2"}, PackageName: "P", DestinationFolder: "/d", ExtractPassword: "x", DownloadPassword: "d",
				Priority: jd.PriorityLow, Autostart: &yes, DeepDecrypt: &yes,
			})
			return err
		}, "/linkgrabberv2/addLinks", `[{"links":"http://a/1\nhttp://a/2","packageName":"P","destinationFolder":"/d","extractPassword":"x","downloadPassword":"d","priority":"LOW","autostart":true,"deepDecrypt":true}]`},
		{"AddContainer", func(c *jd.Client) error { _, err := c.AddContainer(ctx, "DLC", []byte("abc")); return err }, "/linkgrabberv2/addContainer", `["DLC","data:application/octet-stream;base64,YWJj"]`},
		{"MoveToDownloads", func(c *jd.Client) error { return c.MoveToDownloads(ctx, []int64{1}, []int64{2}) }, "/linkgrabberv2/moveToDownloadlist", `[[1],[2]]`},
		{"ClearLinkgrabber", func(c *jd.Client) error { return c.ClearLinkgrabber(ctx) }, "/linkgrabberv2/clearList", `[]`},
		{"AbortCollecting", func(c *jd.Client) error { return c.AbortCollecting(ctx) }, "/linkgrabberv2/abort", `[]`},
		{"IsCollecting", func(c *jd.Client) error { _, err := c.IsCollecting(ctx); return err }, "/linkgrabberv2/isCollecting", `[]`},
		{"IsCrawling", func(c *jd.Client) error { _, err := c.IsCrawling(ctx); return err }, "/linkcrawler/isCrawling", `[]`},
		{"CrawlerJobs", func(c *jd.Client) error { _, err := c.CrawlerJobs(ctx, []int64{11}); return err }, "/linkgrabberv2/queryLinkCrawlerJobs", `[{"collectorInfo":true,"jobIds":[11]}]`},
		{"Variants", func(c *jd.Client) error { _, err := c.Variants(ctx, 4); return err }, "/linkgrabberv2/getVariants", `[4]`},
		{"SetVariant", func(c *jd.Client) error { return c.SetVariant(ctx, 4, "v") }, "/linkgrabberv2/setVariant", `[4,"v"]`},
		{"AddVariantCopy", func(c *jd.Client) error { return c.AddVariantCopy(ctx, 4, 5, 6, "v") }, "/linkgrabberv2/addVariantCopy", `[4,5,6,"v"]`},
		{"DownloadFolderHistory", func(c *jd.Client) error { _, err := c.DownloadFolderHistory(ctx); return err }, "/linkgrabberv2/getDownloadFolderHistorySelectionBase", `[]`},

		// events
		{"Subscribe", func(c *jd.Client) error { _, err := c.Subscribe(ctx, jd.Publishers()); return err }, "/events/subscribe", `[["downloads","linkcollector","linkcrawler","downloadwatchdog","captchas","dialogs"],[]]`},
		{"SetSubscription", func(c *jd.Client) error { _, err := c.SetSubscription(ctx, 9, []string{"downloads"}); return err }, "/events/setsubscription", `[9,["downloads"],[]]`},
		{"SubscriptionStatus", func(c *jd.Client) error { _, err := c.SubscriptionStatus(ctx, 9); return err }, "/events/getsubscriptionstatus", `[9]`},
		{"Listen", func(c *jd.Client) error { _, err := c.Listen(ctx, 9); return err }, "/events/listen", `[9]`},
		{"Unsubscribe", func(c *jd.Client) error { return c.Unsubscribe(ctx, 9) }, "/events/unsubscribe", `[9]`},
		{"ListPublishers", func(c *jd.Client) error { _, err := c.ListPublishers(ctx); return err }, "/events/listpublisher", `[]`},
		{"SetStatusEventInterval", func(c *jd.Client) error { return c.SetStatusEventInterval(ctx, 9, 1000) }, "/downloadevents/setStatusEventInterval", `[9,1000]`},

		// captchas and dialogs
		{"Captchas", func(c *jd.Client) error { _, err := c.Captchas(ctx); return err }, "/captcha/list", `[]`},
		{"Captcha", func(c *jd.Client) error { _, err := c.Captcha(ctx, 3); return err }, "/captcha/getCaptchaJob", `[3]`},
		{"CaptchaImage", func(c *jd.Client) error { _, err := c.CaptchaImage(ctx, 3); return err }, "/captcha/get", `[3]`},
		{"CaptchaSolve", func(c *jd.Client) error { return c.CaptchaSolve(ctx, 3, "abc") }, "/captcha/solve", `[3,"abc"]`},
		{"CaptchaSkip", func(c *jd.Client) error { return c.CaptchaSkip(ctx, 3, jd.SkipBlockHoster) }, "/captcha/skip", `[3,"BLOCK_HOSTER"]`},
		{"DialogIDs", func(c *jd.Client) error { _, err := c.DialogIDs(ctx); return err }, "/dialogs/list", `[]`},
		{"Dialog", func(c *jd.Client) error { _, err := c.Dialog(ctx, 3); return err }, "/dialogs/get", `[3,false,true]`},
		{"DialogType", func(c *jd.Client) error { _, err := c.DialogType(ctx, "x.Y"); return err }, "/dialogs/getTypeInfo", `["x.Y"]`},
		{"DialogAnswer", func(c *jd.Client) error {
			return c.DialogAnswer(ctx, 3, map[string]any{"closereason": "OK"})
		}, "/dialogs/answer", `[3,{"closereason":"OK"}]`},
		{"DialogAnswer without data", func(c *jd.Client) error { return c.DialogAnswer(ctx, 3, nil) }, "/dialogs/answer", `[3,{}]`},

		// extraction
		{"ExtractionQueue", func(c *jd.Client) error { _, err := c.ExtractionQueue(ctx); return err }, "/extraction/getQueue", `[]`},
		{"ExtractionStart", func(c *jd.Client) error { _, err := c.ExtractionStart(ctx, []int64{1}, nil); return err }, "/extraction/startExtractionNow", `[[1],[]]`},
		{"ExtractionCancel", func(c *jd.Client) error { _, err := c.ExtractionCancel(ctx, 12); return err }, "/extraction/cancelExtraction", `[12]`},
		{"Archives", func(c *jd.Client) error { _, err := c.Archives(ctx, nil, []int64{2}); return err }, "/extraction/getArchiveInfo", `[[],[2]]`},
		{"ArchiveSettings", func(c *jd.Client) error { _, err := c.ArchiveSettings(ctx, []string{"a1"}); return err }, "/extraction/getArchiveSettings", `[["a1"]]`},
		{"SetArchiveSettings", func(c *jd.Client) error {
			return c.SetArchiveSettings(ctx, jd.ArchiveSettings{ArchiveID: "a1", ExtractPath: "/x", AutoExtract: &yes})
		}, "/extraction/setArchiveSettings", `["a1",{"archiveId":"a1","extractPath":"/x","passwords":[],"autoExtract":true}]`},
		{"AddArchivePassword", func(c *jd.Client) error { return c.AddArchivePassword(ctx, "pw") }, "/extraction/addArchivePassword", `["pw"]`},

		// accounts
		{"AddAccount", func(c *jd.Client) error { return c.AddAccount(ctx, "1fichier.com", "u", "p") }, "/accountsV2/addAccount", `["1fichier.com","u","p"]`},
		{"SetAccountLogin", func(c *jd.Client) error { return c.SetAccountLogin(ctx, 3, "u", "p") }, "/accountsV2/setUserNameAndPassword", `[3,"u","p"]`},
		{"EnableAccounts", func(c *jd.Client) error { return c.EnableAccounts(ctx, []int64{3}) }, "/accountsV2/enableAccounts", `[[3]]`},
		{"DisableAccounts", func(c *jd.Client) error { return c.DisableAccounts(ctx, []int64{3}) }, "/accountsV2/disableAccounts", `[[3]]`},
		{"RemoveAccounts", func(c *jd.Client) error { return c.RemoveAccounts(ctx, []int64{3}) }, "/accountsV2/removeAccounts", `[[3]]`},
		{"RefreshAccounts", func(c *jd.Client) error { return c.RefreshAccounts(ctx, []int64{3}, true) }, "/accountsV2/refreshAccounts", `[[3],true]`},
		{"PremiumHosters", func(c *jd.Client) error { _, err := c.PremiumHosters(ctx); return err }, "/accountsV2/listPremiumHoster", `[]`},
		{"BasicAuths", func(c *jd.Client) error { _, err := c.BasicAuths(ctx); return err }, "/accountsV2/listBasicAuth", `[]`},
		{"AddBasicAuth", func(c *jd.Client) error {
			_, err := c.AddBasicAuth(ctx, jd.BasicAuthHTTP, "*.example.org", "u", "p")
			return err
		}, "/accountsV2/addBasicAuth", `["HTTP","*.example.org","u","p"]`},
		{"UpdateBasicAuth", func(c *jd.Client) error {
			return c.UpdateBasicAuth(ctx, jd.BasicAuth{ID: 5, Type: jd.BasicAuthFTP, HostMask: "h", Username: "u", Password: "p", Enabled: true})
		}, "/accountsV2/updateBasicAuth", `[{"id":5,"type":"FTP","hostmask":"h","username":"u","password":"p","enabled":true,"created":0,"lastValidated":0}]`},
		{"RemoveBasicAuths", func(c *jd.Client) error { return c.RemoveBasicAuths(ctx, []int64{5}) }, "/accountsV2/removeBasicAuths", `[[5]]`},

		// config and plugins
		{"ConfigQuery", func(c *jd.Client) error {
			_, err := c.ConfigQuery(ctx, jd.ConfigQuery{Pattern: "Speed", Values: true, Description: true})
			return err
		}, "/config/query", `[{"pattern":"Speed","description":true,"values":true,"defaultValues":false,"enumInfo":false}]`},
		{"ConfigGet", func(c *jd.Client) error { _, err := c.ConfigGet(ctx, "x.Cfg", "", "Key"); return err }, "/config/get", `["x.Cfg",null,"Key"]`},
		{"ConfigSet", func(c *jd.Client) error { return c.ConfigSet(ctx, "x.Cfg", "st", "Key", 5) }, "/config/set", `["x.Cfg","st","Key",5]`},
		{"ConfigReset", func(c *jd.Client) error { return c.ConfigReset(ctx, "x.Cfg", "", "Key") }, "/config/reset", `["x.Cfg",null,"Key"]`},
		{"Plugins", func(c *jd.Client) error { _, err := c.Plugins(ctx); return err }, "/plugins/list", `[{"pattern":true,"version":true}]`},
		{"PluginSettings", func(c *jd.Client) error {
			_, err := c.PluginSettings(ctx, jd.ConfigQuery{Pattern: "1fichier", Values: true})
			return err
		}, "/plugins/query", `[{"pattern":"1fichier","description":false,"values":true,"defaultValues":false,"enumInfo":false}]`},
		{"PluginSet", func(c *jd.Client) error { return c.PluginSet(ctx, "x.Cfg", "1fichier.com", "Key", "v") }, "/plugins/set", `["x.Cfg","1fichier.com","Key","v"]`},
		{"PluginReset", func(c *jd.Client) error { return c.PluginReset(ctx, "x.Cfg", "1fichier.com", "Key") }, "/plugins/reset", `["x.Cfg","1fichier.com","Key"]`},

		// extensions
		{"Extensions", func(c *jd.Client) error { _, err := c.Extensions(ctx); return err }, "/extensions/list", `[{"configInterface":true,"description":true,"enabled":true,"iconKey":true,"installed":true,"name":true}]`},
		{"InstallExtension", func(c *jd.Client) error { _, err := c.InstallExtension(ctx, "folderwatch"); return err }, "/extensions/install", `["folderwatch"]`},
		{"SetExtensionEnabled", func(c *jd.Client) error { _, err := c.SetExtensionEnabled(ctx, "folderwatch", true); return err }, "/extensions/setEnabled", `["folderwatch",true]`},

		// icons (answer with an image, see TestIcons)
		{"HosterIcon", func(c *jd.Client) error { _, _, err := c.HosterIcon(ctx, "1fichier.com"); return err }, "/contentV2/getFavIcon", `["1fichier.com"]`},
		{"FileIcon", func(c *jd.Client) error { _, _, err := c.FileIcon(ctx, "a.zip"); return err }, "/contentV2/getFileIcon", `["a.zip"]`},

		// system
		{"Ping", func(c *jd.Client) error { _, err := c.Ping(ctx); return err }, "/device/ping", `[]`},
		{"CoreRevision", func(c *jd.Client) error { _, err := c.CoreRevision(ctx); return err }, "/jd/getCoreRevision", `[]`},
		{"Uptime", func(c *jd.Client) error { _, err := c.Uptime(ctx); return err }, "/jd/uptime", `[]`},
		{"SystemInfos", func(c *jd.Client) error { _, err := c.SystemInfos(ctx); return err }, "/system/getSystemInfos", `[]`},
		{"StorageInfos", func(c *jd.Client) error { _, err := c.StorageInfos(ctx, "/output"); return err }, "/system/getStorageInfos", `["/output"]`},
		{"RestartJD", func(c *jd.Client) error { return c.RestartJD(ctx) }, "/system/restartJD", `[]`},
		{"ExitJD", func(c *jd.Client) error { return c.ExitJD(ctx) }, "/system/exitJD", `[]`},
		{"ShutdownOS", func(c *jd.Client) error { return c.ShutdownOS(ctx, true) }, "/system/shutdownOS", `[true]`},
		{"HibernateOS", func(c *jd.Client) error { return c.HibernateOS(ctx) }, "/system/hibernateOS", `[]`},
		{"StandbyOS", func(c *jd.Client) error { return c.StandbyOS(ctx) }, "/system/standbyOS", `[]`},
		{"Reconnect", func(c *jd.Client) error { return c.Reconnect(ctx) }, "/reconnect/doReconnect", `[]`},
		{"UpdateAvailable", func(c *jd.Client) error { _, err := c.UpdateAvailable(ctx); return err }, "/update/isUpdateAvailable", `[]`},
		{"CheckForUpdate", func(c *jd.Client) error { return c.CheckForUpdate(ctx) }, "/update/runUpdateCheck", `[]`},
		{"RestartAndUpdate", func(c *jd.Client) error { return c.RestartAndUpdate(ctx) }, "/update/restartAndUpdate", `[]`},
		{"Toolbar", func(c *jd.Client) error { _, err := c.Toolbar(ctx); return err }, "/toolbar/getStatus", `[]`},
		{"TogglePremium", func(c *jd.Client) error { _, err := c.TogglePremium(ctx); return err }, "/toolbar/togglePremium", `[]`},
		{"ToggleClipboard", func(c *jd.Client) error { _, err := c.ToggleClipboard(ctx); return err }, "/toolbar/toggleClipboardMonitoring", `[]`},
		{"ToggleReconnect", func(c *jd.Client) error { _, err := c.ToggleReconnect(ctx); return err }, "/toolbar/toggleAutomaticReconnect", `[]`},
		{"ToggleStopAfter", func(c *jd.Client) error { _, err := c.ToggleStopAfter(ctx); return err }, "/toolbar/toggleStopAfterCurrentDownload", `[]`},
		{"ToggleSpeedLimit", func(c *jd.Client) error { _, err := c.ToggleSpeedLimit(ctx); return err }, "/toolbar/toggleDownloadSpeedLimit", `[]`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rep := void
			if boolResults[tc.path] {
				rep = reply{200, []byte(`{"data":true}`)}
			}
			s := newStub(t, map[string]reply{tc.path: rep})
			if strings.HasPrefix(tc.path, "/contentV2/") {
				s.routes[tc.path] = reply{200, binary(t, "icon_file.png")}
				s.ctypes[tc.path] = "image/png"
			}
			err := tc.call(s.client())
			if err != nil {
				t.Fatal(err)
			}
			r := s.one(t)
			if r.Path != tc.path {
				t.Errorf("path = %s, want %s", r.Path, tc.path)
			}
			if want := `{"params":` + tc.params + `}`; string(r.Body) != want {
				t.Errorf("body = %s\nwant   %s", r.Body, want)
			}
		})
	}
}

// boolResults are the paths whose answer is true on success.
var boolResults = map[string]bool{
	"/config/set": true, "/config/reset": true, "/plugins/set": true, "/plugins/reset": true,
	"/extraction/setArchiveSettings": true,
}

// Calls with a bool result that JDownloader answers with false are refused.
func TestRefusedByJDownloader(t *testing.T) {
	ctx := context.Background()
	no := reply{200, []byte(`{"data":false}`)}
	s := newStub(t, map[string]reply{
		"/config/set": no, "/config/reset": no, "/plugins/set": no, "/plugins/reset": no, "/extraction/setArchiveSettings": no,
	})
	c := s.client()
	for name, err := range map[string]error{
		"ConfigSet":          c.ConfigSet(ctx, "i", "", "k", 1),
		"ConfigReset":        c.ConfigReset(ctx, "i", "", "k"),
		"PluginSet":          c.PluginSet(ctx, "i", "p", "k", 1),
		"PluginReset":        c.PluginReset(ctx, "i", "p", "k"),
		"SetArchiveSettings": c.SetArchiveSettings(ctx, jd.ArchiveSettings{ArchiveID: "a"}),
	} {
		if err != jd.ErrRejected {
			t.Errorf("%s: err = %v, want ErrRejected", name, err)
		}
	}
}

func TestSetSpeedLimit(t *testing.T) {
	s := newStub(t, map[string]reply{"/config/set": {200, []byte(`{"data":true}`)}})
	if err := s.client().SetSpeedLimit(context.Background(), true, 204800); err != nil {
		t.Fatal(err)
	}
	if len(s.requests) != 2 {
		t.Fatalf("requests = %d", len(s.requests))
	}
	g := `"org.jdownloader.settings.GeneralSettings"`
	if got := string(s.requests[0].Body); got != `{"params":[`+g+`,null,"DownloadSpeedLimit",204800]}` {
		t.Errorf("first = %s", got)
	}
	if got := string(s.requests[1].Body); got != `{"params":[`+g+`,null,"DownloadSpeedLimitEnabled",true]}` {
		t.Errorf("second = %s", got)
	}

	s = newStub(t, map[string]reply{"/config/set": {200, []byte(`{"data":true}`)}})
	if err := s.client().SetSpeedLimit(context.Background(), false, 0); err != nil {
		t.Fatal(err)
	}
	if got := string(s.one(t).Body); !strings.Contains(got, `"DownloadSpeedLimitEnabled",false`) {
		t.Errorf("switch only = %s", got)
	}
}

func TestSpeedLimit(t *testing.T) {
	s := newStub(t, map[string]reply{"/config/get": {200, []byte(`{"data":10240}`)}})
	_, n, err := s.client().SpeedLimit(context.Background())
	if err != nil || n != 10240 {
		t.Errorf("n = %d, err = %v", n, err)
	}
}
