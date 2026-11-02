package testjd

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// The state in this file is opt-in: it is empty until a tool (fakejd) fills it,
// and an empty fake answers like before. It exists so that the web interface
// can be developed and photographed against accounts, plugins, extensions,
// extraction jobs and settings of every type.

// ConfigMeta describes a setting beyond the four basic kinds of SetConfig.
// With an AbstractType the fake accepts any JSON value that fits it.
type ConfigMeta struct {
	Storage      string
	Docs         string
	AbstractType string // BOOLEAN, INT, LONG, DOUBLE, STRING, ENUM, STRING_LIST, INT_LIST, OBJECT, ...
	Type         string
	EnumOptions  [][]string // pairs of name and label
	Extension    bool       // reported only when extensions are asked for
	Plugin       string     // display name: the setting belongs to plugins/query, not to config/query
}

// Account is a premium account of the fake, in the shape of accountsV2/listAccounts.
type Account struct {
	UUID        int64  `json:"uuid"`
	Hostname    string `json:"hostname"`
	Username    string `json:"username"`
	Enabled     bool   `json:"enabled"`
	Valid       bool   `json:"valid"`
	ErrorType   string `json:"errorType,omitempty"`
	ErrorString string `json:"errorString,omitempty"`
	TrafficLeft int64  `json:"trafficLeft"`
	TrafficMax  int64  `json:"trafficMax"`
	ValidUntil  int64  `json:"validUntil"`
}

// BasicAuth is a stored HTTP or FTP login of the fake.
type BasicAuth struct {
	ID            int64  `json:"id"`
	Type          string `json:"type"`
	HostMask      string `json:"hostmask"`
	Username      string `json:"username"`
	Enabled       bool   `json:"enabled"`
	Created       int64  `json:"created"`
	LastValidated int64  `json:"lastValidated"`
}

// Extension is an extension of the fake.
type Extension struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	Enabled         bool   `json:"enabled"`
	Installed       bool   `json:"installed"`
	IconKey         string `json:"iconKey"`
	ConfigInterface string `json:"configInterface"`
}

// Plugin is a hoster plugin of the fake.
type Plugin struct {
	DisplayName string `json:"displayName"`
	ClassName   string `json:"className"`
	Pattern     string `json:"pattern"`
	Version     string `json:"version"`
}

// QueueItem is an archive in the extraction queue.
type QueueItem struct {
	ArchiveID        string            `json:"archiveId"`
	ArchiveName      string            `json:"archiveName"`
	Type             string            `json:"type"`
	ControllerID     int64             `json:"controllerId"`
	ControllerStatus string            `json:"controllerStatus"`
	States           map[string]string `json:"states"`
}

type extras struct {
	accounts   []Account
	basic      []BasicAuth
	hosters    []string
	extensions []Extension
	plugins    []Plugin
	queue      []QueueItem
	strictQ    bool
}

// AddAccount adds an account to the fake and returns its id.
func (s *Server) AddAccount(a Account) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a.UUID == 0 {
		a.UUID = s.nextID()
	}
	s.x.accounts = append(s.x.accounts, a)
	return a.UUID
}

// AddBasicAuth adds a stored login to the fake.
func (s *Server) AddBasicAuth(b BasicAuth) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b.ID == 0 {
		b.ID = s.nextID()
	}
	s.x.basic = append(s.x.basic, b)
}

// SetHosters sets the host names listPremiumHoster answers with.
func (s *Server) SetHosters(h []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.x.hosters = h
}

// AddExtension adds an extension to the fake.
func (s *Server) AddExtension(e Extension) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.x.extensions = append(s.x.extensions, e)
}

// AddPlugin adds a plugin to the fake.
func (s *Server) AddPlugin(p Plugin) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.x.plugins = append(s.x.plugins, p)
}

// AddQueueItem puts an archive into the extraction queue.
func (s *Server) AddQueueItem(q QueueItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.x.queue = append(s.x.queue, q)
}

// SetConfigMeta describes a setting. Call it after SetConfig.
func (s *Server) SetConfigMeta(iface, key string, m ConfigMeta) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.meta[iface+"/"+key] = m
}

// StrictDialogOrder makes the fake answer 400 "Answer Dialog <id> first" when a
// dialog other than the oldest is answered, as JDownloader does.
func (s *Server) StrictDialogOrder() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.x.strictQ = true
}

