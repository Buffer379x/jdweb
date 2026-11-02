package testjd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

var startedAt = time.Now()

func init() {
	register("/device/ping", func(_ context.Context, s *Server, a args) (any, *apiErr) { return true, nil })
	register("/jd/getCoreRevision", func(_ context.Context, s *Server, a args) (any, *apiErr) { return s.revision, nil })
	register("/jd/uptime", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		return time.Since(startedAt).Milliseconds(), nil
	})
	register("/system/getSystemInfos", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		return map[string]any{
			"archFamily": "X86", "archString": "amd64", "hardware": nil, "javaName": "OpenJDK 64-Bit Server VM",
			"javaVendor": "IcedTea", "javaVersion": 18492009, "javaVersionString": "1.8.0_492-b09",
			"operatingSystem": "ALPINE", "osFamily": "LINUX", "osString": "Linux",
			"heapCommitted": 438829056, "heapMax": 2800222208, "heapUsed": 141712024,
			"startupTimeStamp": startedAt.UnixMilli(), "arch64Bit": true, "docker": !s.notDocker, "jvm64Bit": true, "os64Bit": true,
		}, nil
	})
	register("/system/getStorageInfos", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var p string
		if err := a.scan(&p); err != nil {
			return nil, err
		}
		return []any{map[string]any{"error": nil, "free": 257377894400, "path": p, "size": 311653564416}}, nil
	})
	for _, p := range []string{"/system/restartJD", "/system/exitJD", "/system/hibernateOS", "/system/standbyOS", "/system/shutdownOS",
		"/update/runUpdateCheck", "/update/restartAndUpdate", "/reconnect/doReconnect"} {
		register(p, func(_ context.Context, s *Server, a args) (any, *apiErr) { return "", nil })
	}
	register("/update/isUpdateAvailable", func(_ context.Context, s *Server, a args) (any, *apiErr) { return false, nil })

	// Calls that change settings of JDownloader this fake does not model: they
	// are logged and answer success.
	for _, p := range []string{"/accountsV2/refreshAccounts", "/extraction/addArchivePassword"} {
		register(p, func(_ context.Context, s *Server, a args) (any, *apiErr) { return "", nil })
	}
	for _, p := range []string{"/linkgrabberv2/setVariant", "/linkgrabberv2/addVariantCopy"} {
		register(p, func(_ context.Context, s *Server, a args) (any, *apiErr) { return "", nil })
	}
	register("/extraction/startExtractionNow", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		return map[string]bool{}, nil
	})
	register("/extraction/getArchiveInfo", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		out := []any{}
		for _, ar := range s.archives {
			out = append(out, map[string]any{"archiveId": ar.ID, "archiveName": ar.ID, "type": "ZIP_SINGLE", "controllerId": -1, "controllerStatus": "NA"})
		}
		return out, nil
	})
	register("/extraction/getArchiveSettings", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var ids []string
		if err := a.scan(&ids); err != nil {
			return nil, err
		}
		out := []any{}
		for _, ar := range s.archives {
			for _, id := range ids {
				if id == ar.ID {
					out = append(out, ar.settings())
				}
			}
		}
		return out, nil
	})
	register("/extraction/setArchiveSettings", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var id string
		var v struct {
			ExtractPath   string   `json:"extractPath"`
			FinalPassword string   `json:"finalPassword"`
			Passwords     []string `json:"passwords"`
		}
		if err := a.scan(&id, &v); err != nil {
			return nil, err
		}
		for i := range s.archives {
			if s.archives[i].ID == id {
				if v.ExtractPath != "" {
					s.archives[i].ExtractPath = v.ExtractPath
				}
				if v.FinalPassword != "" {
					s.archives[i].FinalPassword = v.FinalPassword
				}
				s.archives[i].Passwords = v.Passwords
			}
		}
		return true, nil
	})

	// Icons answer with image bytes, not JSON. The fake sends a 1x1 PNG.
	for _, p := range []string{"/contentV2/getFavIcon", "/contentV2/getFileIcon"} {
		register(p, func(_ context.Context, s *Server, a args) (any, *apiErr) {
			var name string
			if err := a.scan(&name); err != nil {
				return nil, err
			}
			return binaryResult{"image/png", pixelPNG}, nil
		})
	}

	registerController()
	registerEvents()
	registerCaptchaDialogs()
	registerConfig()
	registerExtras()
}

