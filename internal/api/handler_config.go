package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/Buffer379x/jdweb/internal/jd"
)

// maskValue stands in for the value of a secret setting.
const maskValue = "••••••" // six bullets

// secretKey matches the keys whose values are shown masked (Concept 9.4).
var secretKey = regexp.MustCompile(`(?i)password|apikey|secret|token`)

// plainTypes are the setting types that cannot hold a text secret. Everything
// else, an unknown or empty type included, is masked when the key matches: a
// new type of JDownloader must not make a secret visible.
var plainTypes = map[string]bool{
	"BOOLEAN": true, "INT": true, "LONG": true, "DOUBLE": true, "FLOAT": true, "BYTE": true, "SHORT": true,
	"INT_LIST": true, "ENUM": true, "ENUM_LIST": true,
}

// isSecret decides on key and type. A switch or a number is no secret even if
// its key says so (for example PasswordProtectionEnabled).
func isSecret(key, abstractType string) bool {
	return !plainTypes[abstractType] && secretKey.MatchString(key)
}

func isListType(abstractType string, raw json.RawMessage) bool {
	return strings.HasSuffix(abstractType, "_LIST") || strings.HasPrefix(strings.TrimSpace(string(raw)), "[")
}

// maskJSON replaces a non-empty value by the mask. A list becomes a list of
// one mask, so that the browser still shows a list editor. Empty values stay
// empty: they tell the user that nothing is set.
func maskJSON(raw json.RawMessage, abstractType string) json.RawMessage {
	t := strings.TrimSpace(string(raw))
	switch {
	case t == "" || t == "null" || t == `""` || t == "[]":
		return raw
	case isListType(abstractType, raw):
		return json.RawMessage(`["` + maskValue + `"]`)
	}
	return json.RawMessage(`"` + maskValue + `"`)
}

// holdsMask reports whether a value to save is the mask, or a list holding it.
func holdsMask(raw json.RawMessage) bool {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s == maskValue
	}
	var l []string
	if json.Unmarshal(raw, &l) == nil {
		for _, v := range l {
			if v == maskValue {
				return true
			}
		}
	}
	return false
}

// configDTO is one setting.
type configDTO struct {
	Interface    string          `json:"interface"`
	Storage      string          `json:"storage"`
	Key          string          `json:"key"`
	Type         string          `json:"type"`
	AbstractType string          `json:"abstractType"`
	Docs         string          `json:"docs"`
	Value        json.RawMessage `json:"value"`
	DefaultValue json.RawMessage `json:"defaultValue"`
	EnumLabel    string          `json:"enumLabel,omitempty"`
	EnumOptions  [][]string      `json:"enumOptions,omitempty"`
	Secret       bool            `json:"secret"`
}

func nullIfEmpty(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("null")
	}
	return raw
}

func newConfigDTO(e jd.ConfigEntry) configDTO {
	d := configDTO{
		Interface: e.InterfaceName, Storage: e.Storage, Key: e.Key, Type: e.Type, AbstractType: e.AbstractType, Docs: e.Docs,
		Value: nullIfEmpty(e.Value), DefaultValue: nullIfEmpty(e.DefaultValue), EnumLabel: e.EnumLabel, EnumOptions: e.EnumOptions,
	}
	if isSecret(e.Key, e.AbstractType) {
		d.Secret = true
		d.Value, d.DefaultValue = maskJSON(d.Value, e.AbstractType), maskJSON(d.DefaultValue, e.AbstractType)
	}
	return d
}

func configList(entries []jd.ConfigEntry) []configDTO {
	out := make([]configDTO, len(entries))
	for i, e := range entries {
		out[i] = newConfigDTO(e)
	}
	return out
}

const maxQuery = 256

// handleConfig is GET /config?q=&interface=&extensions=.
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	pattern, iface := q.Get("q"), q.Get("interface")
	if len(pattern) > maxQuery || len(iface) > maxQuery {
		s.fail(w, r, badRequest("q and interface must not be longer than %d characters.", maxQuery))
		return
	}
	if _, err := regexp.Compile(pattern); err != nil {
		s.fail(w, r, badRequest("q is not a valid regular expression."))
		return
	}
	c, err := s.client()
	var entries []jd.ConfigEntry
	if err == nil {
		entries, err = c.ConfigQuery(r.Context(), jd.ConfigQuery{
			Pattern: pattern, ConfigInterface: iface, Description: true, Values: true, DefaultValues: true, EnumInfo: true,
			IncludeExtensions: q.Get("extensions") == "true" || q.Get("extensions") == "1",
		})
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSONGzip(w, r, map[string]any{"entries": configList(entries)})
}

