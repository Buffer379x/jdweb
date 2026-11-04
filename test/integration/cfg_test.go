//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

const (
	generalSettings = "org.jdownloader.settings.GeneralSettings"
	extractionCfg   = "org.jdownloader.extensions.extraction.ExtractionConfig"
	extractionStore = "cfg/org.jdownloader.extensions.extraction.ExtractionExtension"
)

type cfgEntry struct {
	Interface    string          `json:"interface"`
	Storage      string          `json:"storage"`
	Key          string          `json:"key"`
	Type         string          `json:"type"`
	AbstractType string          `json:"abstractType"`
	Docs         string          `json:"docs"`
	Value        json.RawMessage `json:"value"`
	DefaultValue json.RawMessage `json:"defaultValue"`
	EnumLabel    string          `json:"enumLabel"`
	EnumOptions  [][]string      `json:"enumOptions"`
	Secret       bool            `json:"secret"`
}

func (a *app) config(t testing.TB, query string) []cfgEntry {
	t.Helper()
	var out struct {
		Entries []cfgEntry `json:"entries"`
	}
	a.must(t, "GET", "/api/v1/config?"+query, nil, &out, 200)
	return out.Entries
}

func (a *app) configEntry(t testing.TB, iface, storage, key string) cfgEntry {
	t.Helper()
	q := url.Values{"interface": {iface}, "q": {".*" + key + ".*"}}
	if storage != "" {
		q.Set("extensions", "true")
	}
	for _, e := range a.config(t, q.Encode()) {
		if e.Key == key && e.Storage == storage {
			return e
		}
	}
	t.Fatalf("no setting %s %s", iface, key)
	return cfgEntry{}
}

// T-CFG-01: a changed setting is in JDownloader at once; reset puts the default back.
func TestCFG01_General(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	c := direct()
	restoreConfig(t, c, generalSettings, "", "MaxSimultaneDownloads")
	a.must(t, "PUT", "/api/v1/config", map[string]any{"interface": generalSettings, "key": "MaxSimultaneDownloads", "value": 7}, nil, 204)
	got, _ := c.ConfigGet(bg(), generalSettings, "", "MaxSimultaneDownloads")
	if string(got) != "7" {
		t.Errorf("JDownloader holds %s after the change", got)
	}
	if e := a.configEntry(t, generalSettings, "", "MaxSimultaneDownloads"); string(e.Value) != "7" {
		t.Errorf("JDWeb shows %s", e.Value)
	}
	a.must(t, "POST", "/api/v1/config/reset", map[string]any{"interface": generalSettings, "key": "MaxSimultaneDownloads"}, nil, 204)
	e := a.configEntry(t, generalSettings, "", "MaxSimultaneDownloads")
	if string(e.Value) != string(e.DefaultValue) {
		t.Errorf("after the reset: value %s, default %s", e.Value, e.DefaultValue)
	}
	// A value of the wrong type is refused, by JDWeb or JDownloader, with a clear status.
	st, b := a.call("PUT", "/api/v1/config", map[string]any{"interface": generalSettings, "key": "MaxSimultaneDownloads", "value": "many"})
	t.Logf("T-CFG-01 text for a number: %d %s", st, clip(b))
	if st != 400 && st != 502 { // JDWeb or JDownloader refuses: 400, or 502 for JDownloader's own refusal
		t.Errorf("status %d", st)
	}
	if st, b := a.call("PUT", "/api/v1/config", map[string]any{"interface": generalSettings, "key": "NoSuchKeyAtAll", "value": 1}); st != 400 && st != 404 && st != 502 {
		t.Errorf("unknown key = %d %s", st, clip(b))
	}
}

// LinkgrabberAutoStartEnabled has a switch of its own in the UI (Task 9 decision 1).
func TestCFG_AutoStartKey(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	e := a.configEntry(t, linkgrabberSettings, "", "LinkgrabberAutoStartEnabled")
	t.Logf("autostart key: interface=%s abstractType=%s value=%s default=%s", e.Interface, e.AbstractType, e.Value, e.DefaultValue)
	if e.AbstractType != "BOOLEAN" || string(e.DefaultValue) != "true" {
		t.Errorf("entry = %+v", e)
	}
}