// ---- download controller and toolbar

func registerController() {
	register("/downloadcontroller/start", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		s.setController("RUNNING")
		return true, nil
	})
	register("/downloadcontroller/stop", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		s.setController("STOPPED_STATE")
		return true, nil
	})
	register("/downloadcontroller/pause", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var pause bool
		if err := a.scan(&pause); err != nil {
			return nil, err
		}
		switch {
		case pause:
			s.setController("PAUSE")
		case s.controller == "PAUSE":
			s.setController("RUNNING")
		}
		return true, nil
	})
	register("/downloadcontroller/getCurrentState", func(_ context.Context, s *Server, a args) (any, *apiErr) { return s.controller, nil })
	register("/downloadcontroller/getSpeedInBps", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		return s.totalSpeed(), nil
	})
	register("/downloadcontroller/forceDownload", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var linkIDs, pkgIDs []int64
		if err := a.scan(&linkIDs, &pkgIDs); err != nil {
			return nil, err
		}
		s.eachLink(Downloads, linkIDs, pkgIDs, func(l *Link) { l.Running = true })
		return "", nil
	})
	register("/toolbar/getStatus", func(_ context.Context, s *Server, a args) (any, *apiErr) { return s.toolbar(), nil })
	toggle := func(path string, flag func(s *Server) *bool) {
		register(path, func(_ context.Context, s *Server, a args) (any, *apiErr) {
			f := flag(s)
			*f = !*f
			return *f, nil
		})
	}
	toggle("/toolbar/togglePremium", func(s *Server) *bool { return &s.toggles.premium })
	toggle("/toolbar/toggleClipboardMonitoring", func(s *Server) *bool { return &s.toggles.clipboard })
	toggle("/toolbar/toggleAutomaticReconnect", func(s *Server) *bool { return &s.toggles.reconnect })
	toggle("/toolbar/toggleStopAfterCurrentDownload", func(s *Server) *bool { return &s.toggles.stopAfter })
	register("/toolbar/toggleDownloadSpeedLimit", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		on, _ := s.config[speedLimitEnabledKey].(bool)
		s.config[speedLimitEnabledKey] = !on
		return !on, nil
	})
}

func (s *Server) totalSpeed() int64 {
	var n int64
	for _, p := range s.list(Downloads).pkgs {
		for _, lk := range p.links {
			n += lk.Speed
		}
	}
	return n
}

// setController changes the controller state and announces it.
func (s *Server) setController(state string) {
	if s.controller == state {
		return
	}
	s.controller = state
	ev := map[string]string{"RUNNING": "RUNNING", "STOPPED_STATE": "STOPPED", "PAUSE": "PAUSED", "IDLE": "STOPPED"}[state]
	if ev != "" {
		s.emit("downloadwatchdog", ev, nil)
	}
}

// SetController sets the state of the download controller (IDLE, RUNNING,
// PAUSE, STOPPING, STOPPED_STATE) and sends the matching watchdog event.
func (s *Server) SetController(state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setController(state)
}

// Controller returns the state of the download controller.
func (s *Server) Controller() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.controller
}

func (s *Server) toolbar() map[string]any {
	var current, complete int64
	for _, p := range s.list(Downloads).pkgs {
		for _, lk := range p.links {
			if lk.Finished {
				complete += lk.BytesLoaded
			} else {
				current += lk.BytesLoaded
			}
		}
	}
	m := map[string]any{"state": s.controller, "speed": s.totalSpeed(), "download_current": current, "download_complete": complete}
	put(m, "running", s.controller == "RUNNING")
	put(m, "pause", s.controller == "PAUSE")
	put(m, "premium", s.toggles.premium)
	put(m, "reconnect", s.toggles.reconnect)
	put(m, "clipboard", s.toggles.clipboard)
	put(m, "stopafter", s.toggles.stopAfter)
	on, _ := s.config[speedLimitEnabledKey].(bool)
	put(m, "limit", on)
	if on {
		m["limitspeed"] = s.config[speedLimitKey]
	} else {
		m["limitspeed"] = 0
	}
	return m
}

