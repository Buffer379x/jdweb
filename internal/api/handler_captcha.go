package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Buffer379x/jdweb/internal/jd"
)

// captchaDTO is a waiting captcha. Kind says how the browser shows it.
type captchaDTO struct {
	ID           string `json:"id"`
	Hoster       string `json:"hoster"`
	Type         string `json:"type"`
	Kind         string `json:"kind"` // "image" or "interactive"
	ImageDataURL string `json:"imageDataUrl,omitempty"`
	Explain      string `json:"explain,omitempty"`
	Timeout      int64  `json:"timeout"`
	Created      int64  `json:"created"`
	Remaining    int    `json:"remaining"`
}

// interactiveHints are parts of the type names of challenges the browser
// cannot show as a picture. Anything else is taken for an image captcha. The
// names are a guess from the JDownloader sources, not yet seen on a real job
// (Concept R7).
var interactiveHints = []string{"recaptcha", "hcaptcha", "turnstile", "click", "interactive", "geetest", "funcaptcha", "cutcaptcha", "friendly"}

func captchaKind(j jd.CaptchaJob) string {
	s := strings.ToLower(j.Type + " " + j.Category + " " + j.ChallengeType)
	for _, h := range interactiveHints {
		if strings.Contains(s, h) {
			return "interactive"
		}
	}
	return "image"
}

func newCaptchaDTO(j jd.CaptchaJob) captchaDTO {
	return captchaDTO{
		ID: idString(j.ID), Hoster: j.Hoster, Type: j.Type, Kind: captchaKind(j), Explain: j.Explain,
		Timeout: j.Timeout, Created: j.Created, Remaining: j.Remaining,
	}
}

func (s *Server) handleCaptchas(w http.ResponseWriter, r *http.Request) {
	c, err := s.client()
	var jobs []jd.CaptchaJob
	if err == nil {
		jobs, err = c.Captchas(r.Context())
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]captchaDTO, len(jobs))
	for i, j := range jobs {
		out[i] = newCaptchaDTO(j)
	}
	s.writeJSON(w, r, http.StatusOK, out)
}

// maxCaptchaImage bounds the data URL that goes to the browser.
const maxCaptchaImage = 2 << 20

func (s *Server) handleCaptcha(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	c, err := s.client()
	var job *jd.CaptchaJob
	if err == nil {
		job, err = c.Captcha(r.Context(), id)
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if job == nil {
		s.fail(w, r, &apiError{Status: http.StatusNotFound, Code: codeJDError,
			Message: "This captcha is gone. It was solved, skipped or timed out in JDownloader."})
		return
	}
	dto := newCaptchaDTO(*job)
	if dto.Kind == "image" {
		url, err := c.CaptchaImage(r.Context(), id)
		switch {
		case err == nil && strings.HasPrefix(url, "data:image/") && len(url) <= maxCaptchaImage:
			dto.ImageDataURL = url
		case err != nil && (jd.IsNotFound(err) || jd.IsConnection(err) || r.Context().Err() != nil):
			s.fail(w, r, err)
			return
		default:
			// No usable picture: the browser can only explain and skip it.
			s.log.Warn("captcha without a usable image", "kind", dto.Type, "err", err)
			dto.Kind = "interactive"
		}
	}
	s.writeJSON(w, r, http.StatusOK, dto)
}

func (s *Server) handleCaptchaSolve(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	var b struct {
		Result string `json:"result"`
	}
	if err == nil {
		err = decode(r, &b, false)
	}
	if err == nil && (b.Result == "" || len(b.Result) > 4096) {
		err = badRequest("result is required.")
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	c, err := s.client()
	if err == nil {
		err = c.CaptchaSolve(r.Context(), id, b.Result)
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	noContent(w)
}

var skipRequests = []jd.SkipRequest{jd.SkipSingle, jd.SkipBlockHoster, jd.SkipBlockAllCaptchas, jd.SkipBlockPackage,
	jd.SkipRefresh, jd.SkipStopCurrentAction, jd.SkipTimeout}

func (s *Server) handleCaptchaSkip(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	var b struct {
		Type string `json:"type"`
	}
	if err == nil {
		err = decode(r, &b, false)
	}
	if err == nil && !containsSkip(b.Type) {
		err = badRequest("type must be one of %s.", joinEnum(skipRequests))
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	c, err := s.client()
	if err == nil {
		err = c.CaptchaSkip(r.Context(), id, jd.SkipRequest(b.Type))
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	noContent(w)
}

func containsSkip(t string) bool {
	for _, v := range skipRequests {
		if string(v) == t {
			return true
		}
	}
	return false
}

// dialogDTO is a question of JDownloader. Properties are text for the browser
// to show, never markup.
type dialogDTO struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	Properties map[string]string `json:"properties"`
	// Answer lists the fields an answer may hold with the Java type of each.
	Answer map[string]string `json:"answer,omitempty"`
}

func (s *Server) handleDialogs(w http.ResponseWriter, r *http.Request) {
	c, err := s.client()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ids, err := c.DialogIDs(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := []dialogDTO{}
	for _, id := range ids {
		d, err := c.Dialog(r.Context(), id)
		if err != nil {
			if jd.IsNotFound(err) || strings.HasPrefix(errType(err), "Invalid ID") {
				continue // answered between the two calls
			}
			s.fail(w, r, err)
			return
		}
		out = append(out, newDialogDTO(id, d))
	}
	s.writeJSON(w, r, http.StatusOK, out)
}

func errType(err error) string {
	var e *jd.Error
	if errors.As(err, &e) {
		return e.Type
	}
	return ""
}

func newDialogDTO(id int64, d jd.Dialog) dialogDTO {
	props := d.Properties
	if props == nil {
		props = map[string]string{}
	}
	return dialogDTO{ID: idString(id), Type: d.Type, Properties: props}
}

func (s *Server) handleDialog(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	c, err := s.client()
	var d jd.Dialog
	if err == nil {
		d, err = c.Dialog(r.Context(), id)
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	dto := newDialogDTO(id, d)
	if info, err := c.DialogType(r.Context(), d.Type); err == nil {
		dto.Answer = info.In
	}
	s.writeJSON(w, r, http.StatusOK, dto)
}

// maxDialogAnswer bounds an answer: a handful of short fields.
const maxDialogAnswer = 32

func (s *Server) handleDialogAnswer(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	var b struct {
		Data map[string]any `json:"data"`
	}
	if err == nil {
		err = decode(r, &b, false)
	}
	if err == nil {
		err = checkAnswer(b.Data)
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	c, err := s.client()
	if err == nil {
		err = c.DialogAnswer(r.Context(), id, b.Data)
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	noContent(w)
}

// checkAnswer allows flat answers of text, numbers and booleans.
func checkAnswer(data map[string]any) error {
	if data == nil {
		return badRequest("data is required.")
	}
	if len(data) > maxDialogAnswer {
		return badRequest("data holds too many fields.")
	}
	for k, v := range data {
		if len(k) > 64 {
			return badRequest("A field name in data is too long.")
		}
		switch x := v.(type) {
		case string:
			if len(x) > 4096 {
				return badRequest("A value in data is too long.")
			}
		case bool, float64, nil:
		default:
			return badRequest("Values in data must be text, numbers or booleans.")
		}
	}
	return nil
}
