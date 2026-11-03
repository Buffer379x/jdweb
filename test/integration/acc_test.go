//go:build integration

package integration

import (
	"strings"
	"testing"
	"time"
)

type accountDTO struct {
	ID          string `json:"id"`
	Hoster      string `json:"hoster"`
	Username    string `json:"username"`
	Enabled     bool   `json:"enabled"`
	Valid       bool   `json:"valid"`
	ErrorType   string `json:"errorType"`
	ErrorString string `json:"errorString"`
	TrafficLeft int64  `json:"trafficLeft"`
	ValidUntil  int64  `json:"validUntil"`
}

func (a *app) accounts(t testing.TB) (out []accountDTO) {
	t.Helper()
	a.must(t, "GET", "/api/v1/accounts", nil, &out, 200)
	return
}

func (a *app) accountOf(t testing.TB, user string) (accountDTO, bool) {
	for _, x := range a.accounts(t) {
		if x.Username == user {
			return x, true
		}
	}
	return accountDTO{}, false
}

// T-ACC-01 and T-ACC-02: add, edit, switch, refresh and remove a fake account;
// the password is in no log and no answer. A fake account is invalid, which
// has to be recognisable.
func TestACC01_Account(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	const secret = "it-account-secret-Pw-93142"
	const user1, user2 = "it-account@example.invalid", "it-account2@example.invalid"
	var hosters []string
	a.must(t, "GET", "/api/v1/accounts/hosters", nil, &hosters, 200)
	host := ""
	for _, h := range hosters {
		if h == "1fichier.com" {
			host = h
		}
	}
	if host == "" {
		t.Skip("1fichier.com is not among the premium hosters")
	}
	t.Cleanup(func() {
		for _, u := range []string{user1, user2} {
			if x, ok := a.accountOf(t, u); ok {
				a.call("POST", "/api/v1/accounts/actions/remove", map[string]any{"ids": []string{x.ID}})
			}
		}
	})
	a.must(t, "POST", "/api/v1/accounts", map[string]any{"hoster": host, "username": user1, "password": secret}, nil, 204)
	var acc accountDTO
	// A new account counts as valid until JDownloader has checked it.
	eventually(t, "the account is listed", 20*time.Second, func() bool { _, ok := a.accountOf(t, user1); return ok })
	t0 := time.Now()
	for time.Since(t0) < 90*time.Second {
		acc, _ = a.accountOf(t, user1)
		if acc.ErrorType != "" || !acc.Valid {
			break
		}
		time.Sleep(time.Second)
	}
	t.Logf("T-ACC-02 time until the check was through: %v", time.Since(t0).Round(time.Second))
	t.Logf("T-ACC-02 fake account: valid=%v errorType=%q errorString=%q trafficLeft=%d validUntil=%d", acc.Valid, acc.ErrorType, acc.ErrorString, acc.TrafficLeft, acc.ValidUntil)
	if acc.Valid {
		t.Error("a fake account is valid")
	}
	if acc.ErrorType == "" {
		t.Error("an invalid account has no error type: the status cannot be told")
	}
	// Edit: both fields are required.
	if st, _ := a.call("PUT", "/api/v1/accounts/"+acc.ID, map[string]any{"username": user2}); st != 400 {
		t.Errorf("edit without a password = %d", st)
	}
	a.must(t, "PUT", "/api/v1/accounts/"+acc.ID, map[string]any{"username": user2, "password": secret + "-2"}, nil, 204)
	eventually(t, "the new user name", 30*time.Second, func() bool { _, ok := a.accountOf(t, user2); return ok })
	acc, _ = a.accountOf(t, user2)
	// Switch.
	a.must(t, "POST", "/api/v1/accounts/actions/disable", map[string]any{"ids": []string{acc.ID}}, nil, 204)
	eventually(t, "disabled", 20*time.Second, func() bool { x, _ := a.accountOf(t, user2); return !x.Enabled })
	a.must(t, "POST", "/api/v1/accounts/actions/enable", map[string]any{"ids": []string{acc.ID}}, nil, 204)
	eventually(t, "enabled", 20*time.Second, func() bool { x, _ := a.accountOf(t, user2); return x.Enabled })
	a.must(t, "POST", "/api/v1/accounts/actions/refresh", map[string]any{"ids": []string{acc.ID}}, nil, 204)
	// The password in no answer.
	if b := a.must(t, "GET", "/api/v1/accounts", nil, nil, 200); strings.Contains(string(b), secret) {
		t.Error("the password is in the account list")
	}
	a.must(t, "POST", "/api/v1/accounts/actions/remove", map[string]any{"ids": []string{acc.ID}}, nil, 204)
	eventually(t, "removed", 20*time.Second, func() bool { _, ok := a.accountOf(t, user2); return !ok })
	// And never in the log of JDWeb.
	if strings.Contains(a.logs.String(), secret) {
		t.Error("T-ACC-01: the password is in the log")
	}
	// An id that does not exist.
	st, b := a.call("POST", "/api/v1/accounts/actions/remove", map[string]any{"ids": []string{"12345"}})
	t.Logf("T-ACC-01 remove of an unknown id: %d %s", st, clip(b))
	if st >= 500 {
		t.Errorf("unknown account id = %d", st)
	}
}

// Basic authentication entries: add, list, edit, switch, remove.
func TestACC_BasicAuth(t *testing.T) {
	cleanJD(t)
	a := shared(t)
	const secret = "it-basic-secret-77120"
	type entry struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		HostMask string `json:"hostMask"`
		Username string `json:"username"`
		Enabled  bool   `json:"enabled"`
	}
	list := func() (out []entry) {
		a.must(t, "GET", "/api/v1/basicauth", nil, &out, 200)
		return
	}
	var created struct {
		ID string `json:"id"`
	}
	a.must(t, "POST", "/api/v1/basicauth", map[string]any{"type": "HTTP", "hostMask": "it-basic.example.invalid", "username": "it-user", "password": secret}, &created, 200)
	t.Cleanup(func() { a.call("POST", "/api/v1/basicauth/remove", map[string]any{"ids": []string{created.ID}}) })
	var e entry
	for _, x := range list() {
		if x.ID == created.ID {
			e = x
		}
	}
	if e.ID == "" || e.HostMask != "it-basic.example.invalid" || e.Username != "it-user" || e.Type != "HTTP" {
		t.Fatalf("entry = %+v (list %+v)", e, list())
	}
	a.must(t, "PUT", "/api/v1/basicauth/"+created.ID, map[string]any{"type": "FTP", "hostMask": "it-basic.example.invalid", "username": "it-user2", "password": secret + "2", "enabled": false}, nil, 204)
	for _, x := range list() {
		if x.ID == created.ID {
			e = x
		}
	}
	if e.Type != "FTP" || e.Username != "it-user2" || e.Enabled {
		t.Errorf("after the edit: %+v", e)
	}
	if b := a.must(t, "GET", "/api/v1/basicauth", nil, nil, 200); strings.Contains(string(b), secret) {
		t.Error("the password is in the list")
	}
	a.must(t, "POST", "/api/v1/basicauth/remove", map[string]any{"ids": []string{created.ID}}, nil, 204)
	for _, x := range list() {
		if x.ID == created.ID {
			t.Error("the entry is still there")
		}
	}
	if strings.Contains(a.logs.String(), secret) {
		t.Error("the password is in the log")
	}
}