// ---- events

func registerEvents() {
	subInfo := func(s *Server, sb *sub) map[string]any {
		return map[string]any{
			"exclusions": nil, "maxKeepalive": 120000, "maxPolltimeout": s.pollWait.Milliseconds(),
			"subscriptionid": sb.id, "subscriptions": sb.names(), "subscribed": true,
		}
	}
	register("/events/subscribe", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var pubs, excl []string
		if err := a.scan(&pubs, &excl); err != nil {
			return nil, err
		}
		sb := newSub(s.nextID(), pubs, excl)
		s.subs[sb.id] = sb
		return subInfo(s, sb), nil
	})
	register("/events/setsubscription", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var id int64
		var pubs, excl []string
		if err := a.scan(&id, &pubs, &excl); err != nil {
			return nil, err
		}
		sb := s.subs[id]
		if sb == nil {
			return map[string]any{"exclusions": nil, "maxKeepalive": -1, "maxPolltimeout": -1, "subscriptionid": -1, "subscriptions": nil, "subscribed": false}, nil
		}
		sb.set(pubs, excl)
		return subInfo(s, sb), nil
	})
	register("/events/getsubscriptionstatus", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var id int64
		if err := a.scan(&id); err != nil {
			return nil, err
		}
		// "subscribed" is false even for a live subscription, as in JDownloader revision 50639.
		if sb := s.subs[id]; sb != nil {
			return map[string]any{"queueSize": len(sb.queue), "subscriptionid": id, "subscribed": false}, nil
		}
		return map[string]any{"queueSize": 0, "subscriptionid": -1, "subscribed": false}, nil
	})
	register("/events/unsubscribe", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var id int64
		if err := a.scan(&id); err != nil {
			return nil, err
		}
		if sb := s.subs[id]; sb != nil {
			sb.close()
			delete(s.subs, id)
			return subInfo(s, sb), nil
		}
		return map[string]any{"exclusions": nil, "maxKeepalive": -1, "maxPolltimeout": -1, "subscriptionid": -1, "subscriptions": nil, "subscribed": false}, nil
	})
	register("/events/listpublisher", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		return []any{
			map[string]any{"publisher": "captchas", "eventids": []string{"NEW", "DONE"}},
			map[string]any{"publisher": "downloadwatchdog", "eventids": []string{"PAUSED", "RUNNING", "STOPPED", "UPDATE"}},
			map[string]any{"publisher": "downloads", "eventids": []string{"REFRESH_STRUCTURE", "REMOVE_CONTENT", "REMOVE_PACKAGE", "REMOVE_LINK", "ADD_CONTENT", "ADD_PACKAGE", "ADD_LINK", "REFRESH_CONTENT", "LINK_UPDATE", "PACKAGE_UPDATE"}},
			map[string]any{"publisher": "linkcollector", "eventids": []string{"CONTENT_REMOVED", "DUPE_ADDED", "ABORT", "CONTENT_ADDED", "LINK_ADDED", "STRUCTURE_REFRESH", "LIST_LOADED", "DATA_REFRESH"}},
			map[string]any{"publisher": "linkcrawler", "eventids": []string{"STARTED", "STOPPED", "FINISHED"}},
			map[string]any{"publisher": "dialogs", "eventids": []string{"NEW", "EXPIRED"}},
		}, nil
	})
	register("/downloadevents/setStatusEventInterval", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var id, ms int64
		if err := a.scan(&id, &ms); err != nil {
			return nil, err
		}
		if sb := s.subs[id]; sb != nil {
			sb.interval = ms
		}
		return true, nil
	})
	handlers["/events/listen"] = handler{blocking: true, fn: listen}
}

