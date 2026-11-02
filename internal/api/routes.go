package api

import (
	"net/http"

	"github.com/Buffer379x/jdweb/internal/state"
)

// buildRoutes is the table of all API routes. Every route is registered here
// and nowhere else, so that a test can walk it (T-SEC-01).
func (s *Server) buildRoutes() []Route {
	const v1 = "/api/v1"
	var rs []Route
	add := func(method, p string, h http.HandlerFunc) *Route {
		rs = append(rs, Route{Method: method, Path: v1 + p, handler: h})
		return &rs[len(rs)-1]
	}
	// public routes
	rs = append(rs, Route{Method: "GET", Path: "/healthz", Public: true, handler: s.handleHealthz})
	add("GET", "/setup/status", s.handleSetupStatus).Public = true
	add("POST", "/setup", s.handleSetup).Public = true
	add("POST", "/login", s.handleLogin).Public = true

	// account
	add("POST", "/logout", s.handleLogout)
	add("GET", "/session", s.handleSession)
	add("GET", "/sessions", s.handleSessions)
	add("DELETE", "/sessions", s.handleSignOutOthers)
	add("POST", "/password", s.handlePassword)
	add("POST", "/totp/enroll", s.handleTOTPEnroll)
	add("POST", "/totp/confirm", s.handleTOTPConfirm)
	add("POST", "/totp/disable", s.handleTOTPDisable)

	// state
	add("GET", "/status", s.handleStatus)
	add("GET", "/downloads", s.handleDownloads)
	add("GET", "/linkgrabber", s.handleLinkgrabber)
	add("GET", "/events", s.handleEvents)

	// control
	for _, a := range []string{"start", "stop", "pause", "resume"} {
		add("POST", "/controller/"+a, s.handleController(a))
	}
	add("POST", "/speedlimit", s.handleSpeedLimit)
	for _, t := range []string{"premium", "clipboard", "reconnect", "stop-after"} {
		add("POST", "/toggles/"+t, s.handleToggle(t))
	}
	add("POST", "/reconnect", s.handleReconnect)

	// lists
	add("POST", "/downloads/actions/{action}", s.handleListAction(state.Downloads))
	add("POST", "/linkgrabber/actions/{action}", s.handleListAction(state.Linkgrabber))
	add("GET", "/downloads/stopmark", s.handleStopMark)
	add("POST", "/linkgrabber/add", s.handleAddLinks).limit = bigBodyLimit
	add("POST", "/linkgrabber/container", s.handleAddContainer).limit = bigBodyLimit
	add("GET", "/linkgrabber/variants/{linkId}", s.handleVariants)
	add("GET", "/linkgrabber/folders", s.handleFolders)

	// captchas and dialogs
	add("GET", "/captchas", s.handleCaptchas)
	add("GET", "/captchas/{id}", s.handleCaptcha)
	add("POST", "/captchas/{id}/solve", s.handleCaptchaSolve)
	add("POST", "/captchas/{id}/skip", s.handleCaptchaSkip)
	add("GET", "/dialogs", s.handleDialogs)
	add("GET", "/dialogs/{id}", s.handleDialog)
	add("POST", "/dialogs/{id}/answer", s.handleDialogAnswer)

	// extraction
	add("GET", "/extraction/queue", s.handleExtractionQueue)
	add("POST", "/extraction/start", s.handleExtractionStart)
	add("POST", "/extraction/cancel", s.handleExtractionCancel)
	add("POST", "/extraction/archives", s.handleExtractionArchives)
	add("PUT", "/extraction/archives/{id}", s.handleExtractionSettings)
	add("POST", "/extraction/passwords", s.handleExtractionPassword)

	// accounts
	add("GET", "/accounts", s.handleAccounts)
	add("POST", "/accounts", s.handleAccountAdd)
	add("PUT", "/accounts/{id}", s.handleAccountUpdate)
	add("POST", "/accounts/actions/{action}", s.handleAccountAction)
	add("GET", "/accounts/hosters", s.handleHosters)
	add("GET", "/basicauth", s.handleBasicAuths)
	add("POST", "/basicauth", s.handleBasicAuthAdd)
	add("PUT", "/basicauth/{id}", s.handleBasicAuthUpdate)
	add("POST", "/basicauth/remove", s.handleBasicAuthRemove)

	// settings
	add("GET", "/config", s.handleConfig)
	add("POST", "/config/reveal", s.handleConfigReveal)
	add("PUT", "/config", s.handleConfigSet)
	add("POST", "/config/reset", s.handleConfigReset)
	add("GET", "/plugins", s.handlePlugins)
	add("GET", "/plugins/settings", s.handlePluginSettings)
	add("PUT", "/plugins", s.handlePluginSet)
	add("POST", "/plugins/reset", s.handlePluginReset)
	add("GET", "/extensions", s.handleExtensions)
	add("POST", "/extensions/{action}", s.handleExtensionAction)

	// system
	add("GET", "/system", s.handleSystem)
	for _, a := range []string{"restart", "exit", "shutdown", "hibernate", "standby"} {
		add("POST", "/system/"+a, s.handleSystemAction(a))
	}
	add("GET", "/update", s.handleUpdate)
	add("POST", "/update/check", s.handleUpdateCheck)
	add("POST", "/update/apply", s.handleUpdateApply)
	add("GET", "/diagnostics", s.handleDiagnostics)
	add("POST", "/diagnostics/acknowledge", s.handleAcknowledge)
	add("GET", "/connection", s.handleConnection)
	add("PUT", "/connection", s.handleConnectionSet)

	// icons and preferences
	add("GET", "/icons/hoster/{name}", s.handleIcon("h", hosterIcon))
	add("GET", "/icons/file/{name}", s.handleIcon("f", fileIcon))
	add("GET", "/prefs", s.handlePrefs)
	add("PUT", "/prefs", s.handlePrefsSet).limit = prefsLimit
	return rs
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte("ok\n"))
}
