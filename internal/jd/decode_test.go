package jd_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Buffer379x/jdweb/internal/jd"
)

// These tests run the answers recorded from a real JDownloader (core revision
// 50639, jlesage/jdownloader-2) through the typed methods.

func serve(t *testing.T, path, name string) *jd.Client {
	t.Helper()
	return newStub(t, map[string]reply{path: ok(t, name)}).client()
}

func TestGrabber(t *testing.T) {
	ctx := context.Background()
	links, err := serve(t, "/linkgrabberv2/queryLinks", "lg_query_links").GrabberLinks(ctx, jd.AllGrabberLinkFields())
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 4 {
		t.Fatalf("links = %d", len(links))
	}
	// JDownloader order is kept.
	var names []string
	for _, l := range links {
		names = append(names, l.Name)
	}
	if got := names; got[0] != "archiv.zip" || got[1] != "big1.bin" || got[3] != "off.bin" {
		t.Errorf("order = %v", got)
	}
	if l := links[1]; l.Availability != jd.AvailabilityOnline || l.BytesTotal != 1048576 || l.Priority != jd.PriorityHigh ||
		l.Comment != "rec comment" || l.Password != "dlpw" || !l.Enabled || l.Host != "http links" || l.Variants {
		t.Errorf("link = %+v", l)
	}
	if links[3].Availability != jd.AvailabilityTempUnknown || links[3].BytesTotal != 0 {
		t.Errorf("unchecked link = %+v", links[3])
	}

	pkgs, err := serve(t, "/linkgrabberv2/queryPackages", "lg_query_packages").GrabberPackages(ctx, jd.AllGrabberPackageFields())
	if err != nil || len(pkgs) != 1 {
		t.Fatal(pkgs, err)
	}
	p := pkgs[0]
	if p.Name != "Rec B" || p.ChildCount != 4 || p.OnlineCount != 3 || p.OfflineCount != 0 || p.UnknownCount != 0 || p.TempUnknownCount != 1 ||
		p.SaveTo != "/output/jdweb-rec/Rec B" || p.BytesTotal != 2097461 || !p.Enabled || len(p.Hosts) != 1 {
		t.Errorf("package = %+v", p)
	}

	page, err := serve(t, "/linkgrabberv2/queryLinks", "lg_query_links_page").GrabberLinks(ctx, jd.GrabberLinkQuery{MaxResults: 2, StartAt: 1})
	if err != nil || len(page) != 2 || page[0].Name != "big1.bin" {
		t.Errorf("page = %+v, %v", page, err)
	}
}

func TestStructureCounters(t *testing.T) {
	ctx := context.Background()
	c := serve(t, "/downloadsV2/getStructureChangeCounter", "dl_structure_counter")
	n, changed, err := c.DownloadsStructureChanged(ctx, 0)
	if err != nil || !changed || n != 1793562910511 {
		t.Errorf("changed: %d %v %v", n, changed, err)
	}
	c = serve(t, "/downloadsV2/getStructureChangeCounter", "dl_structure_unchanged")
	n, changed, err = c.DownloadsStructureChanged(ctx, 1793562910513)
	if err != nil || changed || n != 1793562910513 {
		t.Errorf("unchanged: %d %v %v", n, changed, err)
	}
	c = serve(t, "/linkgrabberv2/getChildrenChanged", "lg_children_changed")
	if _, changed, err = c.GrabberStructureChanged(ctx, 0); err != nil || !changed {
		t.Errorf("grabber: %v %v", changed, err)
	}
}