// listen is the long-poll: it answers as soon as the queue holds events, with
// an empty list after the poll timeout, and with 404 when the subscription is
// unknown or goes away while waiting.
func listen(ctx context.Context, s *Server, a args) (any, *apiErr) {
	var id int64
	if err := a.scan(&id); err != nil {
		return nil, err
	}
	s.mu.Lock()
	wait := s.pollWait
	s.mu.Unlock()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		s.mu.Lock()
		sb := s.subs[id]
		if sb == nil {
			s.mu.Unlock()
			return nil, &apiErr{404, "FILE_NOT_FOUND", nil}
		}
		if len(sb.queue) > 0 {
			out := sb.queue
			sb.queue = nil
			s.mu.Unlock()
			return out, nil
		}
		wake, gone := sb.wake, sb.gone
		s.mu.Unlock()
		select {
		case <-wake:
		case <-gone:
		case <-timer.C:
			return []Event{}, nil
		case <-ctx.Done():
			return []Event{}, nil
		}
	}
}

// ---- captchas and dialogs

// CaptchaJob is a captcha the fake waits for. Image is the data URL captcha/get returns.
type CaptchaJob struct {
	ID            int64  `json:"id"`
	Hoster        string `json:"hoster,omitempty"`
	Type          string `json:"type,omitempty"`
	Category      string `json:"captchaCategory,omitempty"`
	ChallengeType string `json:"challengeType,omitempty"`
	Explain       string `json:"explain,omitempty"`
	Created       int64  `json:"created,omitempty"`
	Timeout       int64  `json:"timeout,omitempty"`
	Remaining     int    `json:"remaining,omitempty"`
	Link          int64  `json:"link,omitempty"`
	Image         string `json:"-"`
}

// Dialog is a question the fake waits for.
type Dialog struct {
	ID         int64
	Type       string
	Properties map[string]string
}

// AddCaptcha adds a waiting captcha and sends the captchas NEW event. The id is assigned when 0.
func (s *Server) AddCaptcha(j CaptchaJob) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j.ID == 0 {
		j.ID = s.nextID()
	}
	s.captchas = append(s.captchas, j)
	s.emit("captchas", "NEW", map[string]any{"id": j.ID})
	return j.ID
}

// AddDialog adds an open dialog and sends the dialogs NEW event. The id is assigned when 0.
func (s *Server) AddDialog(d Dialog) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d.ID == 0 {
		d.ID = s.nextID()
	}
	s.dialogs = append(s.dialogs, d)
	s.emit("dialogs", "NEW", map[string]any{"id": d.ID})
	return d.ID
}

func (s *Server) takeCaptcha(id int64) (CaptchaJob, bool) {
	for i, c := range s.captchas {
		if c.ID == id {
			s.captchas = append(s.captchas[:i], s.captchas[i+1:]...)
			s.emit("captchas", "DONE", map[string]any{"id": id})
			return c, true
		}
	}
	return CaptchaJob{}, false
}

func (s *Server) findCaptcha(id int64) (CaptchaJob, bool) {
	for _, c := range s.captchas {
		if c.ID == id {
			return c, true
		}
	}
	return CaptchaJob{}, false
}

var notAvailable = &apiErr{404, "NOT_AVAILABLE", nil}

