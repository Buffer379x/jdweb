package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const (
	extractionCfg = "org.jdownloader.extensions.extraction.ExtractionConfig"
	captchaCfg    = "org.jdownloader.captcha.v2.solver.solver9kw.Captcha9kwSettings"
	reconnectCfg  = "jd.controlling.reconnect.ipcheck.IPCheckConfig"
)

type cfgEntry struct {
	Interface    string          `json:"interface"`
	Key          string          `json:"key"`
	AbstractType string          `json:"abstractType"`
	Value        json.RawMessage `json:"value"`
	DefaultValue json.RawMessage `json:"defaultValue"`
	Secret       bool            `json:"secret"`
}

func (e *env) configEntries(query string) map[string]cfgEntry {
	e.t.Helper()
	r := e.get("/api/v1/config?q=" + query)
	e.expect(r, 200)
	var out struct{ Entries []cfgEntry }
	r.json(e.t, &out)
	m := map[string]cfgEntry{}
	for _, en := range out.Entries {
		m[en.Key] = en
	}
	return m
}

// T-CFG-05: keys with password, apikey, secret or token are masked when the
// value is text or a list; numbers and switches are not.
func TestSecretSettingsAreMasked(t *testing.T) {
	e := newEnv(t, withDebugLog)
	const secretPW, secretKey, secretList = "hunter2-reconnect", "k3y-captcha-9kw", "archive-pass-one"
	e.jd.SetConfig(reconnectCfg, "RouterPassword", secretPW)
	e.jd.SetConfig(captchaCfg, "ApiKey", secretKey)
	e.jd.SetConfig(extractionCfg, "PasswordList", []string{secretList, "second"})
	e.jd.SetConfig(captchaCfg, "OAuthToken", "")                    // a secret that is not set
	e.jd.SetConfig(extractionCfg, "SecretWord", "case-INSENSITIVE") // matched case-insensitively
	e.jd.SetConfig(extractionCfg, "PasswordProtectionEnabled", true)
	e.jd.SetConfig(extractionCfg, "TokenCount", int64(7))
	e.jd.SetConfig(extractionCfg, "DisplayName", "visible text")

	r := e.get("/api/v1/config?q=.")
	e.expect(r, 200)
	for _, s := range []string{secretPW, secretKey, secretList, "case-INSENSITIVE"} {
		if strings.Contains(string(r.body), s) {
			t.Fatalf("the answer holds the secret %q: %s", s, r.body)
		}
	}
	var out struct{ Entries []cfgEntry }
	r.json(t, &out)
	m := map[string]cfgEntry{}
	for _, en := range out.Entries {
		m[en.Key] = en
	}
	const mask = "••••••"
	for key, want := range map[string]string{
		"RouterPassword": `"` + mask + `"`, "ApiKey": `"` + mask + `"`, "SecretWord": `"` + mask + `"`,
		"PasswordList": `["` + mask + `"]`,
	} {
		en := m[key]
		if !en.Secret || string(en.Value) != want {
			t.Errorf("%s: secret=%v value=%s, want secret and %s", key, en.Secret, en.Value, want)
		}
		if strings.Contains(string(en.DefaultValue), "hunter") {
			t.Errorf("%s: default value %s", key, en.DefaultValue)
		}
	}
	// An empty secret stays empty: nothing is set, and the browser can show that.
	if en := m["OAuthToken"]; !en.Secret || string(en.Value) != `""` {
		t.Errorf("empty secret: secret=%v value=%s", en.Secret, en.Value)
	}
	// Not text and not a list: shown as it is.
	for key, want := range map[string]string{"PasswordProtectionEnabled": "true", "TokenCount": "7", "DisplayName": `"visible text"`} {
		if en := m[key]; en.Secret || string(en.Value) != want {
			t.Errorf("%s: secret=%v value=%s, want plain %s", key, en.Secret, en.Value, want)
		}
	}

	// Reveal gives the real value, for secrets only.
	rev := e.post("/api/v1/config/reveal", map[string]any{"interface": reconnectCfg, "key": "RouterPassword"})
	e.expect(rev, 200)
	if !strings.Contains(string(rev.body), secretPW) {
		t.Fatalf("reveal = %s", rev.body)
	}
	rev = e.post("/api/v1/config/reveal", map[string]any{"interface": extractionCfg, "key": "PasswordList"})
	e.expect(rev, 200)
	if !strings.Contains(string(rev.body), secretList) || !strings.Contains(string(rev.body), `"second"`) {
		t.Fatalf("reveal list = %s", rev.body)
	}
	e.expectErr(e.post("/api/v1/config/reveal", map[string]any{"interface": extractionCfg, "key": "DisplayName"}), 400, "bad_request")
	e.expectErr(e.post("/api/v1/config/reveal", map[string]any{"interface": extractionCfg, "key": "PasswordProtectionEnabled"}), 400, "bad_request")
	e.expectErr(e.post("/api/v1/config/reveal", map[string]any{"interface": extractionCfg}), 400, "bad_request")

	// T-CFG-05 and T-SEC-11: never in the log, not at debug level either.
	for _, s := range []string{secretPW, secretKey, secretList} {
		if strings.Contains(e.logs.String(), s) {
			t.Errorf("the log holds the secret %q", s)
		}
	}
	if !strings.Contains(e.logs.String(), "secret setting revealed") {
		t.Error("a reveal leaves no trace in the log")
	}
}

