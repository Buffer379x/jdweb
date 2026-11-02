package api

import (
	"net/http"
	"time"

	"github.com/Buffer379x/jdweb/internal/auth"
)

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	required, err := s.auth.SetupRequired(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]bool{"required": required})
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Token    string `json:"token"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decode(r, &b, false); err != nil {
		s.fail(w, r, err)
		return
	}
	err := s.auth.Setup(r.Context(), auth.SetupRequest{Token: b.Token, Username: b.Username, Password: b.Password, IP: s.auth.ClientIP(r)})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.sessionsChanged() // a reset ends all sessions
	noContent(w)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Username string `json:"username"`
		Password string `json:"password"`
		TOTP     string `json:"totp"`
	}
	if err := decode(r, &b, false); err != nil {
		s.fail(w, r, err)
		return
	}
	res, err := s.auth.Login(r.Context(), auth.LoginRequest{
		Username: b.Username, Password: b.Password, TOTP: b.TOTP, IP: s.auth.ClientIP(r), UserAgent: r.UserAgent(),
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.auth.SetSessionCookie(w, r, res.Token, res.ExpiresAt)
	noContent(w)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cur, _ := auth.SessionFromContext(r.Context())
	if err := s.auth.Logout(r.Context(), cur.TokenHash); err != nil {
		s.fail(w, r, err)
		return
	}
	s.auth.ClearSessionCookie(w, r)
	s.sessionsChanged()
	noContent(w)
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	acc, err := s.auth.Account(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"username": acc.Username, "totpEnabled": acc.TOTPEnabled, "version": s.version})
}

type sessionDTO struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
	UserAgent  string    `json:"userAgent"`
	Current    bool      `json:"current"`
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	cur, _ := auth.SessionFromContext(r.Context())
	list, err := s.auth.ListSessions(r.Context(), cur.TokenHash)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]sessionDTO, len(list))
	for i, si := range list {
		out[i] = sessionDTO{si.ID, si.CreatedAt, si.LastSeenAt, si.ExpiresAt, si.UserAgent, si.Current}
	}
	s.writeJSON(w, r, http.StatusOK, out)
}

func (s *Server) handleSignOutOthers(w http.ResponseWriter, r *http.Request) {
	cur, _ := auth.SessionFromContext(r.Context())
	if _, err := s.auth.SignOutOthers(r.Context(), cur.TokenHash); err != nil {
		s.fail(w, r, err)
		return
	}
	s.sessionsChanged()
	noContent(w)
}

func (s *Server) handlePassword(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := decode(r, &b, false); err != nil {
		s.fail(w, r, err)
		return
	}
	cur, _ := auth.SessionFromContext(r.Context())
	if err := s.auth.ChangePassword(r.Context(), b.Current, b.New, cur.TokenHash); err != nil {
		s.fail(w, r, err)
		return
	}
	s.sessionsChanged()
	noContent(w)
}

func (s *Server) handleTOTPEnroll(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Password string `json:"password"`
	}
	if err := decode(r, &b, false); err != nil {
		s.fail(w, r, err)
		return
	}
	en, err := s.auth.EnrollTOTP(r.Context(), b.Password)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]string{"secret": en.Secret, "uri": en.URI, "qrSvg": en.QRSVG})
}

func (s *Server) handleTOTPConfirm(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Code string `json:"code"`
	}
	if err := decode(r, &b, false); err != nil {
		s.fail(w, r, err)
		return
	}
	codes, err := s.auth.ConfirmTOTP(r.Context(), b.Code)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"recoveryCodes": codes})
}

func (s *Server) handleTOTPDisable(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Password string `json:"password"`
	}
	if err := decode(r, &b, false); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.auth.DisableTOTP(r.Context(), b.Password); err != nil {
		s.fail(w, r, err)
		return
	}
	noContent(w)
}