// settingRef names one setting.
type settingRef struct {
	Interface string `json:"interface"`
	Storage   string `json:"storage"`
	Key       string `json:"key"`
}

func (b settingRef) check() error {
	if b.Interface == "" || b.Key == "" || len(b.Interface) > 512 || len(b.Key) > 512 || len(b.Storage) > 512 {
		return badRequest("interface and key are required.")
	}
	return nil
}

// handleConfigReveal is POST /config/reveal: the real value of a secret setting.
// It reads nothing else, so it cannot be used to read the whole config unmasked.
func (s *Server) handleConfigReveal(w http.ResponseWriter, r *http.Request) {
	s.reveal(w, r, false)
}

// handlePluginReveal is POST /plugins/reveal: the same for a plugin setting.
// config/get cannot name a plugin storage, so the value comes from plugins/query.
func (s *Server) handlePluginReveal(w http.ResponseWriter, r *http.Request) {
	s.reveal(w, r, true)
}

func (s *Server) reveal(w http.ResponseWriter, r *http.Request, plugin bool) {
	var b settingRef
	err := decode(r, &b, false)
	if err == nil {
		err = b.check()
	}
	if err == nil && plugin && b.Storage == "" {
		err = badRequest("storage (the plugin name) is required.")
	}
	if err == nil && !secretKey.MatchString(b.Key) {
		err = badRequest("This setting is not a secret. Its value is not masked.")
	}
	var raw json.RawMessage
	if err == nil {
		var c *jd.Client
		if c, err = s.client(); err == nil {
			if plugin {
				raw, err = c.PluginValue(r.Context(), b.Interface, b.Storage, b.Key)
			} else {
				raw, err = c.ConfigGet(r.Context(), b.Interface, b.Storage, b.Key)
			}
		}
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if t := strings.TrimSpace(string(raw)); t == "true" || t == "false" || (t != "" && (t[0] == '-' || t[0] >= '0' && t[0] <= '9')) {
		s.fail(w, r, badRequest("This setting is a switch or a number and not masked."))
		return
	}
	s.log.Info("secret setting revealed", "interface", b.Interface, "plugin", plugin, "key", b.Key) // never the value
	s.writeJSON(w, r, http.StatusOK, map[string]any{"value": nullIfEmpty(raw)})
}

type settingValue struct {
	settingRef
	Value json.RawMessage `json:"value"`
}

func (b settingValue) check() error {
	if err := b.settingRef.check(); err != nil {
		return err
	}
	if len(b.Value) == 0 {
		return badRequest("value is required.")
	}
	if secretKey.MatchString(b.Key) && holdsMask(b.Value) {
		return badRequest("This is the mask of a secret, not a value. Enter the new value or leave the setting alone.")
	}
	return nil
}

// handleConfigSet is PUT /config.
func (s *Server) handleConfigSet(w http.ResponseWriter, r *http.Request) {
	var b settingValue
	err := decode(r, &b, false)
	if err == nil {
		err = b.check()
	}
	if err == nil {
		var c *jd.Client
		if c, err = s.client(); err == nil {
			err = c.ConfigSet(r.Context(), b.Interface, b.Storage, b.Key, b.Value)
		}
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	noContent(w)
}

func (s *Server) handleConfigReset(w http.ResponseWriter, r *http.Request) {
	var b settingRef
	err := decode(r, &b, false)
	if err == nil {
		err = b.check()
	}
	if err == nil {
		var c *jd.Client
		if c, err = s.client(); err == nil {
			err = c.ConfigReset(r.Context(), b.Interface, b.Storage, b.Key)
		}
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	noContent(w)
}

// pluginDTO is a hoster or decrypter plugin.
type pluginDTO struct {
	DisplayName string `json:"displayName"`
	ClassName   string `json:"className"`
	Pattern     string `json:"pattern"`
	Version     string `json:"version"`
}

// handlePlugins is GET /plugins?q=: the plugins whose name or class name holds q.
func (s *Server) handlePlugins(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	if len(q) > maxQuery {
		s.fail(w, r, badRequest("q must not be longer than %d characters.", maxQuery))
		return
	}
	c, err := s.client()
	var list []jd.Plugin
	if err == nil {
		list, err = c.Plugins(r.Context())
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := []pluginDTO{}
	for _, p := range list {
		if q == "" || strings.Contains(strings.ToLower(p.DisplayName), q) || strings.Contains(strings.ToLower(p.ClassName), q) {
			out = append(out, pluginDTO{p.DisplayName, p.ClassName, p.Pattern, p.Version})
		}
	}
	s.writeJSONGzip(w, r, map[string]any{"plugins": out})
}

// handlePluginSettings is GET /plugins/settings?q=&interface=: the settings of
// plugins, masked like the config.
func (s *Server) handlePluginSettings(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	pattern, iface := q.Get("q"), q.Get("interface")
	if len(pattern) > maxQuery || len(iface) > maxQuery {
		s.fail(w, r, badRequest("q and interface must not be longer than %d characters.", maxQuery))
		return
	}
	if _, err := regexp.Compile(pattern); err != nil {
		s.fail(w, r, badRequest("q is not a valid regular expression."))
		return
	}
	c, err := s.client()
	var entries []jd.ConfigEntry
	if err == nil {
		entries, err = c.PluginSettings(r.Context(), jd.ConfigQuery{
			Pattern: pattern, ConfigInterface: iface, Description: true, Values: true, DefaultValues: true, EnumInfo: true,
		})
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSONGzip(w, r, map[string]any{"entries": configList(entries)})
}

// handlePluginSet is PUT /plugins. Storage names the plugin (its display name).
func (s *Server) handlePluginSet(w http.ResponseWriter, r *http.Request) {
	var b settingValue
	err := decode(r, &b, false)
	if err == nil {
		err = b.check()
	}
	if err == nil && b.Storage == "" {
		err = badRequest("storage (the plugin name) is required.")
	}
	if err == nil {
		var c *jd.Client
		if c, err = s.client(); err == nil {
			err = c.PluginSet(r.Context(), b.Interface, b.Storage, b.Key, b.Value)
		}
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	noContent(w)
}

func (s *Server) handlePluginReset(w http.ResponseWriter, r *http.Request) {
	var b settingRef
	err := decode(r, &b, false)
	if err == nil {
		err = b.check()
	}
	if err == nil && b.Storage == "" {
		err = badRequest("storage (the plugin name) is required.")
	}
	if err == nil {
		var c *jd.Client
		if c, err = s.client(); err == nil {
			err = c.PluginReset(r.Context(), b.Interface, b.Storage, b.Key)
		}
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	noContent(w)
}

// extensionDTO is an extension of JDownloader.
type extensionDTO struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	Enabled         bool   `json:"enabled"`
	Installed       bool   `json:"installed"`
	IconKey         string `json:"iconKey"`
	ConfigInterface string `json:"configInterface"`
}

func (s *Server) handleExtensions(w http.ResponseWriter, r *http.Request) {
	c, err := s.client()
	var list []jd.Extension
	if err == nil {
		list, err = c.Extensions(r.Context())
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]extensionDTO, len(list))
	for i, e := range list {
		out[i] = extensionDTO(e)
	}
	s.writeJSON(w, r, http.StatusOK, out)
}

func (s *Server) handleExtensionAction(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("action")
	switch name {
	case "install", "enable", "disable":
	default:
		s.fail(w, r, notFound("Unknown action %q.", clip(url.PathEscape(name), 40)))
		return
	}
	var b struct {
		ID string `json:"id"`
	}
	err := decode(r, &b, false)
	if err == nil {
		_, err = needText("id", b.ID, 512)
	}
	ok := false
	if err == nil {
		var c *jd.Client
		if c, err = s.client(); err == nil {
			switch name {
			case "install":
				ok, err = c.InstallExtension(r.Context(), b.ID)
			default:
				ok, err = c.SetExtensionEnabled(r.Context(), b.ID, name == "enable")
			}
		}
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !ok {
		s.fail(w, r, &apiError{Status: http.StatusConflict, Code: codeConflict,
			Message: "JDownloader refused. The extension may be missing, not installed or not available."})
		return
	}
	noContent(w)
}