func TestSavingTheMaskIsRefused(t *testing.T) {
	e := newEnv(t)
	const mask = "••••••"
	e.jd.SetConfig(reconnectCfg, "RouterPassword", "real")
	e.jd.SetConfig(extractionCfg, "PasswordList", []string{"real"})
	e.jd.ResetCalls()
	for name, body := range map[string]map[string]any{
		"string mask": {"interface": reconnectCfg, "key": "RouterPassword", "value": mask},
		"list mask":   {"interface": extractionCfg, "key": "PasswordList", "value": []string{mask}},
		"list with":   {"interface": extractionCfg, "key": "PasswordList", "value": []string{"new", mask}},
	} {
		if r := e.do("PUT", "/api/v1/config", body); r.status != 400 || !strings.Contains(r.errMessage(), "mask") {
			t.Errorf("%s: %d %s", name, r.status, r.body)
		}
	}
	if len(e.jdCalls("/config/set")) != 0 {
		t.Fatal("a mask was sent to JDownloader")
	}
	if e.jd.Config(reconnectCfg, "RouterPassword") != "real" {
		t.Fatal("the secret was overwritten")
	}
	// A real new value is saved.
	e.expect(e.do("PUT", "/api/v1/config", map[string]any{"interface": reconnectCfg, "key": "RouterPassword", "value": "newer"}), 204)
	if e.jd.Config(reconnectCfg, "RouterPassword") != "newer" {
		t.Fatal("the new secret was not saved")
	}
}

func TestConfigSetResetAndQuery(t *testing.T) {
	e := newEnv(t)
	const gs = "org.jdownloader.settings.GeneralSettings"
	e.expect(e.do("PUT", "/api/v1/config", map[string]any{"interface": gs, "key": "MaxSimultaneDownloads", "value": 5}), 204)
	if e.jd.Config(gs, "MaxSimultaneDownloads") != int64(5) {
		t.Fatalf("value = %v", e.jd.Config(gs, "MaxSimultaneDownloads"))
	}
	if got := argsJSON(e.lastCall("/config/set")); got != fmt.Sprintf(`["%s",null,"MaxSimultaneDownloads",5]`, gs) {
		t.Errorf("params = %s", got)
	}
	e.expect(e.post("/api/v1/config/reset", map[string]any{"interface": gs, "key": "MaxSimultaneDownloads"}), 204)
	if e.jd.Config(gs, "MaxSimultaneDownloads") != int64(3) {
		t.Fatalf("value after reset = %v", e.jd.Config(gs, "MaxSimultaneDownloads"))
	}
	// A refusal of JDownloader (wrong type, unknown key) is a 400 with a hint.
	r := e.do("PUT", "/api/v1/config", map[string]any{"interface": gs, "key": "MaxSimultaneDownloads", "value": "five"})
	if r.status != 400 || !strings.Contains(r.errMessage(), "wrong type") {
		t.Errorf("wrong type: %d %s", r.status, r.body)
	}
	e.expectErr(e.do("PUT", "/api/v1/config", map[string]any{"interface": gs, "key": "Nope", "value": 1}), 400, "bad_request")
	e.expectErr(e.do("PUT", "/api/v1/config", map[string]any{"interface": gs, "key": "X"}), 400, "bad_request")

	m := e.configEntries("MaxSimultaneDownloads")
	if en := m["MaxSimultaneDownloads"]; string(en.Value) != "3" || string(en.DefaultValue) != "3" || en.AbstractType != "INT" || en.Secret {
		t.Errorf("entry = %+v", en)
	}
	// The filter is a regular expression; a broken one is a 400, not JDownloader's problem.
	e.expectErr(e.get("/api/v1/config?q=%28%28"), 400, "bad_request")
	e.expectErr(e.get("/api/v1/config?q="+strings.Repeat("a", 300)), 400, "bad_request")
}