func registerCaptchaDialogs() {
	register("/captcha/list", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		out := []CaptchaJob{}
		return append(out, s.captchas...), nil
	})
	register("/captcha/getCaptchaJob", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var id int64
		if err := a.scan(&id); err != nil {
			return nil, err
		}
		if c, ok := s.findCaptcha(id); ok {
			return c, nil
		}
		return nil, nil
	})
	register("/captcha/get", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var id int64
		if err := a.scan(&id); err != nil {
			return nil, err
		}
		if c, ok := s.findCaptcha(id); ok && c.Image != "" {
			return c.Image, nil
		}
		return nil, notAvailable
	})
	register("/captcha/solve", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var id int64
		var result string
		if err := a.scan(&id, &result); err != nil {
			return nil, err
		}
		if _, ok := s.takeCaptcha(id); !ok {
			return nil, notAvailable
		}
		return true, nil
	})
	register("/captcha/skip", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var id int64
		var how string
		if err := a.scan(&id, &how); err != nil {
			return nil, err
		}
		if !validSkip[how] {
			return nil, badParams(string(mustMarshal(how)))
		}
		if _, ok := s.takeCaptcha(id); !ok {
			return nil, notAvailable
		}
		return true, nil
	})

	register("/dialogs/list", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		out := []int64{}
		for _, d := range s.dialogs {
			out = append(out, d.ID)
		}
		return out, nil
	})
	register("/dialogs/get", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var id int64
		var icon, props bool
		if err := a.scan(&id, &icon, &props); err != nil {
			return nil, err
		}
		for _, d := range s.dialogs {
			if d.ID == id {
				m := map[string]any{"type": d.Type}
				if props {
					m["properties"] = d.Properties
				}
				return m, nil
			}
		}
		return nil, &apiErr{400, "Invalid ID: " + string(mustMarshal(id)), nil}
	})
	register("/dialogs/getTypeInfo", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var typ string
		if err := a.scan(&typ); err != nil {
			return nil, err
		}
		for _, d := range s.dialogs {
			if d.Type == typ {
				out := map[string]string{}
				for k := range d.Properties {
					out[k] = "class java.lang.String"
				}
				return map[string]any{"in": map[string]string{"closereason": "class org.appwork.uio.CloseReason"}, "out": out}, nil
			}
		}
		return nil, &apiErr{400, "Requested Type not found!", nil}
	})
	register("/dialogs/answer", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var id int64
		var data map[string]any
		if err := a.scan(&id, &data); err != nil {
			return nil, err
		}
		for i, d := range s.dialogs {
			if d.ID == id {
				if err := s.wrongDialogOrder(id); err != nil {
					return nil, err
				}
				s.dialogs = append(s.dialogs[:i], s.dialogs[i+1:]...)
				s.emit("dialogs", "EXPIRED", map[string]any{"id": id})
				return "", nil
			}
		}
		return nil, &apiErr{400, "Invalid ID: " + string(mustMarshal(id)), nil}
	})
}

var validSkip = map[string]bool{"SINGLE": true, "BLOCK_HOSTER": true, "BLOCK_ALL_CAPTCHAS": true, "BLOCK_PACKAGE": true, "REFRESH": true, "STOP_CURRENT_ACTION": true, "TIMEOUT": true}

// ---- config

const (
	generalSettings      = "org.jdownloader.settings.GeneralSettings"
	speedLimitKey        = generalSettings + "/DownloadSpeedLimit"
	speedLimitEnabledKey = generalSettings + "/DownloadSpeedLimitEnabled"
)

func defaultConfig() map[string]any {
	return map[string]any{
		speedLimitKey:        int64(51200),
		speedLimitEnabledKey: false,
		generalSettings + "/MaxSimultaneDownloads": int64(3),
		generalSettings + "/DefaultDownloadFolder": "/output",
	}
}

