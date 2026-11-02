package api

import (
	"net/http"
	"strings"
	"unicode"

	"github.com/Buffer379x/jdweb/internal/jd"
)

// accountDTO is a premium account. The password is never known here.
type accountDTO struct {
	ID          string `json:"id"`
	Hoster      string `json:"hoster"`
	Username    string `json:"username"`
	Enabled     bool   `json:"enabled"`
	Valid       bool   `json:"valid"`
	ErrorType   string `json:"errorType"`
	ErrorString string `json:"errorString"`
	TrafficLeft int64  `json:"trafficLeft"` // -1 when unknown
	TrafficMax  int64  `json:"trafficMax"`  // -1 when unknown
	ValidUntil  int64  `json:"validUntil"`  // Unix milliseconds, -1 when unknown
}

func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	c, err := s.client()
	var list []jd.Account
	if err == nil {
		list, err = c.Accounts(r.Context())
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]accountDTO, len(list))
	for i, a := range list {
		out[i] = accountDTO{idString(a.UUID), a.Hostname, a.Username, a.Enabled, a.Valid, a.ErrorType, a.ErrorString,
			a.TrafficLeft, a.TrafficMax, a.ValidUntil}
	}
	s.writeJSON(w, r, http.StatusOK, out)
}

// checkLogin validates the user name and password of an account or an entry.
func checkLogin(username, password string, passwordRequired bool) error {
	if username = strings.TrimSpace(username); username == "" || len(username) > 512 || strings.ContainsFunc(username, unicode.IsControl) {
		return badRequest("username is required and must not be longer than 512 characters.")
	}
	if (passwordRequired && password == "") || len(password) > 4096 {
		return badRequest("password is required and must not be longer than 4096 characters.")
	}
	return nil
}

func (s *Server) handleAccountAdd(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Hoster   string `json:"hoster"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	err := decode(r, &b, false)
	if err == nil {
		_, err = needText("hoster", b.Hoster, 256)
	}
	if err == nil {
		err = checkLogin(b.Username, b.Password, true)
	}
	if err == nil {
		var c *jd.Client
		if c, err = s.client(); err == nil {
			err = c.AddAccount(r.Context(), strings.TrimSpace(b.Hoster), strings.TrimSpace(b.Username), b.Password)
		}
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	noContent(w)
}

func (s *Server) handleAccountUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	var b struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err == nil {
		err = decode(r, &b, false)
	}
	if err == nil {
		err = checkLogin(b.Username, b.Password, true)
	}
	if err == nil {
		var c *jd.Client
		if c, err = s.client(); err == nil {
			err = c.SetAccountLogin(r.Context(), id, strings.TrimSpace(b.Username), b.Password)
		}
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	noContent(w)
}

type idsBody struct {
	IDs []ID `json:"ids"`
}

func (b idsBody) check() error {
	if len(b.IDs) == 0 || len(b.IDs) > 10_000 {
		return badRequest("ids must hold between 1 and 10000 ids.")
	}
	return nil
}

func (s *Server) handleAccountAction(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("action")
	switch name {
	case "enable", "disable", "remove", "refresh":
	default:
		s.fail(w, r, notFound("Unknown action %q.", clip(name, 40)))
		return
	}
	var b idsBody
	err := decode(r, &b, false)
	if err == nil {
		err = b.check()
	}
	if err == nil {
		var c *jd.Client
		if c, err = s.client(); err == nil {
			ids := ints(b.IDs)
			switch name {
			case "enable":
				err = c.EnableAccounts(r.Context(), ids)
			case "disable":
				err = c.DisableAccounts(r.Context(), ids)
			case "remove":
				err = c.RemoveAccounts(r.Context(), ids)
			case "refresh":
				err = c.RefreshAccounts(r.Context(), ids, true)
			}
		}
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	noContent(w)
}

func (s *Server) handleHosters(w http.ResponseWriter, r *http.Request) {
	c, err := s.client()
	var hosters []string
	if err == nil {
		hosters, err = c.PremiumHosters(r.Context())
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if hosters == nil {
		hosters = []string{}
	}
	s.writeJSON(w, r, http.StatusOK, hosters)
}

// basicAuthDTO is a stored login for a host mask. The password is never returned.
type basicAuthDTO struct {
	ID            string `json:"id"`
	Type          string `json:"type"`
	HostMask      string `json:"hostMask"`
	Username      string `json:"username"`
	Enabled       bool   `json:"enabled"`
	Created       int64  `json:"created"`
	LastValidated int64  `json:"lastValidated"`
}

func (s *Server) handleBasicAuths(w http.ResponseWriter, r *http.Request) {
	c, err := s.client()
	var list []jd.BasicAuth
	if err == nil {
		list, err = c.BasicAuths(r.Context())
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]basicAuthDTO, len(list))
	for i, a := range list {
		out[i] = basicAuthDTO{idString(a.ID), string(a.Type), a.HostMask, a.Username, a.Enabled, a.Created, a.LastValidated}
	}
	s.writeJSON(w, r, http.StatusOK, out)
}

type basicAuthBody struct {
	Type     string `json:"type"`
	HostMask string `json:"hostMask"`
	Username string `json:"username"`
	Password string `json:"password"`
	Enabled  *bool  `json:"enabled"`
}

func (b basicAuthBody) check(passwordRequired bool) error {
	if b.Type != string(jd.BasicAuthHTTP) && b.Type != string(jd.BasicAuthFTP) {
		return badRequest("type must be HTTP or FTP.")
	}
	if _, err := needText("hostMask", b.HostMask, 512); err != nil {
		return err
	}
	return checkLogin(b.Username, b.Password, passwordRequired)
}

func (s *Server) handleBasicAuthAdd(w http.ResponseWriter, r *http.Request) {
	var b basicAuthBody
	err := decode(r, &b, false)
	if err == nil {
		err = b.check(true)
	}
	var id int64
	if err == nil {
		var c *jd.Client
		if c, err = s.client(); err == nil {
			id, err = c.AddBasicAuth(r.Context(), jd.BasicAuthType(b.Type), strings.TrimSpace(b.HostMask), strings.TrimSpace(b.Username), b.Password)
		}
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]string{"id": idString(id)})
}

func (s *Server) handleBasicAuthUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	var b basicAuthBody
	if err == nil {
		err = decode(r, &b, false)
	}
	if err == nil {
		err = b.check(true) // the stored password is unknown here, so an edit sends it again
	}
	if err == nil {
		var c *jd.Client
		if c, err = s.client(); err == nil {
			err = c.UpdateBasicAuth(r.Context(), jd.BasicAuth{
				ID: id, Type: jd.BasicAuthType(b.Type), HostMask: strings.TrimSpace(b.HostMask),
				Username: strings.TrimSpace(b.Username), Password: b.Password, Enabled: b.Enabled == nil || *b.Enabled,
			})
		}
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	noContent(w)
}

func (s *Server) handleBasicAuthRemove(w http.ResponseWriter, r *http.Request) {
	var b idsBody
	err := decode(r, &b, false)
	if err == nil {
		err = b.check()
	}
	if err == nil {
		var c *jd.Client
		if c, err = s.client(); err == nil {
			err = c.RemoveBasicAuths(r.Context(), ints(b.IDs))
		}
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	noContent(w)
}