func TestPluginsAndExtensionsRoutes(t *testing.T) {
	e := newEnv(t)
	e.jd.SetConfig("jd.plugins.hoster.Foo", "ApiKey", "plugin-secret")
	r := e.get("/api/v1/plugins?q=foo")
	e.expect(r, 200)
	if !strings.Contains(string(r.body), `"plugins":[]`) {
		t.Fatalf("plugins = %s", r.body)
	}
	r = e.get("/api/v1/plugins/settings?q=ApiKey")
	e.expect(r, 200)
	if strings.Contains(string(r.body), "plugin-secret") {
		t.Fatalf("plugin settings leak a secret: %s", r.body)
	}
	e.expect(e.do("PUT", "/api/v1/plugins", map[string]any{"interface": "i", "storage": "Foo", "key": "k", "value": "v"}), 204)
	if got := argsJSON(e.lastCall("/plugins/set")); got != `["i","Foo","k","v"]` {
		t.Errorf("plugins/set params = %s", got)
	}
	e.expectErr(e.do("PUT", "/api/v1/plugins", map[string]any{"interface": "i", "key": "k", "value": "v"}), 400, "bad_request")
	e.expectErr(e.do("PUT", "/api/v1/plugins", map[string]any{"interface": "i", "storage": "Foo", "key": "ApiKey", "value": "••••••"}), 400, "bad_request")
	e.expect(e.post("/api/v1/plugins/reset", map[string]any{"interface": "i", "storage": "Foo", "key": "k"}), 204)

	e.expect(e.get("/api/v1/extensions"), 200)
	for _, a := range []string{"install", "enable", "disable"} {
		e.expect(e.post("/api/v1/extensions/"+a, map[string]any{"id": "ext1"}), 204)
	}
	if got := argsJSON(e.lastCall("/extensions/setEnabled")); got != `["ext1",false]` {
		t.Errorf("setEnabled params = %s", got)
	}
	// JDownloader refuses (false): not installed, not available.
	e.jd.FailNext("/extensions/install", 200, `{"data":false}`)
	r = e.post("/api/v1/extensions/install", map[string]any{"id": "ext1"})
	if r.status != 409 || r.errCode() != "conflict" {
		t.Errorf("refused install: %d %s", r.status, r.body)
	}
}