// T-CFG-02: speed limit in bytes per second, on and off, in JDownloader and in the status.
func TestCFG02_SpeedLimit(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	c := direct()
	for _, bps := range []int64{1, 1234, 51200, 10 << 20, 1<<31 - 1} {
		if st, b := a.call("POST", "/api/v1/speedlimit", map[string]any{"enabled": true, "bytesPerSecond": bps}); st != 204 {
			t.Errorf("limit %d = %d %s", bps, st, clip(b))
			continue
		}
		on, got, err := c.SpeedLimit(bg())
		if err != nil || !on || got != bps {
			t.Errorf("limit %d: JDownloader has on=%v %d (%v)", bps, on, got, err)
		}
		eventually(t, "status shows the limit", 10*time.Second, func() bool {
			s := a.status(t).Toggles
			return s.Limit && s.LimitBytes == bps
		})
	}
	// Switching off keeps the value.
	a.must(t, "POST", "/api/v1/speedlimit", map[string]any{"enabled": false, "bytesPerSecond": 0}, nil, 204)
	on, got, _ := c.SpeedLimit(bg())
	if on || got != 1<<31-1 {
		t.Errorf("after switching off: on=%v value=%d", on, got)
	}
	for _, bad := range []int64{-1, 1 << 31} {
		if st, _ := a.call("POST", "/api/v1/speedlimit", map[string]any{"enabled": true, "bytesPerSecond": bad}); st != 400 {
			t.Errorf("limit %d = %d", bad, st)
		}
	}
}

// T-CFG-03: an editor for every type: set a new value, read it back, reset it.
func TestCFG03_Types(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	c := direct()
	cases := []struct {
		key, typ string
		set      any
		want     string
	}{
		{"UseOriginalLastModified", "BOOLEAN", true, "true"},
		{"MaxDownloadLinkHistoryEntries", "INT", 12, "12"},
		{"DownloadTempUnavailableRetryWaittime", "LONG", 1234567, "1234567"},
		{"DefaultDownloadFolder", "STRING", "/output/it-cfg03", `"/output/it-cfg03"`},
		{"DelayWriteMode", "ENUM", "ON", `"ON"`},
		{"CrawlerHostPluginBlacklist", "STRING_LIST", []string{"it-a.example", "it-b.example"}, `["it-a.example","it-b.example"]`},
	}
	for _, k := range cases {
		e := a.configEntry(t, generalSettings, "", k.key)
		if e.AbstractType != k.typ {
			t.Errorf("%s: abstractType %s, want %s", k.key, e.AbstractType, k.typ)
			continue
		}
		if k.typ == "ENUM" {
			ok := false
			for _, o := range e.EnumOptions {
				ok = ok || o[0] == "ON"
			}
			if len(e.EnumOptions) < 2 || !ok {
				t.Errorf("%s: enum options %v", k.key, e.EnumOptions)
			}
		}
		orig := e.Value
		t.Cleanup(func() { _ = c.ConfigSet(bg(), generalSettings, "", k.key, json.RawMessage(orig)) })
		a.must(t, "PUT", "/api/v1/config", map[string]any{"interface": generalSettings, "key": k.key, "value": k.set}, nil, 204)
		got, _ := c.ConfigGet(bg(), generalSettings, "", k.key)
		if compact(got) != k.want {
			t.Errorf("%s (%s): JDownloader holds %s, want %s", k.key, k.typ, got, k.want)
		}
		a.must(t, "POST", "/api/v1/config/reset", map[string]any{"interface": generalSettings, "key": k.key}, nil, 204)
		e = a.configEntry(t, generalSettings, "", k.key)
		if compact(e.Value) != compact(e.DefaultValue) {
			t.Errorf("%s: after the reset value %s, default %s", k.key, e.Value, e.DefaultValue)
		}
	}
	// The types JDWeb meets in total, for the record.
	count := map[string]int{}
	for _, e := range a.config(t, "") {
		count[e.AbstractType]++
	}
	t.Logf("T-CFG-03 abstract types in the general config: %v", count)
}

// norm is compact, with an absent value read as null.
func norm(b json.RawMessage) string {
	if len(b) == 0 {
		return "null"
	}
	return compact(b)
}

// secretValueless reports whether the masked entry holds no value (the mask of an empty secret is empty or null).
func secretValueless(e *cfgEntry) bool {
	v := norm(e.Value)
	return v == "null" || v == `""` || v == "[]"
}

func compact(b json.RawMessage) string {
	var out bytes.Buffer
	if json.Compact(&out, b) != nil {
		return string(b)
	}
	return out.String()
}