func TestCollecting(t *testing.T) {
	ctx := context.Background()
	if b, err := serve(t, "/linkgrabberv2/isCollecting", "lg_is_collecting").IsCollecting(ctx); err != nil || b {
		t.Errorf("isCollecting = %v, %v", b, err)
	}
	if b, err := serve(t, "/linkgrabberv2/isCollecting", "lg_is_collecting_true").IsCollecting(ctx); err != nil || !b {
		t.Errorf("isCollecting = %v, %v", b, err)
	}
	if b, err := serve(t, "/linkcrawler/isCrawling", "lc_is_crawling_true").IsCrawling(ctx); err != nil || !b {
		t.Errorf("isCrawling = %v, %v", b, err)
	}
	jobs, err := serve(t, "/linkgrabberv2/queryLinkCrawlerJobs", "lg_query_crawler_jobs").CrawlerJobs(ctx, []int64{1793563956781})
	if err != nil || len(jobs) != 1 || !jobs[0].Crawling || !jobs[0].Checking || jobs[0].Crawled != 8 || jobs[0].JobID != 1793563956781 {
		t.Errorf("jobs = %+v, %v", jobs, err)
	}
	id, err := serve(t, "/linkgrabberv2/addLinks", "lg_add_links").AddLinks(ctx, jd.AddLinksRequest{Links: []string{"x"}})
	if err != nil || id != 1793563933607 {
		t.Errorf("job id = %d, %v", id, err)
	}
	h, err := serve(t, "/linkgrabberv2/getDownloadFolderHistorySelectionBase", "lg_folder_history").DownloadFolderHistory(ctx)
	if err != nil || len(h) != 3 || h[2] != "/output" {
		t.Errorf("history = %v, %v", h, err)
	}
}

func TestDownloadsFinishedAndExtracted(t *testing.T) {
	links, err := serve(t, "/downloadsV2/queryLinks", "dl_query_links_finished_extracted").DownloadLinks(context.Background(), jd.AllDownloadLinkFields())
	if err != nil || len(links) != 1 {
		t.Fatal(links, err)
	}
	l := links[0]
	if !l.Finished || l.ExtractionStatus != "SUCCESSFUL" || l.Status != "Extraction OK" || l.FinishedDate == 0 || l.Running || l.StatusIconKey != "extract_ok" {
		t.Errorf("link = %+v", l)
	}
	pkgs, err := serve(t, "/downloadsV2/queryPackages", "dl_query_packages_running").DownloadPackages(context.Background(), jd.AllDownloadPackageFields())
	if err != nil || len(pkgs) != 1 {
		t.Fatal(pkgs, err)
	}
	p := pkgs[0]
	if !p.Running || p.Finished || p.ChildCount != 4 || p.BytesTotal != 2097461 || p.BytesLoaded != 145416 || p.Speed != 10097 || p.ETA != 193 ||
		p.SaveTo != "/output/jdweb-rec/Rec B" || len(p.Hosts) != 1 || p.Hosts[0] != "jdweb-test-files" {
		t.Errorf("package = %+v", p)
	}
}

func TestController(t *testing.T) {
	ctx := context.Background()
	if s, err := serve(t, "/downloadcontroller/getCurrentState", "dl_controller_state_running").ControllerState(ctx); err != nil || s != jd.ControllerRunning {
		t.Errorf("state = %q, %v", s, err)
	}
	if s, err := serve(t, "/downloadcontroller/getCurrentState", "dl_controller_state_stopped").ControllerState(ctx); err != nil || s != jd.ControllerStopped {
		t.Errorf("state = %q, %v", s, err)
	}
	if n, err := serve(t, "/downloadcontroller/getSpeedInBps", "dl_speed").ControllerSpeed(ctx); err != nil || n == 0 {
		t.Errorf("speed = %d, %v", n, err)
	}
	if l, err := serve(t, "/downloadsV2/getStopMarkedLink", "dl_stopmarked_link").StopMarkedLink(ctx); err != nil || l == nil || l.Name != "b.bin" {
		t.Errorf("stop mark = %+v, %v", l, err)
	}
	if l, err := serve(t, "/downloadsV2/getStopMarkedLink", "dl_stopmarked_link_none").StopMarkedLink(ctx); err != nil || l != nil {
		t.Errorf("no stop mark = %+v, %v", l, err)
	}
	urls, err := serve(t, "/downloadsV2/getDownloadUrls", "dl_urls").Downloads().URLs(ctx, []int64{1793563981166}, nil)
	if got := urls["http://jdweb-test-files:8000/archiv.zip"]; err != nil || len(urls) != 1 || len(got) != 1 || got[0] != 1793563981166 {
		t.Errorf("urls = %v, %v", urls, err)
	}
}