func (s *Server) setMeta(k string, m ConfigMeta, raw json.RawMessage) bool {
	switch m.AbstractType {
	case "BOOLEAN":
		var v bool
		if json.Unmarshal(raw, &v) != nil {
			return false
		}
		s.config[k] = v
	case "INT", "LONG":
		var v int64
		if json.Unmarshal(raw, &v) != nil {
			return false
		}
		s.config[k] = v
	case "DOUBLE":
		var v float64
		if json.Unmarshal(raw, &v) != nil {
			return false
		}
		s.config[k] = v
	case "STRING", "HEX_COLOR":
		var v string
		if json.Unmarshal(raw, &v) != nil {
			return false
		}
		s.config[k] = v
	case "ENUM":
		var v string
		if json.Unmarshal(raw, &v) != nil {
			return false
		}
		for _, o := range m.EnumOptions {
			if len(o) > 0 && o[0] == v {
				s.config[k] = v
				return true
			}
		}
		return false
	default:
		var v any
		if json.Unmarshal(raw, &v) != nil {
			return false
		}
		s.config[k] = v
	}
	return true
}

func abstractOf(v any) (string, string) {
	switch v.(type) {
	case bool:
		return "boolean", "BOOLEAN"
	case int64:
		return "int", "INT"
	case []string:
		return "java.util.List<java.lang.String>", "STRING_LIST"
	}
	return "java.lang.String", "STRING"
}