func registerConfig() {
	register("/config/get", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var iface, key string
		var storage *string
		if err := a.scan(&iface, &storage, &key); err != nil {
			return nil, err
		}
		if m, ok := s.meta[iface+"/"+key]; ok && m.Plugin != "" {
			// A real JDownloader answers an empty body for a plugin setting: its
			// plugin storage is not one config/get can name. plugins/query has the value.
			return nil, nil
		}
		if v, ok := s.config[iface+"/"+key]; ok {
			return v, nil
		}
		return nil, nil
	})
	register("/config/set", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var iface, key string
		var storage *string
		var raw json.RawMessage
		if err := a.scan(&iface, &storage, &key, &raw); err != nil {
			return nil, err
		}
		k := iface + "/" + key
		old, ok := s.config[k]
		if !ok {
			return false, nil
		}
		if m, ok := s.meta[k]; ok && m.AbstractType != "" {
			return s.setMeta(k, m, raw), nil
		}
		switch old.(type) {
		case bool:
			var v bool
			if json.Unmarshal(raw, &v) != nil {
				return false, nil
			}
			s.config[k] = v
		case int64:
			var v int64
			if json.Unmarshal(raw, &v) != nil {
				return false, nil
			}
			s.config[k] = v
		case string:
			var v string
			if json.Unmarshal(raw, &v) != nil {
				return false, nil
			}
			s.config[k] = v
		case []string:
			var v []string
			if json.Unmarshal(raw, &v) != nil {
				return false, nil
			}
			s.config[k] = v
		}
		return true, nil
	})
	register("/config/reset", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var iface, key string
		var storage *string
		if err := a.scan(&iface, &storage, &key); err != nil {
			return nil, err
		}
		k := iface + "/" + key
		def, ok := s.defaults[k]
		if !ok {
			return false, nil
		}
		s.config[k] = def
		return true, nil
	})
	register("/config/query", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var q struct {
			Pattern           string `json:"pattern"`
			ConfigInterface   string `json:"configInterface"`
			IncludeExtensions bool   `json:"includeExtensions"`
			Values            bool   `json:"values"`
			DefaultValues     bool   `json:"defaultValues"`
		}
		if err := a.scan(&q); err != nil {
			return nil, err
		}
		re, err := regexp.Compile(q.Pattern)
		if err != nil {
			return nil, badParams(q.Pattern)
		}
		return s.queryConfig(func(iface, key string, m ConfigMeta) bool {
			if m.Plugin != "" || (m.Extension && !q.IncludeExtensions) {
				return false
			}
			if q.ConfigInterface != "" && q.ConfigInterface != iface {
				return false
			}
			return re.MatchString(key) || re.MatchString(iface+"."+key)
		}, q.Values, q.DefaultValues), nil
	})
}

// Archive is an archive known to the fake, with its settings.
type Archive struct {
	ID            string
	ExtractPath   string
	FinalPassword string
	Passwords     []string
}

func (a Archive) settings() map[string]any {
	return map[string]any{"archiveId": a.ID, "extractPath": a.ExtractPath, "finalPassword": a.FinalPassword, "passwords": a.Passwords}
}

// AddArchive adds an archive to the fake: extraction/getArchiveInfo lists all
// of them, whatever ids are asked for.
func (s *Server) AddArchive(a Archive) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.archives = append(s.archives, a)
}

// Archives returns the archives with the settings that were saved.
func (s *Server) Archives() []Archive {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Archive(nil), s.archives...)
}

// SetConfig adds or replaces a setting of the fake, and makes it the default
// the setting is reset to. The type follows the value: bool, int64, string or
// []string (reported as STRING_LIST).
func (s *Server) SetConfig(iface, key string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config[iface+"/"+key] = value
	s.defaults[iface+"/"+key] = value
}

// Config returns the value of a config key of the fake, for example
// Config("org.jdownloader.settings.GeneralSettings", "DownloadSpeedLimit").
func (s *Server) Config(iface, key string) any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.config[iface+"/"+key]
}

// decodeContainer reads the data URL of addContainer. The fake treats the
// content as text with one url per line instead of a real container.
func decodeContainer(content string) ([]string, bool) {
	head, data, ok := strings.Cut(content, ",")
	if !ok || !strings.HasPrefix(head, "data:") || !strings.HasSuffix(head, ";base64") {
		return nil, false
	}
	b, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, false
	}
	return strings.Fields(string(b)), true
}

// pixelPNG is a valid 1x1 transparent PNG.
var pixelPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0b, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x60, 0x00, 0x02, 0x00,
	0x00, 0x05, 0x00, 0x01, 0x7a, 0x5e, 0xab, 0x3f, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44,
	0xae, 0x42, 0x60, 0x82,
}
