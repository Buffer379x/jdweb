package main

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/Buffer379x/jdweb/internal/testjd"
)

// captchaImage is a picture captcha as the data URL JDownloader hands out. An SVG keeps the fake free of image files.
func captchaImage(text string) string {
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="220" height="80" viewBox="0 0 220 80"><rect width="220" height="80" fill="#e8e0c8"/>`)
	b.WriteString(`<path d="M0 20 C60 60 120 0 220 50" stroke="#6a5a3a" fill="none" stroke-width="2"/><path d="M0 70 C80 10 150 90 220 25" stroke="#8a2a2a" fill="none" stroke-width="1.5"/>`)
	for i, r := range text {
		fmt.Fprintf(&b, `<text x="%d" y="%d" font-family="monospace" font-size="%d" font-weight="bold" fill="#222" transform="rotate(%d %d 40)">%c</text>`, 18+i*38, 52+(i%2)*8-4, 38+(i%3)*4, (i*13)%30-12, 18+i*38, r)
	}
	b.WriteString(`</svg>`)
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(b.String()))
}

func imageCaptcha(hoster, text string, timeout time.Duration) testjd.CaptchaJob {
	return testjd.CaptchaJob{Hoster: hoster, Type: "ImageCaptcha", Category: "image", ChallengeType: "image",
		Created: time.Now().UnixMilli(), Timeout: timeout.Milliseconds(), Remaining: int(timeout.Milliseconds()), Image: captchaImage(text)}
}

func interactiveCaptcha(hoster string, timeout time.Duration) testjd.CaptchaJob {
	return testjd.CaptchaJob{Hoster: hoster, Type: "RecaptchaV2Challenge", Category: "recaptchav2", ChallengeType: "recaptchav2",
		Explain: "Select all images with traffic lights.", Created: time.Now().UnixMilli(), Timeout: timeout.Milliseconds(), Remaining: int(timeout.Milliseconds())}
}

func addDialogs(s *testjd.Server) {
	s.StrictDialogOrder()
	s.AddDialog(testjd.Dialog{Type: "jd.controlling.downloadcontroller.IfFileExistsDialogInterface", Properties: map[string]string{
		"title": "File already exists", "message": "A file with this name exists already.\nWhat should happen to the new download?",
		"fileName": "bunny-2160p60.mp4", "path": "/mnt/downloads/Big Buck Bunny 4K/bunny-2160p60.mp4", "size": "1.31 GB",
	}})
	s.AddDialog(testjd.Dialog{Type: "org.appwork.uio.ConfirmDialogInterface", Properties: map[string]string{
		"title": "Disable premium?", "message": "Premium accounts will be ignored until you switch it on again.",
	}})
}

func seedExtras(s *testjd.Server, generated int) {
	// accounts
	now := time.Now()
	s.SetHosters([]string{"1fichier.com", "debrid.example", "mediafire.com", "rapidgator.net", "uploaded.net"})
	s.AddAccount(testjd.Account{Hostname: "debrid.example", Username: "demo", Enabled: true, Valid: true, TrafficLeft: 412 << 30, TrafficMax: 1 << 40, ValidUntil: now.Add(61 * 24 * time.Hour).UnixMilli()})
	s.AddAccount(testjd.Account{Hostname: "rapidgator.net", Username: "old-user", Enabled: true, Valid: false, ErrorType: "EXPIRED", ErrorString: "Account expired", TrafficLeft: 0, TrafficMax: 100 << 30, ValidUntil: now.Add(-9 * 24 * time.Hour).UnixMilli()})
	s.AddAccount(testjd.Account{Hostname: "1fichier.com", Username: "typo@example.org", Enabled: true, Valid: false, ErrorType: "INVALID", ErrorString: "Invalid username or password", TrafficLeft: -1, TrafficMax: -1, ValidUntil: -1})
	s.AddAccount(testjd.Account{Hostname: "mediafire.com", Username: "paused-account", Enabled: false, Valid: true, TrafficLeft: -1, TrafficMax: -1, ValidUntil: -1})
	s.AddBasicAuth(testjd.BasicAuth{Type: "HTTP", HostMask: "files.example.org", Username: "reader", Enabled: true, LastValidated: now.Add(-3 * time.Hour).UnixMilli()})
	s.AddBasicAuth(testjd.BasicAuth{Type: "FTP", HostMask: "ftp.example.net", Username: "anonymous", Enabled: true})

	// extensions
	const ext = "org.jdownloader.extensions.extraction.ExtractionConfig"
	s.AddExtension(testjd.Extension{ID: "org.jdownloader.extensions.extraction.ExtractionExtension", Name: "Archive Extractor", Description: "Extracts all usual types of archives (zip, rar, 7zip, ...)", Enabled: true, Installed: true, IconKey: "extract", ConfigInterface: ext})
	s.AddExtension(testjd.Extension{ID: "eventscripter", Name: "Event Scripter", Description: "Listens to internal JDownloader events and allows to run scripts.", IconKey: "event"})
	s.AddExtension(testjd.Extension{ID: "folderwatch", Name: "Folder Watch", Description: "Add links to JDownloader by putting link list files in a special folder.", IconKey: "folder_add"})
	s.AddExtension(testjd.Extension{ID: "scheduler", Name: "Scheduler", Description: "Define time schedules to execute actions, start downloads, ...", IconKey: "wait", Installed: true, ConfigInterface: "org.jdownloader.extensions.schedule.SchedulerConfig"})
	setExt := func(key string, v any, m testjd.ConfigMeta) {
		m.Extension = true
		s.SetConfig(ext, key, v)
		s.SetConfigMeta(ext, key, m)
	}
	setExt("DeepExtractionEnabled", true, testjd.ConfigMeta{Storage: "cfg/org.jdownloader.extensions.extraction.ExtractionExtension", AbstractType: "BOOLEAN", Type: "boolean", Docs: "Also extract archives found inside extracted folders."})
	setExt("BlacklistPatterns", []string{"##Lines with ## are comments", ".*sample/.*"}, testjd.ConfigMeta{Storage: "cfg/org.jdownloader.extensions.extraction.ExtractionExtension", AbstractType: "STRING_LIST", Type: "[Ljava.lang.String;", Docs: "A list of regular expressions. Use to avoid extracting certain filetypes."})
	setExt("MaxCheckedFileSizeDuringOptimizedPasswordFindingInBytes", int64(1<<20), testjd.ConfigMeta{Storage: "cfg/org.jdownloader.extensions.extraction.ExtractionExtension", AbstractType: "LONG", Type: "long", Docs: "How many bytes of a file are tested when looking for the right password."})
	setExt("FlushMode", "AUTO", testjd.ConfigMeta{Storage: "cfg/org.jdownloader.extensions.extraction.ExtractionExtension", AbstractType: "ENUM", Type: "x.FlushMode", EnumOptions: [][]string{{"AUTO", ""}, {"ALWAYS", ""}, {"NEVER", ""}}})

	// extraction queue and archives
	s.AddArchive(testjd.Archive{ID: "archiv.part1.rar", ExtractPath: "/mnt/downloads/archiv", FinalPassword: "hunter2", Passwords: []string{"hunter2", "secret-two"}})
	s.AddQueueItem(testjd.QueueItem{ArchiveID: "archiv.part1.rar", ArchiveName: "archiv.part1.rar", Type: "RAR_MULTI", ControllerID: 41, ControllerStatus: "RUNNING",
		States: map[string]string{"archiv.part1.rar": "COMPLETE", "archiv.part2.rar": "COMPLETE", "archiv.part3.rar": "COMPLETE", "archiv.part4.rar": "COMPLETE", "archiv.part5.rar": "INCOMPLETE"}})
	s.AddQueueItem(testjd.QueueItem{ArchiveID: "Projekt-Backup.zip", ArchiveName: "Projekt-Backup.zip", Type: "ZIP_SINGLE", ControllerID: 42, ControllerStatus: "QUEUED",
		States: map[string]string{"Projekt-Backup.zip": "COMPLETE"}})

	// general settings
	const gs = "org.jdownloader.settings.GeneralSettings"
	s.SetConfig(gs, "MaxSimultaneDownloadsPerHost", int64(1))
	s.SetConfig(gs, "DownloadSpeedLimit", int64(1536<<10))
	s.SetConfigMeta(gs, "MaxSimultaneDownloadsPerHost", testjd.ConfigMeta{AbstractType: "INT", Type: "int"})
	s.SetConfigMeta(gs, "MaxSimultaneDownloads", testjd.ConfigMeta{AbstractType: "INT", Type: "int"})
	s.SetConfigMeta(gs, "DownloadSpeedLimit", testjd.ConfigMeta{AbstractType: "INT", Type: "int"})
	s.SetConfigMeta(gs, "DefaultDownloadFolder", testjd.ConfigMeta{AbstractType: "STRING", Type: "java.lang.String"})
	more := func(iface, key string, v any, m testjd.ConfigMeta) {
		s.SetConfig(iface, key, v)
		s.SetConfigMeta(iface, key, m)
	}
	more(gs, "DelayWriteMode", "AUTO", testjd.ConfigMeta{AbstractType: "ENUM", Type: "x.DelayWriteMode", Docs: "Delay writes to disk of background tasks", EnumOptions: [][]string{{"OFF", ""}, {"ON", ""}, {"AUTO", ""}}})
	more(gs, "DownloadTempUnavailableRetryWaittime", int64(1800000), testjd.ConfigMeta{AbstractType: "LONG", Type: "long", Docs: "Waittime in ms if a download is temporarily unavailable"})
	more(gs, "UseOriginalLastModified", true, testjd.ConfigMeta{AbstractType: "BOOLEAN", Type: "boolean", Docs: "Use the original last modified date of files when writing them to disk."})
	more(gs, "BrowserCommandLine", []string{"firefox", "%s"}, testjd.ConfigMeta{AbstractType: "STRING_LIST", Type: "[Ljava.lang.String;", Docs: "Command line to open a link in a browser. Use %s as wildcard for the url."})
	more(gs, "PackagenameReplaceMap", map[string]any{"[<>:\"/|?*]": "_"}, testjd.ConfigMeta{AbstractType: "OBJECT", Type: "java.util.Map", Docs: "Characters that are replaced inside package names."})
	more("jd.controlling.reconnect.RouterSettings", "AutoReconnectEnabled", true, testjd.ConfigMeta{AbstractType: "BOOLEAN", Type: "boolean", Docs: "Reconnect automatically when a download asks for a new IP."})
	more("jd.controlling.reconnect.pluginsinc.liveheader.LiveHeaderReconnectSettings", "Password", "router-secret", testjd.ConfigMeta{AbstractType: "STRING", Type: "java.lang.String"})
	more("org.jdownloader.captcha.v2.solver.CaptchaSolverConfig", "ApiKey", "", testjd.ConfigMeta{AbstractType: "STRING", Type: "java.lang.String", Docs: "Key of the captcha solving service."})
	more("org.jdownloader.plugins.components.containers.ContainerConfig", "SFDLContainerPasswordList", []string{"pw-one", "pw-two"}, testjd.ConfigMeta{AbstractType: "STRING_LIST", Type: "java.util.List", Docs: "A list of passwords for .SFDL FTP containers."})
	more("org.jdownloader.api.myjdownloader.MyJDownloaderSettings", "DeviceConnectPorts", "[80,10101]", testjd.ConfigMeta{AbstractType: "INT_LIST", Type: "[I"})
	s.SetConfig("org.jdownloader.api.myjdownloader.MyJDownloaderSettings", "DeviceConnectPorts", []any{80.0, 10101.0})
	s.SetConfig("org.jdownloader.settings.GraphicalUserInterfaceSettings", "LowCreditsWarningThreshold", 0.5)
	s.SetConfigMeta("org.jdownloader.settings.GraphicalUserInterfaceSettings", "LowCreditsWarningThreshold", testjd.ConfigMeta{AbstractType: "DOUBLE", Type: "double", Docs: "Minimum credit before a warning shows."})

	// plugins and their settings
	for _, p := range []testjd.Plugin{
		{DisplayName: "youtube.com", ClassName: "jd.plugins.hoster.YoutubeDashV2", Pattern: "youtubev2://.+", Version: "53537"},
		{DisplayName: "1fichier.com", ClassName: "jd.plugins.hoster.OneFichierCom", Pattern: "https?://(?:www\\.)?1fichier\\.com/.+", Version: "53010"},
		{DisplayName: "mediafire.com", ClassName: "jd.plugins.hoster.MediafireCom", Pattern: "https?://(?:www\\.)?mediafire\\.com/.+", Version: "52877"},
		{DisplayName: "archive.org", ClassName: "jd.plugins.hoster.ArchiveOrg", Pattern: "https?://(?:www\\.)?archive\\.org/.+", Version: "53301"},
		{DisplayName: "debrid.example", ClassName: "jd.plugins.hoster.DebridExample", Pattern: "https?://debrid\\.example/.+", Version: "50100"},
	} {
		s.AddPlugin(p)
	}
	pl := func(plugin, iface, key string, v any, m testjd.ConfigMeta) {
		m.Plugin = plugin
		more(iface, key, v, m)
	}
	const yt = "org.jdownloader.plugins.components.youtube.YoutubeConfig"
	pl("youtube.com", yt, "PreferredVideoCodec", "H264", testjd.ConfigMeta{AbstractType: "ENUM", Type: "x.Codec", Docs: "Codec that is chosen when several are online.", EnumOptions: [][]string{{"H264", "H.264"}, {"VP9", "VP9"}, {"AV1", "AV1"}}})
	pl("youtube.com", yt, "SubtitlesEnabled", false, testjd.ConfigMeta{AbstractType: "BOOLEAN", Type: "boolean", Docs: "Also grab the subtitles of a video."})
	pl("youtube.com", yt, "MaxVideoHeight", int64(1080), testjd.ConfigMeta{AbstractType: "INT", Type: "int", Docs: "Highest video height that is taken by default."})
	pl("youtube.com", yt, "ApiToken", "tok-123456", testjd.ConfigMeta{AbstractType: "STRING", Type: "java.lang.String", Docs: "Token for the optional YouTube API."})
	pl("youtube.com", yt, "BlacklistedAudioBitrates", []string{}, testjd.ConfigMeta{AbstractType: "ENUM_LIST", Type: "java.util.List", EnumOptions: [][]string{{"KBIT_48", ""}, {"KBIT_128", ""}}})
	const of = "org.jdownloader.plugins.components.config.OneFichierConfigInterface"
	pl("1fichier.com", of, "SSLMode", "AUTO", testjd.ConfigMeta{AbstractType: "ENUM", Type: "x.SSLMode", Docs: "Auto uses whichever protocol the website provides.", EnumOptions: [][]string{{"AUTO", "Auto"}, {"FORCE_HTTP", "Force http"}, {"FORCE_HTTPS", "Force https"}}})
	pl("1fichier.com", of, "UseDirectLinks", true, testjd.ConfigMeta{AbstractType: "BOOLEAN", Type: "boolean"})
	pl("mediafire.com", "org.jdownloader.plugins.components.config.MediafireConfig", "ResolvePassword", false, testjd.ConfigMeta{AbstractType: "BOOLEAN", Type: "boolean", Docs: "Ask for the password of protected files."})

	if generated > 0 {
		generate(s, generated)
	}
}

// generate adds n settings of all kinds across many interfaces, the size of the real configuration.
func generate(s *testjd.Server, n int) {
	kinds := []struct {
		at, typ string
		val     func(i int) any
		opts    [][]string
	}{
		{"BOOLEAN", "boolean", func(i int) any { return i%3 == 0 }, nil},
		{"BOOLEAN", "boolean", func(i int) any { return i%2 == 0 }, nil},
		{"INT", "int", func(i int) any { return int64(i * 7 % 500) }, nil},
		{"STRING", "java.lang.String", func(i int) any { return fmt.Sprintf("value %d", i) }, nil},
		{"ENUM", "x.Mode", func(i int) any { return []string{"A", "B", "C"}[i%3] }, [][]string{{"A", "First"}, {"B", "Second"}, {"C", ""}}},
		{"LONG", "long", func(i int) any { return int64(i) * 1000 }, nil},
		{"STRING_LIST", "[Ljava.lang.String;", func(i int) any { return []string{"a", fmt.Sprint(i)} }, nil},
	}
	for i := 0; i < n; i++ {
		k := kinds[i%len(kinds)]
		iface := fmt.Sprintf("org.jdownloader.generated.Config%02d", i%40)
		key := fmt.Sprintf("Setting%04d%s", i, []string{"Enabled", "Limit", "Name", "Mode", "Timeout", "Items"}[i%6])
		s.SetConfig(iface, key, k.val(i))
		s.SetConfigMeta(iface, key, testjd.ConfigMeta{AbstractType: k.at, Type: k.typ, EnumOptions: k.opts,
			Docs: fmt.Sprintf("Generated setting number %d. It only exists to give the list of settings its real size, so the filter and the scrolling can be measured.", i)})
	}
}