// queryConfig renders the settings that pass keep, sorted by interface and key.
func (s *Server) queryConfig(keep func(iface, key string, m ConfigMeta) bool, values, defaults bool) []any {
	var keys []string
	for k := range s.config {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := []any{}
	for _, k := range keys {
		iface, key, _ := strings.Cut(k, "/")
		m := s.meta[k]
		if !keep(iface, key, m) {
			continue
		}
		e := map[string]any{"interfaceName": iface, "key": key}
		t, at := abstractOf(s.config[k])
		if m.AbstractType != "" {
			at = m.AbstractType
			t = m.Type
		}
		e["type"], e["abstractType"] = t, at
		e["storage"] = nilIfEmpty(m.Plugin, m.Storage)
		e["docs"] = m.Docs
		if len(m.EnumOptions) > 0 {
			e["enumOptions"] = m.EnumOptions
		}
		if values {
			e["value"] = s.config[k]
		}
		if defaults {
			e["defaultValue"] = s.defaults[k]
		}
		out = append(out, e)
	}
	return out
}

func nilIfEmpty(parts ...string) any {
	for _, p := range parts {
		if p != "" {
			return p
		}
	}
	return nil
}

func (s *Server) eachID(ids []int64, fn func(i int)) {
	set := idSet(ids)
	for i := len(s.x.accounts) - 1; i >= 0; i-- {
		if set[s.x.accounts[i].UUID] {
			fn(i)
		}
	}
}

func registerExtras() {
	register("/accountsV2/listAccounts", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		return append([]Account{}, s.x.accounts...), nil
	})
	register("/accountsV2/listPremiumHoster", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		return append([]string{}, s.x.hosters...), nil
	})
	register("/accountsV2/addAccount", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var host, user, pass string
		if err := a.scan(&host, &user, &pass); err != nil {
			return nil, err
		}
		s.x.accounts = append(s.x.accounts, Account{UUID: s.nextID(), Hostname: host, Username: user, Enabled: true, Valid: true, TrafficLeft: -1, TrafficMax: -1, ValidUntil: -1})
		return "", nil
	})
	register("/accountsV2/setUserNameAndPassword", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var id int64
		var user, pass string
		if err := a.scan(&id, &user, &pass); err != nil {
			return nil, err
		}
		s.eachID([]int64{id}, func(i int) { s.x.accounts[i].Username = user })
		return "", nil
	})
	for path, enabled := range map[string]bool{"/accountsV2/enableAccounts": true, "/accountsV2/disableAccounts": false} {
		enabled := enabled
		register(path, func(_ context.Context, s *Server, a args) (any, *apiErr) {
			var ids []int64
			if err := a.scan(&ids); err != nil {
				return nil, err
			}
			s.eachID(ids, func(i int) { s.x.accounts[i].Enabled = enabled })
			return "", nil
		})
	}
	register("/accountsV2/removeAccounts", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var ids []int64
		if err := a.scan(&ids); err != nil {
			return nil, err
		}
		s.eachID(ids, func(i int) { s.x.accounts = append(s.x.accounts[:i], s.x.accounts[i+1:]...) })
		return "", nil
	})

	register("/accountsV2/listBasicAuth", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		return append([]BasicAuth{}, s.x.basic...), nil
	})
	register("/accountsV2/addBasicAuth", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var typ, mask, user, pass string
		if err := a.scan(&typ, &mask, &user, &pass); err != nil {
			return nil, err
		}
		id := s.nextID()
		s.x.basic = append(s.x.basic, BasicAuth{ID: id, Type: typ, HostMask: mask, Username: user, Enabled: true, Created: id})
		return id, nil
	})
	register("/accountsV2/updateBasicAuth", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var b struct {
			ID       int64  `json:"id"`
			Type     string `json:"type"`
			HostMask string `json:"hostmask"`
			Username string `json:"username"`
			Enabled  bool   `json:"enabled"`
		}
		if err := a.scan(&b); err != nil {
			return nil, err
		}
		for i := range s.x.basic {
			if s.x.basic[i].ID == b.ID {
				s.x.basic[i].Type, s.x.basic[i].HostMask, s.x.basic[i].Username, s.x.basic[i].Enabled = b.Type, b.HostMask, b.Username, b.Enabled
			}
		}
		return "", nil
	})
	register("/accountsV2/removeBasicAuths", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var ids []int64
		if err := a.scan(&ids); err != nil {
			return nil, err
		}
		set := idSet(ids)
		kept := s.x.basic[:0]
		for _, b := range s.x.basic {
			if !set[b.ID] {
				kept = append(kept, b)
			}
		}
		s.x.basic = kept
		return "", nil
	})

	register("/extensions/list", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		return append([]Extension{}, s.x.extensions...), nil
	})
	register("/extensions/install", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var id string
		if err := a.scan(&id); err != nil {
			return nil, err
		}
		for i := range s.x.extensions {
			if s.x.extensions[i].ID == id {
				s.x.extensions[i].Installed, s.x.extensions[i].Enabled = true, true
			}
		}
		return true, nil
	})
	register("/extensions/setEnabled", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var id string
		var on bool
		if err := a.scan(&id, &on); err != nil {
			return nil, err
		}
		for i := range s.x.extensions {
			if s.x.extensions[i].ID == id {
				if !s.x.extensions[i].Installed {
					return false, nil
				}
				s.x.extensions[i].Enabled = on
			}
		}
		return true, nil
	})

	register("/plugins/list", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		return append([]Plugin{}, s.x.plugins...), nil
	})
	register("/plugins/query", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var q struct {
			Pattern         string `json:"pattern"`
			ConfigInterface string `json:"configInterface"`
			Values          bool   `json:"values"`
			DefaultValues   bool   `json:"defaultValues"`
		}
		if err := a.scan(&q); err != nil {
			return nil, err
		}
		re, err := regexp.Compile(q.Pattern)
		if err != nil {
			return nil, badParams(q.Pattern)
		}
		return s.queryConfig(func(iface, key string, m ConfigMeta) bool {
			if m.Plugin == "" || (q.ConfigInterface != "" && q.ConfigInterface != iface) {
				return false
			}
			return re.MatchString(key) || re.MatchString(iface+"."+key)
		}, q.Values, q.DefaultValues), nil
	})
	register("/plugins/set", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var iface, plugin, key string
		var raw json.RawMessage
		if err := a.scan(&iface, &plugin, &key, &raw); err != nil {
			return nil, err
		}
		k := iface + "/" + key
		m, ok := s.meta[k]
		if !ok || m.Plugin == "" {
			return true, nil // a plugin setting the fake does not model: accepted like before
		}
		if m.Plugin != plugin {
			return false, nil
		}
		return s.setMeta(k, m, raw), nil
	})
	register("/plugins/reset", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var iface, plugin, key string
		if err := a.scan(&iface, &plugin, &key); err != nil {
			return nil, err
		}
		k := iface + "/" + key
		m, ok := s.meta[k]
		if !ok || m.Plugin == "" {
			return true, nil
		}
		if m.Plugin != plugin {
			return false, nil
		}
		s.config[k] = s.defaults[k]
		return true, nil
	})

	register("/extraction/getQueue", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		return append([]QueueItem{}, s.x.queue...), nil
	})
	register("/extraction/cancelExtraction", func(_ context.Context, s *Server, a args) (any, *apiErr) {
		var id int64
		if err := a.scan(&id); err != nil {
			return nil, err
		}
		for i, q := range s.x.queue {
			if q.ControllerID == id {
				s.x.queue = append(s.x.queue[:i], s.x.queue[i+1:]...)
				return true, nil
			}
		}
		return len(s.x.queue) == 0, nil // an empty fake accepts every id, as before
	})
}

// firstDialog reports the id JDownloader wants answered first, or 0.
func (s *Server) firstDialog() int64 {
	if len(s.dialogs) == 0 {
		return 0
	}
	return s.dialogs[0].ID
}

func (s *Server) wrongDialogOrder(id int64) *apiErr {
	if s.x.strictQ && len(s.dialogs) > 0 && s.dialogs[0].ID != id {
		return &apiErr{400, fmt.Sprintf("Answer Dialog %d first", s.firstDialog()), nil}
	}
	return nil
}
