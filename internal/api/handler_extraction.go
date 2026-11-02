package api

import (
	"net/http"
	"slices"

	"github.com/Buffer379x/jdweb/internal/jd"
)

func (s *Server) handleExtractionQueue(w http.ResponseWriter, r *http.Request) {
	c, err := s.client()
	var q []jd.ArchiveStatus
	if err == nil {
		q, err = c.ExtractionQueue(r.Context())
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if q == nil {
		q = []jd.ArchiveStatus{}
	}
	s.writeJSON(w, r, http.StatusOK, q)
}

type selectionBody struct {
	LinkIDs    []ID `json:"linkIds"`
	PackageIDs []ID `json:"packageIds"`
}

func (b selectionBody) check() error {
	if len(b.LinkIDs)+len(b.PackageIDs) == 0 {
		return badRequest("Select at least one link or package (linkIds, packageIds).")
	}
	if len(b.LinkIDs) > maxIDs || len(b.PackageIDs) > maxIDs {
		return badRequest("Too many ids in one request (at most %d).", maxIDs)
	}
	return nil
}

// handleExtractionStart is POST /extraction/start. The answer maps the id of
// each started extraction to whether it started.
func (s *Server) handleExtractionStart(w http.ResponseWriter, r *http.Request) {
	var b selectionBody
	err := decode(r, &b, false)
	if err == nil {
		err = b.check()
	}
	var res map[string]bool
	if err == nil {
		var c *jd.Client
		if c, err = s.client(); err == nil {
			res, err = c.ExtractionStart(r.Context(), ints(b.LinkIDs), ints(b.PackageIDs))
		}
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if res == nil {
		res = map[string]bool{}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"started": res})
}

func (s *Server) handleExtractionCancel(w http.ResponseWriter, r *http.Request) {
	var b struct {
		ID ID `json:"id"`
	}
	err := decode(r, &b, false)
	if err == nil && b.ID == 0 {
		err = badRequest("id is required.")
	}
	var ok bool
	if err == nil {
		var c *jd.Client
		if c, err = s.client(); err == nil {
			ok, err = c.ExtractionCancel(r.Context(), int64(b.ID))
		}
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !ok {
		s.fail(w, r, &apiError{Status: http.StatusNotFound, Code: codeJDError, Message: "This extraction is gone. It finished or was cancelled already."})
		return
	}
	noContent(w)
}

// archiveSettingsDTO are the settings of an archive. finalPassword and
// passwords are secrets (Concept 9.4): they show as the mask, secret says so,
// and POST /extraction/archives/{id}/reveal gives the real values.
type archiveSettingsDTO struct {
	ArchiveID                          string   `json:"archiveId"`
	ExtractPath                        string   `json:"extractPath"`
	FinalPassword                      string   `json:"finalPassword"`
	Passwords                          []string `json:"passwords"`
	AutoExtract                        *bool    `json:"autoExtract"`
	RemoveDownloadLinksAfterExtraction *bool    `json:"removeDownloadLinksAfterExtraction"`
	RemoveFilesAfterExtraction         *bool    `json:"removeFilesAfterExtraction"`
	Secret                             bool     `json:"secret"`
}

func newArchiveSettingsDTO(a jd.ArchiveSettings) *archiveSettingsDTO {
	d := &archiveSettingsDTO{
		ArchiveID: a.ArchiveID, ExtractPath: a.ExtractPath, Passwords: []string{}, AutoExtract: a.AutoExtract,
		RemoveDownloadLinksAfterExtraction: a.RemoveDownloadLinksAfterExtract, RemoveFilesAfterExtraction: a.RemoveFilesAfterExtraction,
		Secret: true,
	}
	if a.FinalPassword != "" {
		d.FinalPassword = maskValue
	}
	if len(a.Passwords) > 0 {
		d.Passwords = []string{maskValue}
	}
	return d
}

// archiveDTO is an archive with its settings.
type archiveDTO struct {
	jd.ArchiveStatus
	Settings *archiveSettingsDTO `json:"settings"`
}

// handleExtractionArchives is POST /extraction/archives: the archives of the
// selected links and packages with the settings of each.
func (s *Server) handleExtractionArchives(w http.ResponseWriter, r *http.Request) {
	var b selectionBody
	err := decode(r, &b, false)
	if err == nil {
		err = b.check()
	}
	out := []archiveDTO{}
	if err == nil {
		var c *jd.Client
		if c, err = s.client(); err == nil {
			out, err = s.archives(r, c, b)
		}
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, out)
}

func (s *Server) archives(r *http.Request, c *jd.Client, b selectionBody) ([]archiveDTO, error) {
	list, err := c.Archives(r.Context(), ints(b.LinkIDs), ints(b.PackageIDs))
	if err != nil {
		return nil, err
	}
	out := make([]archiveDTO, len(list))
	ids := make([]string, len(list))
	for i, a := range list {
		out[i].ArchiveStatus, ids[i] = a, a.ArchiveID
	}
	if len(ids) == 0 {
		return out, nil
	}
	settings, err := c.ArchiveSettings(r.Context(), ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		for j := range settings {
			if settings[j].ArchiveID == out[i].ArchiveID {
				out[i].Settings = newArchiveSettingsDTO(settings[j])
			}
		}
	}
	return out, nil
}

// handleExtractionReveal is POST /extraction/archives/{id}/reveal: the real
// finalPassword and passwords of one archive. Every call is logged without values.
func (s *Server) handleExtractionReveal(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var cur []jd.ArchiveSettings
	var err error
	if id == "" || len(id) > 512 {
		err = badRequest("The archive id is missing or too long.")
	}
	if err == nil {
		var c *jd.Client
		if c, err = s.client(); err == nil {
			cur, err = c.ArchiveSettings(r.Context(), []string{id})
		}
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := map[string]any{"finalPassword": "", "passwords": []string{}}
	for _, a := range cur {
		if a.ArchiveID == id {
			out["finalPassword"] = a.FinalPassword
			if a.Passwords != nil {
				out["passwords"] = a.Passwords
			}
		}
	}
	s.log.Info("archive passwords revealed", "archive", id) // never the values
	s.writeJSON(w, r, http.StatusOK, out)
}

// handleExtractionSettings is PUT /extraction/archives/{id}. Fields that are
// absent keep their value, passwords included. The mask is not a value.
func (s *Server) handleExtractionSettings(w http.ResponseWriter, r *http.Request) {
	var b struct {
		ExtractPath                     string    `json:"extractPath"`
		FinalPassword                   string    `json:"finalPassword"`
		Passwords                       *[]string `json:"passwords"`
		AutoExtract                     *bool     `json:"autoExtract"`
		RemoveDownloadLinksAfterExtract *bool     `json:"removeDownloadLinksAfterExtraction"`
		RemoveFilesAfterExtraction      *bool     `json:"removeFilesAfterExtraction"`
	}
	id := r.PathValue("id")
	err := decode(r, &b, false)
	if err == nil && (id == "" || len(id) > 512 || (b.Passwords != nil && len(*b.Passwords) > 1000)) {
		err = badRequest("The archive id is missing or too long, or there are too many passwords.")
	}
	if err == nil && (b.FinalPassword == maskValue || (b.Passwords != nil && slices.Contains(*b.Passwords, maskValue))) {
		err = badRequest("This is the mask of a secret, not a value. Send a new value or leave the field out.")
	}
	if err == nil {
		var c *jd.Client
		if c, err = s.client(); err == nil {
			err = s.saveArchiveSettings(r, c, id, b.ExtractPath, b.FinalPassword, b.Passwords, b.AutoExtract,
				b.RemoveDownloadLinksAfterExtract, b.RemoveFilesAfterExtraction)
		}
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	noContent(w)
}

func (s *Server) saveArchiveSettings(r *http.Request, c *jd.Client, id, path, final string, pws *[]string, auto, rmLinks, rmFiles *bool) error {
	var passwords []string
	if pws != nil {
		passwords = *pws
	} else {
		// JDownloader replaces the whole list: keep the stored one.
		cur, err := c.ArchiveSettings(r.Context(), []string{id})
		if err != nil {
			return err
		}
		for _, a := range cur {
			if a.ArchiveID == id {
				passwords = a.Passwords
			}
		}
	}
	return c.SetArchiveSettings(r.Context(), jd.ArchiveSettings{
		ArchiveID: id, ExtractPath: path, FinalPassword: final, Passwords: passwords,
		AutoExtract: auto, RemoveDownloadLinksAfterExtract: rmLinks, RemoveFilesAfterExtraction: rmFiles,
	})
}

func (s *Server) handleExtractionPassword(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Password string `json:"password"`
	}
	err := decode(r, &b, false)
	if err == nil && (b.Password == "" || len(b.Password) > 4096) {
		err = badRequest("password is required.")
	}
	if err == nil {
		var c *jd.Client
		if c, err = s.client(); err == nil {
			err = c.AddArchivePassword(r.Context(), b.Password)
		}
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	noContent(w)
}