// T-CFG-04 (server part): the whole list, and the filter by q. The 150 ms
// budget is the browser's; here the cost of the request is measured.
func TestCFG04_ListSize(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	t0 := time.Now()
	req, _ := http.NewRequest("GET", a.base+"/api/v1/config?extensions=true", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := a.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	buf := make([]byte, 1<<16)
	for {
		m, err := resp.Body.Read(buf)
		n += m
		if err != nil {
			break
		}
	}
	resp.Body.Close()
	all := a.config(t, "extensions=true")
	t.Logf("T-CFG-04 /config?extensions=true: %d entries, %d bytes on the wire (%s), %v", len(all), n, resp.Header.Get("Content-Encoding"), time.Since(t0).Round(10*time.Millisecond))
	t0 = time.Now()
	some := a.config(t, "q="+url.QueryEscape(".*MaxSimultan.*"))
	t.Logf("T-CFG-04 q filter: %d entries, %v", len(some), time.Since(t0).Round(time.Millisecond))
	// JDownloader matches q against the whole of interface and key: a word needs wildcards (docs/api.md).
	word := a.config(t, "q=MaxSimultan")
	t.Logf("T-CFG-04 q=MaxSimultan (no wildcards): %d entries", len(word))
	if len(some) == 0 {
		t.Error("the filter finds nothing")
	}
	if len(word) != 0 {
		t.Error("q is no longer anchored: update docs/api.md")
	}
}

// T-CFG-05: secrets are masked, revealed on request, and never logged.
func TestCFG05_Secrets(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	c := direct()
	restoreConfig(t, c, extractionCfg, extractionStore, "PasswordList")
	const pw = "it-cfg05-list-secret-48213"
	if err := c.ConfigSet(bg(), extractionCfg, extractionStore, "PasswordList", []string{pw, "second-" + pw}); err != nil {
		t.Fatal(err)
	}
	e := a.configEntry(t, extractionCfg, extractionStore, "PasswordList")
	if !e.Secret || strings.Contains(string(e.Value), pw) {
		t.Errorf("not masked: %+v", e)
	}
	var rev struct {
		Value json.RawMessage `json:"value"`
	}
	a.must(t, "POST", "/api/v1/config/reveal", map[string]any{"interface": extractionCfg, "storage": extractionStore, "key": "PasswordList"}, &rev, 200)
	if !strings.Contains(string(rev.Value), pw) {
		t.Errorf("reveal = %s", rev.Value)
	}
	// Not a secret key: no reveal.
	if st, _ := a.call("POST", "/api/v1/config/reveal", map[string]any{"interface": generalSettings, "key": "MaxSimultaneDownloads"}); st != 400 {
		t.Errorf("reveal of a plain setting = %d", st)
	}
	// Writing the mask back is refused and does not change anything.
	if st, _ := a.call("PUT", "/api/v1/config", map[string]any{"interface": extractionCfg, "storage": extractionStore, "key": "PasswordList", "value": []string{"••••••"}}); st != 400 {
		t.Errorf("writing the mask = %d", st)
	}
	got, _ := c.ConfigGet(bg(), extractionCfg, extractionStore, "PasswordList")
	if !strings.Contains(string(got), pw) {
		t.Errorf("JDownloader holds %s", got)
	}
	// A new list through JDWeb.
	a.must(t, "PUT", "/api/v1/config", map[string]any{"interface": extractionCfg, "storage": extractionStore, "key": "PasswordList", "value": []string{"it-new-" + pw}}, nil, 204)
	if logs := a.logs.String(); strings.Contains(logs, pw) {
		t.Error("a password is in the log")
	}
	// Every key that matches the pattern is masked in the whole list.
	n := 0
	for _, x := range a.config(t, "extensions=true") {
		if x.Secret {
			n++
			if x.AbstractType != "STRING" && x.AbstractType != "STRING_LIST" {
				t.Errorf("%s is secret with the type %s", x.Key, x.AbstractType)
			}
			if x.Key != "" && !strings.Contains(string(x.Value), "••••••") && string(x.Value) != "null" && string(x.Value) != `""` && string(x.Value) != "[]" && len(x.Value) > 0 {
				t.Errorf("%s is secret but shows %.40s", x.Key, x.Value)
			}
		}
	}
	t.Logf("T-CFG-05 secret entries: %d", n)
}

// Plugins: list, settings, secrets, reveal, set, reset.
func TestPlugins(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	var pl struct {
		Plugins []struct{ DisplayName, ClassName, Pattern, Version string } `json:"plugins"`
	}
	a.must(t, "GET", "/api/v1/plugins?q=1fichier", nil, &pl, 200)
	if len(pl.Plugins) == 0 {
		t.Fatal("no plugin matches 1fichier")
	}
	t.Logf("plugins: q=1fichier gives %d, e.g. %+v", len(pl.Plugins), pl.Plugins[0])
	// Plugin settings with a secret entry.
	var all struct {
		Entries []cfgEntry `json:"entries"`
	}
	t0 := time.Now()
	a.must(t, "GET", "/api/v1/plugins/settings", nil, &all, 200)
	t.Logf("plugin settings: %d entries in %v", len(all.Entries), time.Since(t0).Round(10*time.Millisecond))
	var secret, plain *cfgEntry
	for i := range all.Entries {
		e := &all.Entries[i]
		if e.Secret && (secret == nil || secretValueless(secret)) && !secretValueless(e) {
			secret = e // prefer a secret that holds a value, so that the reveal can be compared with something
		} else if secret == nil && e.Secret {
			secret = e
		}
		if plain == nil && e.AbstractType == "BOOLEAN" && !e.Secret && e.Storage != "" {
			plain = e
		}
	}
	if plain == nil {
		t.Fatal("no plain plugin setting")
	}
	// Set and reset a boolean.
	flip := string(plain.Value) != "true"
	a.must(t, "PUT", "/api/v1/plugins", map[string]any{"interface": plain.Interface, "storage": plain.Storage, "key": plain.Key, "value": flip}, nil, 204)
	v, err := direct().PluginValue(bg(), plain.Interface, plain.Storage, plain.Key)
	t.Logf("plugin setting %s/%s: set %v, JDownloader holds %s (%v)", plain.Storage, plain.Key, flip, v, err)
	if compact(v) != fmt.Sprint(flip) {
		t.Errorf("plugin setting holds %s, want %v", v, flip)
	}
	a.must(t, "POST", "/api/v1/plugins/reset", map[string]any{"interface": plain.Interface, "storage": plain.Storage, "key": plain.Key}, nil, 204)
	t.Cleanup(func() {
		var o any
		_ = json.Unmarshal(plain.Value, &o)
		_ = direct().PluginSet(bg(), plain.Interface, plain.Storage, plain.Key, o)
	})
	if secret == nil {
		t.Log("no plugin setting is secret on this JDownloader: reveal of plugin settings not tried with a secret")
	} else {
		var rev struct {
			Value json.RawMessage `json:"value"`
		}
		st, b := a.call("POST", "/api/v1/plugins/reveal", map[string]any{"interface": secret.Interface, "storage": secret.Storage, "key": secret.Key})
		t.Logf("plugin reveal %s/%s: %d %.80s", secret.Storage, secret.Key, st, b)
		if st != 200 {
			t.Errorf("reveal = %d", st)
		}
		_ = json.Unmarshal(b, &rev)
		if want, err := direct().PluginValue(bg(), secret.Interface, secret.Storage, secret.Key); err != nil {
			t.Errorf("cannot read the secret from JDownloader: %v", err)
		} else if norm(rev.Value) != norm(want) {
			t.Errorf("revealed value %s differs from JDownloader's %s", rev.Value, want)
		} else if norm(want) == "null" {
			t.Log("the plugin secrets of this JDownloader are all empty: the reveal is compared with an empty value only")
		}
	}
	if st, _ := a.call("POST", "/api/v1/plugins/reveal", map[string]any{"interface": plain.Interface, "storage": plain.Storage, "key": plain.Key}); st != 400 {
		t.Errorf("reveal of a plain plugin setting = %d", st)
	}
	if st, _ := a.call("POST", "/api/v1/plugins/reveal", map[string]any{"interface": plain.Interface, "key": plain.Key}); st != 400 {
		t.Errorf("reveal without storage = %d", st)
	}
}

// Extensions: list; disable and enable the extraction; installing needs the internet and is not tried.
func TestExtensions(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	type ext struct {
		ID, Name        string
		Enabled         bool
		Installed       bool
		ConfigInterface string
	}
	list := func() (out []ext) {
		a.must(t, "GET", "/api/v1/extensions", nil, &out, 200)
		return
	}
	find := func(id string) (ext, bool) {
		for _, e := range list() {
			if e.ID == id {
				return e, true
			}
		}
		return ext{}, false
	}
	const id = "org.jdownloader.extensions.extraction.ExtractionExtension"
	e, ok := find(id)
	if !ok || !e.Installed || !e.Enabled {
		t.Fatalf("extraction extension = %+v (%v)", e, ok)
	}
	t.Logf("extensions: %d, extraction: %+v", len(list()), e)
	t.Cleanup(func() { _, _ = direct().SetExtensionEnabled(bg(), id, true) })
	a.must(t, "POST", "/api/v1/extensions/disable", map[string]any{"id": id}, nil, 204)
	if e, _ := find(id); e.Enabled {
		t.Error("still enabled")
	}
	a.must(t, "POST", "/api/v1/extensions/enable", map[string]any{"id": id}, nil, 204)
	if e, _ := find(id); !e.Enabled {
		t.Error("not enabled again")
	}
	// An extension that is not installed cannot be enabled: a clear conflict, no 5xx.
	st, b := a.call("POST", "/api/v1/extensions/enable", map[string]any{"id": "folderwatch"})
	t.Logf("enable of a not installed extension: %d %s", st, clip(b))
	if st >= 500 && st != 502 {
		t.Errorf("status %d", st)
	}
}