func TestToolbar(t *testing.T) {
	ctx := context.Background()
	st, err := serve(t, "/toolbar/getStatus", "toolbar_status_running").Toolbar(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := jd.ToolbarStatus{State: jd.ControllerRunning, Running: true, Speed: 8860, Limit: true, LimitSpeed: 10240, Premium: true, Reconnect: true,
		Clipboard: true, DownloadCurrent: 145416, DownloadComplete: 2097461}
	if st != want {
		t.Errorf("running = %+v\nwant      %+v", st, want)
	}
	st, err = serve(t, "/toolbar/getStatus", "toolbar_status_stopped").Toolbar(ctx)
	if err != nil || st.State != jd.ControllerStopped || st.Running || st.Pause || !st.Limit {
		t.Errorf("stopped = %+v, %v", st, err)
	}
	st, err = serve(t, "/toolbar/getStatus", "toolbar_status_idle").Toolbar(ctx)
	if err != nil || st.State != jd.ControllerIdle || st.Running || st.Limit || st.LimitSpeed != 0 || !st.Premium {
		t.Errorf("idle = %+v, %v", st, err)
	}
}

func TestSystem(t *testing.T) {
	ctx := context.Background()
	if ok, err := serve(t, "/device/ping", "device_ping").Ping(ctx); err != nil || !ok {
		t.Errorf("ping = %v, %v", ok, err)
	}
	if rev, err := serve(t, "/jd/getCoreRevision", "jd_core_revision").CoreRevision(ctx); err != nil || rev != 50639 {
		t.Errorf("revision = %d, %v", rev, err)
	}
	if ms, err := serve(t, "/jd/uptime", "jd_uptime").Uptime(ctx); err != nil || ms <= 0 {
		t.Errorf("uptime = %d, %v", ms, err)
	}
	si, err := serve(t, "/system/getSystemInfos", "system_infos").SystemInfos(ctx)
	if err != nil || !si.Docker || si.Headless || si.OSFamily != "LINUX" || si.JavaVersion != "1.8.0_492-b09" || si.HeapMax != 2800222208 || si.StartupTimestamp != 1793562910148 {
		t.Errorf("system = %+v, %v", si, err)
	}
	st, err := serve(t, "/system/getStorageInfos", "system_storage").StorageInfos(ctx, "/output")
	if err != nil || len(st) != 1 || st[0].Path != "/output" || st[0].Free != 257374752768 || st[0].Size != 311653564416 || st[0].Error != "" {
		t.Errorf("storage = %+v, %v", st, err)
	}
	if b, err := serve(t, "/update/isUpdateAvailable", "void_ok").UpdateAvailable(ctx); err != nil || b {
		t.Errorf("update = %v, %v", b, err)
	}
}

func TestEventsDecode(t *testing.T) {
	ctx := context.Background()
	sub, err := serve(t, "/events/subscribe", "events_subscribe").Subscribe(ctx, jd.Publishers())
	if err != nil {
		t.Fatal(err)
	}
	if sub.ID != 1793563999821 || sub.MaxPollTimeout != 25000 || sub.MaxKeepalive != 120000 || len(sub.Subscriptions) != 6 || sub.Exclusions != nil {
		t.Errorf("subscription = %+v", sub)
	}

	// The recorded status of a live subscription says subscribed:false.
	st, err := serve(t, "/events/getsubscriptionstatus", "events_status").SubscriptionStatus(ctx, sub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Subscribed {
		t.Error("recording changed: subscribed was false for a live subscription")
	}
	if !st.Exists(sub.ID) {
		t.Errorf("a live subscription must exist: %+v", st)
	}
	gone, err := serve(t, "/events/getsubscriptionstatus", "subscription_gone_status").SubscriptionStatus(ctx, sub.ID)
	if err != nil || gone.Exists(sub.ID) || gone.ID != -1 {
		t.Errorf("gone = %+v, %v", gone, err)
	}

	events, err := serve(t, "/events/listen", "events_listen_downloads").Listen(ctx, sub.ID)
	if err != nil || len(events) != 32 {
		t.Fatalf("events = %d, %v", len(events), err)
	}
	var sawLinkUpdate, sawNull bool
	for _, e := range events {
		if e.Publisher != jd.PublisherDownloads && e.Publisher != jd.PublisherWatchdog {
			t.Errorf("unexpected publisher %q", e.Publisher)
		}
		if e.EventID == "LINK_UPDATE.bytesLoaded" {
			var d struct {
				UUID        int64 `json:"uuid"`
				BytesLoaded int64 `json:"bytesLoaded"`
			}
			if err := json.Unmarshal(e.EventData, &d); err != nil || d.UUID == 0 || d.BytesLoaded == 0 {
				t.Errorf("event data = %s, %v", e.EventData, err)
			}
			sawLinkUpdate = true
		}
		if e.EventID == "REFRESH_CONTENT" {
			sawNull = string(e.EventData) == "null"
		}
	}
	if !sawLinkUpdate || !sawNull {
		t.Errorf("sawLinkUpdate=%v sawNull=%v", sawLinkUpdate, sawNull)
	}

	events, err = serve(t, "/events/listen", "events_listen_collector").Listen(ctx, sub.ID)
	if err != nil || len(events) != 1 || events[0].Publisher != jd.PublisherLinkCrawler || events[0].EventID != "STARTED" {
		t.Errorf("collector events = %+v, %v", events, err)
	}
}

func TestListenOnUnknownSubscriptionIs404(t *testing.T) {
	s := newStub(t, map[string]reply{"/events/listen": {404, fixture(t, "err_404_not_found")}})
	_, err := s.client().Listen(context.Background(), 12345)
	if !jd.IsNotFound(err) {
		t.Errorf("err = %v, want IsNotFound", err)
	}
}

func TestPublishersAreExactlyTheSix(t *testing.T) {
	want := []string{"downloads", "linkcollector", "linkcrawler", "downloadwatchdog", "captchas", "dialogs"}
	got := jd.Publishers()
	if len(got) != len(want) {
		t.Fatalf("publishers = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("publishers = %v, want %v", got, want)
		}
	}
	for _, p := range got {
		if p == "linkgrabberv2" {
			t.Error("linkgrabberv2 is not a publisher")
		}
	}
}

func TestCaptchasAndDialogs(t *testing.T) {
	ctx := context.Background()
	if l, err := serve(t, "/captcha/list", "captcha_list_empty").Captchas(ctx); err != nil || len(l) != 0 {
		t.Errorf("captchas = %v, %v", l, err)
	}
	if j, err := serve(t, "/captcha/getCaptchaJob", "captcha_not_available").Captcha(ctx, 5); err != nil || j != nil {
		t.Errorf("captcha = %+v, %v", j, err)
	}
	s := newStub(t, map[string]reply{"/captcha/get": {404, fixture(t, "err_404_not_found")}})
	if _, err := s.client().CaptchaImage(ctx, 5); !jd.IsNotFound(err) {
		t.Errorf("err = %v", err)
	}
	// A captcha job in the shape of the reference (none was observed live).
	s = newStub(t, map[string]reply{"/captcha/list": {200, []byte(`{"data":[{"id":9,"hoster":"example.org","type":"recaptchav2","created":1793563999821,"timeout":300000,"captchaCategory":"x","challengeType":"y","explain":"Click all","link":4,"remaining":1}]}`)}})
	jobs, err := s.client().Captchas(ctx)
	if err != nil || len(jobs) != 1 || jobs[0].ID != 9 || jobs[0].Hoster != "example.org" || jobs[0].Timeout != 300000 || jobs[0].Explain != "Click all" || jobs[0].Link != 4 {
		t.Errorf("jobs = %+v, %v", jobs, err)
	}

	ids, err := serve(t, "/dialogs/list", "dialogs_list").DialogIDs(ctx)
	if err != nil || len(ids) != 2 || ids[0] != 1793563825856 {
		t.Errorf("ids = %v, %v", ids, err)
	}
	d, err := serve(t, "/dialogs/get", "dialogs_get_filexists").Dialog(ctx, ids[0])
	if err != nil || d.Type != "jd.controlling.downloadcontroller.IfFileExistsDialogInterface" || d.Properties["filepath"] != "/output/jdweb-rec/Rec A/hello.txt" || d.Properties["title"] != "File exists" {
		t.Errorf("dialog = %+v, %v", d, err)
	}
	d, err = serve(t, "/dialogs/get", "dialogs_get").Dialog(ctx, ids[1])
	if err != nil || d.Type != "jd.controlling.downloadcontroller.IfFileExistsDialogInterface" || d.Properties["flags"] != "32" || d.Properties["timeout"] != "0" {
		t.Errorf("dialog = %+v, %v", d, err)
	}
	ti, err := serve(t, "/dialogs/getTypeInfo", "dialogs_get_type_filexists").DialogType(ctx, d.Type)
	if err != nil || ti.In["action"] != "class org.jdownloader.settings.IfFileExistsAction" || ti.In["closereason"] == "" || ti.Out["filepath"] == "" {
		t.Errorf("type = %+v, %v", ti, err)
	}
	if err := serve(t, "/dialogs/answer", "dialogs_answer_ok").DialogAnswer(ctx, ids[1], map[string]any{"closereason": "CANCEL"}); err != nil {
		t.Errorf("answer: %v", err)
	}
	s = newStub(t, map[string]reply{"/dialogs/answer": {400, fixture(t, "err_dialog_answer_first")}})
	err = s.client().DialogAnswer(ctx, 1, nil)
	var je *jd.Error
	if err == nil || !asJD(err, &je) || je.Status != 400 {
		t.Errorf("answer out of order: %v", err)
	}
}

func TestStoppedControllerKeepsFinishedLinks(t *testing.T) {
	links, err := serve(t, "/downloadsV2/queryLinks", "dl_query_links_stopped").DownloadLinks(context.Background(), jd.AllDownloadLinkFields())
	if err != nil || len(links) == 0 {
		t.Fatal(links, err)
	}
	for _, l := range links {
		if l.Running {
			t.Errorf("%s still running after stop", l.Name)
		}
	}
}

func TestVoidResults(t *testing.T) {
	ctx := context.Background()
	if err := serve(t, "/downloadevents/setStatusEventInterval", "events_set_interval").SetStatusEventInterval(ctx, 1, 1000); err != nil {
		t.Errorf("interval: %v", err)
	}
	if err := serve(t, "/linkgrabberv2/clearList", "lg_clear_list").ClearLinkgrabber(ctx); err != nil {
		t.Errorf("clear: %v", err)
	}
}

func TestExceptionDialog(t *testing.T) {
	ctx := context.Background()
	d, err := serve(t, "/dialogs/get", "dialogs_get_exception").Dialog(ctx, 1793565385124)
	if err != nil || d.Type != "org.appwork.uio.ExceptionDialogInterface" || d.Properties["title"] != "An Error occurred..." ||
		!strings.Contains(d.Properties["stacktrace"], "DownloadWatchDog.move") || !strings.Contains(d.Properties["message"], "\r\n") {
		t.Errorf("dialog = %+v, %v", d, err)
	}
	ti, err := serve(t, "/dialogs/getTypeInfo", "dialogs_get_type_exception").DialogType(ctx, d.Type)
	if err != nil || ti.Out["stacktrace"] != "class java.lang.String" || ti.In["closereason"] == "" {
		t.Errorf("type = %+v, %v", ti, err)
	}
}

func TestExtraction(t *testing.T) {
	ctx := context.Background()
	a, err := serve(t, "/extraction/getArchiveInfo", "extraction_archive_info").Archives(ctx, []int64{1}, nil)
	if err != nil || len(a) != 1 {
		t.Fatal(a, err)
	}
	if a[0].ArchiveName != "archiv" || a[0].Type != "ZIP_SINGLE" || a[0].ControllerID != -1 || a[0].ControllerStatus != "NA" || a[0].States["archiv.zip"] != "COMPLETE" || len(a[0].ArchiveID) != 64 {
		t.Errorf("archive = %+v", a[0])
	}
	set, err := serve(t, "/extraction/getArchiveSettings", "extraction_archive_settings").ArchiveSettings(ctx, []string{a[0].ArchiveID})
	if err != nil || len(set) != 1 || set[0].ExtractPath != "/output/exx" || len(set[0].Passwords) != 1 || set[0].AutoExtract == nil || !*set[0].AutoExtract || set[0].RemoveFilesAfterExtraction != nil {
		t.Errorf("settings = %+v, %v", set, err)
	}
	m, err := serve(t, "/extraction/startExtractionNow", "extraction_start_now").ExtractionStart(ctx, []int64{1}, nil)
	if err != nil || !m["1793563825843"] {
		t.Errorf("start = %v, %v", m, err)
	}
	q, err := serve(t, "/extraction/getQueue", "captcha_list_empty").ExtractionQueue(ctx)
	if err != nil || len(q) != 0 {
		t.Errorf("queue = %v, %v", q, err)
	}
}

func TestAccounts(t *testing.T) {
	ctx := context.Background()
	acc, err := serve(t, "/accountsV2/listAccounts", "accounts_list_error").Accounts(ctx)
	if err != nil || len(acc) != 1 {
		t.Fatal(acc, err)
	}
	a := acc[0]
	if a.Hostname != "1fichier.com" || a.Username != "user" || !a.Enabled || a.Valid || a.ErrorType != "INVALID" || a.TrafficLeft != -1 || a.TrafficMax != -1 || a.ValidUntil != -1 ||
		a.ErrorString != "Please enter a valid e-mail address into the username field." || a.UUID != 1793564066894 {
		t.Errorf("account = %+v", a)
	}
	ba, err := serve(t, "/accountsV2/listBasicAuth", "basicauth_list_entry").BasicAuths(ctx)
	if err != nil || len(ba) != 1 || ba[0].Type != jd.BasicAuthHTTP || ba[0].HostMask != "*.example.org" || !ba[0].Enabled || ba[0].Password != "" || ba[0].LastValidated != -1 {
		t.Errorf("basic auth = %+v, %v", ba, err)
	}
	if l, err := serve(t, "/accountsV2/listBasicAuth", "basicauth_list").BasicAuths(ctx); err != nil || len(l) != 0 {
		t.Errorf("empty = %v, %v", l, err)
	}
	id, err := newStub(t, map[string]reply{"/accountsV2/addBasicAuth": {200, []byte(`{"data":1793563843085}`)}}).client().AddBasicAuth(ctx, jd.BasicAuthHTTP, "h", "u", "p")
	if err != nil || id != 1793563843085 {
		t.Errorf("id = %d, %v", id, err)
	}
}

func TestConfigPluginsExtensions(t *testing.T) {
	ctx := context.Background()
	e, err := serve(t, "/config/query", "config_query_speed").ConfigQuery(ctx, jd.ConfigQuery{Pattern: ".*DownloadSpeedLimit.*"})
	if err != nil || len(e) != 3 {
		t.Fatal(e, err)
	}
	var lim jd.ConfigEntry
	for _, x := range e {
		if x.Key == "DownloadSpeedLimit" {
			lim = x
		}
	}
	if lim.InterfaceName != "org.jdownloader.settings.GeneralSettings" || lim.AbstractType != "INT" || string(lim.Value) != "10240" || string(lim.DefaultValue) != "51200" ||
		lim.Docs != "Download Speed limit in bytes." || lim.Storage != "" {
		t.Errorf("entry = %+v", lim)
	}
	v, err := serve(t, "/config/get", "config_get_speed").ConfigGet(ctx, "i", "", "k")
	if err != nil || string(v) != "10240" {
		t.Errorf("get = %s, %v", v, err)
	}
	v, err = serve(t, "/config/get", "config_get_missing").ConfigGet(ctx, "i", "", "k")
	if err != nil || v != nil {
		t.Errorf("missing = %q, %v", v, err)
	}
	if err := serve(t, "/config/set", "config_set_ok").ConfigSet(ctx, "i", "", "k", 1); err != nil {
		t.Errorf("set: %v", err)
	}
	pl, err := serve(t, "/plugins/list", "plugins_list").Plugins(ctx)
	if err != nil || len(pl) != 3 || pl[0].DisplayName != "1fichier.com" || pl[0].ClassName != "jd.plugins.hoster.OneFichierCom" || pl[0].Version != "53703" || pl[0].Pattern == "" {
		t.Errorf("plugins = %+v, %v", pl, err)
	}
	ex, err := serve(t, "/extensions/list", "extension_list").Extensions(ctx)
	if err != nil || len(ex) != 8 {
		t.Fatal(ex, err)
	}
	if x := ex[0]; x.ID != "org.jdownloader.extensions.extraction.ExtractionExtension" || x.Name != "Archive Extractor" || !x.Installed || !x.Enabled || x.ConfigInterface == "" {
		t.Errorf("extension = %+v", x)
	}
	if x := ex[1]; x.ID != "eventscripter" || x.Installed || x.Enabled {
		t.Errorf("extension = %+v", x)
	}
}

func asJD(err error, target **jd.Error) bool {
	e, ok := err.(*jd.Error)
	if ok {
		*target = e
	}
	return ok
}