func TestAccountsRoutes(t *testing.T) {
	e := newEnv(t, withDebugLog)
	e.expect(e.get("/api/v1/accounts"), 200)
	e.expect(e.get("/api/v1/accounts/hosters"), 200)
	e.expect(e.post("/api/v1/accounts", map[string]any{"hoster": "example.com", "username": "me", "password": "pw-hoster-1"}), 204)
	if got := argsJSON(e.lastCall("/accountsV2/addAccount")); got != `["example.com","me","pw-hoster-1"]` {
		t.Errorf("addAccount params = %s", got)
	}
	e.expect(e.do("PUT", "/api/v1/accounts/55", map[string]any{"username": "me2", "password": "pw-hoster-2"}), 204)
	if got := argsJSON(e.lastCall("/accountsV2/setUserNameAndPassword")); got != `[55,"me2","pw-hoster-2"]` {
		t.Errorf("params = %s", got)
	}
	for _, a := range []string{"enable", "disable", "remove", "refresh"} {
		e.expect(e.post("/api/v1/accounts/actions/"+a, map[string]any{"ids": []string{"5", "6"}}), 204)
	}
	if got := argsJSON(e.lastCall("/accountsV2/refreshAccounts")); got != `[[5,6],true]` {
		t.Errorf("refresh params = %s", got)
	}
	for name, c := range map[string]struct {
		method, path string
		body         any
	}{
		"no hoster":      {"POST", "/api/v1/accounts", map[string]any{"username": "me", "password": "x"}},
		"no password":    {"POST", "/api/v1/accounts", map[string]any{"hoster": "h", "username": "me"}},
		"no username":    {"POST", "/api/v1/accounts", map[string]any{"hoster": "h", "password": "x"}},
		"bad id":         {"PUT", "/api/v1/accounts/x", map[string]any{"username": "me", "password": "x"}},
		"action no ids":  {"POST", "/api/v1/accounts/actions/remove", map[string]any{"ids": []string{}}},
		"numeric ids":    {"POST", "/api/v1/accounts/actions/remove", `{"ids":[5]}`},
		"basic bad type": {"POST", "/api/v1/basicauth", map[string]any{"type": "SSH", "hostMask": "*", "username": "u", "password": "p"}},
	} {
		e.jd.ResetCalls()
		if r := e.do(c.method, c.path, c.body); r.status != 400 || len(e.jd.Calls()) != 0 {
			t.Errorf("%s: %d %s calls=%d", name, r.status, r.body, len(e.jd.Calls()))
		}
	}
	r := e.post("/api/v1/basicauth", map[string]any{"type": "HTTP", "hostMask": "*.example.com", "username": "u", "password": "pw-basic"})
	e.expect(r, 200)
	if !strings.Contains(string(r.body), `"id":"`) {
		t.Errorf("basicauth add = %s", r.body)
	}
	e.expect(e.do("PUT", "/api/v1/basicauth/9", map[string]any{"type": "FTP", "hostMask": "ftp.*", "username": "u", "password": "pw-basic2", "enabled": false}), 204)
	e.expect(e.post("/api/v1/basicauth/remove", map[string]any{"ids": []string{"9"}}), 204)
	e.expect(e.get("/api/v1/basicauth"), 200)

	// Hoster passwords are never logged (T-SEC-11): not even at debug level.
	for _, s := range []string{"pw-hoster-1", "pw-hoster-2", "pw-basic"} {
		if strings.Contains(e.logs.String(), s) {
			t.Errorf("the log holds %q", s)
		}
	}
}

func TestExtractionRoutes(t *testing.T) {
	e := newEnv(t)
	e.expect(e.get("/api/v1/extraction/queue"), 200)
	e.expect(e.post("/api/v1/extraction/start", map[string]any{"linkIds": []string{"1"}, "packageIds": []string{}}), 200)
	if got := argsJSON(e.lastCall("/extraction/startExtractionNow")); got != `[[1],[]]` {
		t.Errorf("params = %s", got)
	}
	r := e.post("/api/v1/extraction/archives", map[string]any{"linkIds": []string{"1"}})
	e.expect(r, 200)
	if strings.TrimSpace(string(r.body)) != "[]" {
		t.Errorf("archives = %s", r.body)
	}
	e.expect(e.do("PUT", "/api/v1/extraction/archives/arch-1", map[string]any{"extractPath": "/output/x", "passwords": []string{"p"}, "autoExtract": true}), 204)
	if got := argsJSON(e.lastCall("/extraction/setArchiveSettings")); !strings.HasPrefix(got, `["arch-1",`) || !strings.Contains(got, `"autoExtract":true`) {
		t.Errorf("params = %s", got)
	}
	e.expect(e.post("/api/v1/extraction/passwords", map[string]any{"password": "pw-archive"}), 204)
	e.expect(e.post("/api/v1/extraction/cancel", map[string]any{"id": "77"}), 204)
	e.expectErr(e.post("/api/v1/extraction/start", map[string]any{}), 400, "bad_request")
	e.expectErr(e.post("/api/v1/extraction/passwords", map[string]any{"password": ""}), 400, "bad_request")
	e.expectErr(e.post("/api/v1/extraction/cancel", map[string]any{"id": 77}), 400, "bad_request")
	// A cancel of something that finished is understood, not an error page.
	e.jd.FailNext("/extraction/cancelExtraction", 200, `{"data":false}`)
	r = e.post("/api/v1/extraction/cancel", map[string]any{"id": "77"})
	if r.status != 404 || r.errCode() != "jd_error" || !strings.Contains(r.errMessage(), "gone") {
		t.Errorf("cancel of a finished extraction: %d %s", r.status, r.body)
	}
}
