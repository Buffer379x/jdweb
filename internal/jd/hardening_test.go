package jd_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Buffer379x/jdweb/internal/jd"
)

func TestIcons(t *testing.T) {
	ctx := context.Background()
	hoster, file := binary(t, "icon_hoster.png"), binary(t, "icon_file.png")
	s := newStub(t, map[string]reply{
		"/contentV2/getFavIcon":  {200, hoster},
		"/contentV2/getFileIcon": {200, file},
	})
	s.ctypes["/contentV2/getFavIcon"] = "image/png"
	s.ctypes["/contentV2/getFileIcon"] = "image/png"
	c := s.client()

	data, ct, err := c.HosterIcon(ctx, "1fichier.com")
	if err != nil || ct != "image/png" || !bytes.Equal(data, hoster) || !bytes.HasPrefix(data, []byte("\x89PNG")) {
		t.Fatalf("hoster icon: %d bytes, %q, %v", len(data), ct, err)
	}
	data, ct, err = c.FileIcon(ctx, "archive.zip")
	if err != nil || ct != "image/png" || !bytes.Equal(data, file) {
		t.Fatalf("file icon: %d bytes, %q, %v", len(data), ct, err)
	}
	reqs := s.requests
	if string(reqs[0].Body) != `{"params":["1fichier.com"]}` || string(reqs[1].Body) != `{"params":["archive.zip"]}` {
		t.Errorf("bodies = %s, %s", reqs[0].Body, reqs[1].Body)
	}

	t.Run("an error object of JDownloader is still an error", func(t *testing.T) {
		s := newStub(t, map[string]reply{"/contentV2/getFavIcon": {400, fixture(t, "err_no_such_function_400")}})
		_, _, err := s.client().HosterIcon(ctx, "")
		var e *jd.Error
		if !errors.As(err, &e) || e.Type != jd.TypeBadParameters || e.Status != 400 {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("JSON instead of an image is an invalid response", func(t *testing.T) {
		s := newStub(t, map[string]reply{"/contentV2/getFileIcon": ok(t, "device_ping")})
		if _, _, err := s.client().FileIcon(ctx, "a.zip"); !jd.IsInvalidResponse(err) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("another service", func(t *testing.T) {
		s := newStub(t, map[string]reply{"/contentV2/getFileIcon": {200, []byte("<html>")}})
		s.ctypes["/contentV2/getFileIcon"] = "text/html"
		if _, _, err := s.client().FileIcon(ctx, "a.zip"); !jd.IsInvalidResponse(err) {
			t.Errorf("err = %v", err)
		}
	})
}

func TestControllerStatesAreTheKnownRawStates(t *testing.T) {
	want := []jd.ControllerState{"IDLE", "RUNNING", "PAUSE", "STOPPING", "STOPPED_STATE"}
	got := jd.ControllerStates()
	if len(got) != len(want) {
		t.Fatalf("states = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("states = %v, want %v", got, want)
		}
	}
	// Every state seen in the recordings is in the list.
	known := map[jd.ControllerState]bool{}
	for _, s := range got {
		known[s] = true
	}
	for _, name := range []string{"dl_controller_state_running", "dl_controller_state_stopped"} {
		s, err := serve(t, "/downloadcontroller/getCurrentState", name).ControllerState(context.Background())
		if err != nil || !known[s] {
			t.Errorf("%s: %q, %v", name, s, err)
		}
	}
	for _, name := range []string{"toolbar_status_idle", "toolbar_status_running", "toolbar_status_stopped"} {
		st, err := serve(t, "/toolbar/getStatus", name).Toolbar(context.Background())
		if err != nil || !known[st.State] {
			t.Errorf("%s: %q, %v", name, st.State, err)
		}
	}
	if jd.ControllerStopped != "STOPPED_STATE" {
		t.Error("STOPPED_STATE, which docs/api.md does not list, must stay visible")
	}
}

func TestErrorTextIsCutOnRuneBoundaries(t *testing.T) {
	// Emoji are 4 bytes, the Hebrew letters 2 bytes. A byte cut lands inside a rune
	// for most lengths, so try a span of them.
	hostile := strings.Repeat("😀שלום עולם", 600)
	for _, body := range []string{hostile, strings.Repeat("a", 4095) + "😀😀", strings.Repeat("é", 5000), "\xff\xfe broken " + hostile} {
		s := newStub(t, map[string]reply{"/p": {500, []byte(body)}})
		err := s.client().Call(context.Background(), "/p", nil)
		var e *jd.Error
		if !errors.As(err, &e) {
			t.Fatal(err)
		}
		if !utf8.ValidString(e.Raw) {
			t.Errorf("Raw is not valid UTF-8: %q", e.Raw[len(e.Raw)-12:])
		}
		if len(e.Raw) > 4096+3 {
			t.Errorf("Raw has %d bytes", len(e.Raw))
		}
		if !utf8.ValidString(err.Error()) {
			t.Errorf("Error() is not valid UTF-8")
		}
	}
	// Error() cuts a long text of another service as well, on a rune boundary.
	for n := 150; n < 170; n++ {
		s := newStub(t, map[string]reply{"/p": {404, []byte(strings.Repeat("a", n-150) + strings.Repeat("😀", 40))}})
		err := s.client().Call(context.Background(), "/p", nil)
		if !utf8.ValidString(err.Error()) {
			t.Fatalf("n=%d: Error() = %q", n, err.Error())
		}
	}
	// Right-to-left text and its control characters stay text.
	rtl := "‮exe.txt שלום"
	s := newStub(t, map[string]reply{"/p": {404, []byte(rtl)}})
	if err := s.client().Call(context.Background(), "/p", nil); !utf8.ValidString(err.Error()) || !strings.Contains(err.Error(), "שלום") {
		t.Errorf("Error() = %q", err.Error())
	}
}

func TestPasswordsStayOutOfErrors(t *testing.T) {
	ctx := context.Background()
	const secret = "s3cr3t-P@ss"
	echo := []byte("{\n \"src\"  : \"DEVICE\",\n \"data\" : \"\\\"" + secret + "\\\"\",\n \"type\" : \"BAD_PARAMETERS\"\n}")
	calls := map[string]struct {
		path string
		call func(c *jd.Client) error
	}{
		"AddAccount":      {"/accountsV2/addAccount", func(c *jd.Client) error { return c.AddAccount(ctx, "h.com", "u", secret) }},
		"SetAccountLogin": {"/accountsV2/setUserNameAndPassword", func(c *jd.Client) error { return c.SetAccountLogin(ctx, 1, "u", secret) }},
		"AddBasicAuth": {"/accountsV2/addBasicAuth", func(c *jd.Client) error {
			_, err := c.AddBasicAuth(ctx, jd.BasicAuthHTTP, "*", "u", secret)
			return err
		}},
		"UpdateBasicAuth": {"/accountsV2/updateBasicAuth", func(c *jd.Client) error {
			return c.UpdateBasicAuth(ctx, jd.BasicAuth{ID: 1, Type: jd.BasicAuthHTTP, Password: secret})
		}},
		"AddArchivePassword": {"/extraction/addArchivePassword", func(c *jd.Client) error { return c.AddArchivePassword(ctx, secret) }},
		"Downloads.SetPassword": {"/downloadsV2/setDownloadPassword", func(c *jd.Client) error {
			return c.Downloads().SetPassword(ctx, secret, []int64{1}, nil)
		}},
		"Linkgrabber.SetPassword": {"/linkgrabberv2/setDownloadPassword", func(c *jd.Client) error {
			return c.Linkgrabber().SetPassword(ctx, secret, nil, []int64{1})
		}},
		"AddLinks": {"/linkgrabberv2/addLinks", func(c *jd.Client) error {
			_, err := c.AddLinks(ctx, jd.AddLinksRequest{Links: []string{"http://h/x"}, ExtractPassword: secret, DownloadPassword: secret})
			return err
		}},
		"SetArchiveSettings": {"/extraction/setArchiveSettings", func(c *jd.Client) error {
			return c.SetArchiveSettings(ctx, jd.ArchiveSettings{ArchiveID: "a", FinalPassword: func() *string { s := secret; return &s }(), Passwords: []string{secret}})
		}},
		"ConfigSet": {"/config/set", func(c *jd.Client) error { return c.ConfigSet(ctx, "i", "", "ProxyPassword", secret) }},
		"DialogAnswer": {"/dialogs/answer", func(c *jd.Client) error {
			return c.DialogAnswer(ctx, 1, map[string]any{"password": secret})
		}},
		"PluginSet": {"/plugins/set", func(c *jd.Client) error { return c.PluginSet(ctx, "i", "p", "Password", secret) }},
	}
	for name, tc := range calls {
		t.Run(name, func(t *testing.T) {
			for _, status := range []int{400, 500} {
				s := newStub(t, map[string]reply{tc.path: {status, echo}})
				err := tc.call(s.client())
				var e *jd.Error
				if !errors.As(err, &e) {
					t.Fatalf("err = %v", err)
				}
				if strings.Contains(err.Error(), secret) || strings.Contains(e.Raw, secret) {
					t.Errorf("HTTP %d: the password reached the error: %v / %q", status, err, e.Raw)
				}
				if e.Status != status || e.Path != tc.path {
					t.Errorf("status and path must stay: %+v", e)
				}
				if status == 400 && e.Type != jd.TypeBadParameters {
					t.Errorf("type = %q", e.Type)
				}
			}
		})
	}
	t.Run("the error type is left out when it echoes the input", func(t *testing.T) {
		typeEcho := []byte(`{"src":"DEVICE","data":null,"type":"Invalid value: ` + secret + `"}`)
		for _, status := range []int{400, 404} {
			s := newStub(t, map[string]reply{"/accountsV2/addAccount": {status, typeEcho}})
			err := s.client().AddAccount(ctx, "h.com", "u", secret)
			var e *jd.Error
			if !errors.As(err, &e) {
				t.Fatalf("err = %v", err)
			}
			if strings.Contains(err.Error(), secret) || strings.Contains(e.Type, secret) || strings.Contains(e.Raw, secret) {
				t.Errorf("HTTP %d: the type echoed the password: %v / %+v", status, err, e)
			}
			if e.Type != jd.TypeRedacted || e.Status != status {
				t.Errorf("Error = %+v", e)
			}
		}
	})
	t.Run("dialog errors that name only an id keep their type", func(t *testing.T) {
		for _, typ := range []string{"Answer Dialog 1793566158196 first", "Invalid ID: 12"} {
			s := newStub(t, map[string]reply{"/dialogs/answer": {400, []byte(`{"src":"DEVICE","data":null,"type":"` + typ + `"}`)}})
			var e *jd.Error
			if err := s.client().DialogAnswer(ctx, 1, map[string]any{"password": secret}); !errors.As(err, &e) || e.Type != typ {
				t.Errorf("%q: %v", typ, err)
			}
		}
		// ...but not when the type carries other text, such as the answer.
		s := newStub(t, map[string]reply{"/dialogs/answer": {400, []byte(`{"src":"DEVICE","data":null,"type":"Invalid ID: ` + secret + `"}`)}})
		var e *jd.Error
		if err := s.client().DialogAnswer(ctx, 1, nil); !errors.As(err, &e) || e.Type != jd.TypeRedacted {
			t.Errorf("echoed answer: %v", err)
		}
	})
	t.Run("other calls keep the text for diagnosis", func(t *testing.T) {
		s := newStub(t, map[string]reply{"/accountsV2/enableAccounts": {400, echo}})
		err := s.client().EnableAccounts(ctx, []int64{1})
		if !strings.Contains(err.Error(), secret) {
			t.Errorf("err = %v", err)
		}
	})
}

func TestResponseLimit(t *testing.T) {
	big := append([]byte(`{"data":"`), bytes.Repeat([]byte("a"), 33<<20)...)
	big = append(big, `"}`...)
	s := newStub(t, map[string]reply{"/x": {200, big}})
	var out string
	err := s.client().Call(context.Background(), "/x", &out)
	if !jd.IsInvalidResponse(err) {
		t.Errorf("err = %v, want JD_INVALID_RESPONSE for a response over 32 MiB", err)
	}
	ok32 := append([]byte(`{"data":"`), bytes.Repeat([]byte("a"), 3<<20)...)
	ok32 = append(ok32, `"}`...)
	s = newStub(t, map[string]reply{"/x": {200, ok32}})
	if err := s.client().Call(context.Background(), "/x", &out); err != nil || len(out) != 3<<20 {
		t.Errorf("3 MB answer: %d, %v", len(out), err)
	}
}
