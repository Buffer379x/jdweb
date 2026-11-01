package sync

import (
	"errors"
	"testing"
	"time"

	"github.com/Buffer379x/jdweb/internal/jd"
	"github.com/Buffer379x/jdweb/internal/state"
)

// The values the brief and Concept 5.3, 5.5 and 10.8 ask for.
func TestDefaultConfig(t *testing.T) {
	c := DefaultConfig()
	for name, tc := range map[string]struct{ got, want time.Duration }{
		"patch interval":         {c.PatchInterval, 500 * time.Millisecond},
		"debounce":               {c.Debounce, 300 * time.Millisecond},
		"reload gap":             {c.MinReloadGap, 2 * time.Second},
		"structure check":        {c.StructureCheck, 30 * time.Second},
		"poll with browser":      {c.PollAttached, 2 * time.Second},
		"poll without browser":   {c.PollDetached, 10 * time.Second},
		"events with browser":    {c.EventsAttached, time.Second},
		"events without browser": {c.EventsDetached, 5 * time.Second},
		"backoff from":           {c.BackoffMin, time.Second},
		"backoff to":             {c.BackoffMax, 15 * time.Second},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", name, tc.got, tc.want)
		}
	}
	// A zero Config means the defaults, single fields can be overridden.
	got := Config{PatchInterval: time.Minute}.withDefaults()
	if got.PatchInterval != time.Minute || got.Debounce != c.Debounce || got.BackoffMax != c.BackoffMax || got.Logger == nil {
		t.Errorf("withDefaults = %+v", got)
	}
}

func TestBackoff(t *testing.T) {
	b := backoff{min: time.Second, max: 15 * time.Second}
	want := []time.Duration{1, 2, 4, 8, 15, 15, 15}
	for i, w := range want {
		if got := b.next(); got != w*time.Second {
			t.Errorf("step %d = %v, want %v s", i, got, w)
		}
	}
	b.reset()
	if got := b.next(); got != time.Second {
		t.Errorf("after reset = %v", got)
	}
}

func TestDebouncer(t *testing.T) {
	const quiet, gap = 300 * time.Millisecond, 2 * time.Second
	t0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	at := func(ms int) time.Time { return t0.Add(time.Duration(ms) * time.Millisecond) }

	t.Run("nothing noted, nothing due", func(t *testing.T) {
		d := debouncer{quiet: quiet, gap: gap}
		if _, ok := d.due(); ok {
			t.Error("due without an event")
		}
	})
	t.Run("one event waits for the quiet time", func(t *testing.T) {
		d := debouncer{quiet: quiet, gap: gap}
		d.note(at(0))
		if got, ok := d.due(); !ok || !got.Equal(at(300)) {
			t.Errorf("due = %v, %v", got, ok)
		}
	})
	t.Run("more events move it back", func(t *testing.T) {
		d := debouncer{quiet: quiet, gap: gap}
		d.note(at(0))
		d.note(at(200))
		if got, _ := d.due(); !got.Equal(at(500)) {
			t.Errorf("due = %v", got)
		}
	})
	t.Run("a steady stream of events cannot starve it", func(t *testing.T) {
		d := debouncer{quiet: quiet, gap: gap}
		for ms := 0; ms <= 5000; ms += 100 {
			d.note(at(ms))
		}
		if got, _ := d.due(); !got.Equal(at(2000)) {
			t.Errorf("due = %v", got)
		}
	})
	t.Run("at most one run per gap", func(t *testing.T) {
		d := debouncer{quiet: quiet, gap: gap}
		d.ran(at(0))
		if _, ok := d.due(); ok {
			t.Error("due after a run without new events")
		}
		d.note(at(100))
		if got, _ := d.due(); !got.Equal(at(2000)) {
			t.Errorf("due = %v", got)
		}
		d.ran(at(2000))
		d.note(at(4000))
		if got, _ := d.due(); !got.Equal(at(4300)) {
			t.Errorf("after the gap: due = %v", got)
		}
	})
}

func TestClassify(t *testing.T) {
	const url = "http://jd.example:3128"
	conn := &jd.Error{Type: jd.TypeConnection}
	other := &jd.Error{Type: jd.TypeInvalidResponse, Status: 200}
	unknownPath := &jd.Error{Type: jd.TypeNotImplemented, Status: 501}
	internal := &jd.Error{Type: jd.TypeInternal, Status: 500, Raw: "java.lang.NullPointerException"}
	for name, tc := range map[string]struct {
		err    error
		wasUp  bool
		want   state.Connection
		detail string
	}{
		"refused, never up":     {conn, false, state.ConnMisconfigured, state.DetailNotReachable(url)},
		"refused, was up":       {conn, true, state.ConnOffline, state.DetailOffline(url)},
		"other service":         {other, false, state.ConnMisconfigured, state.DetailOtherService},
		"other service, was up": {other, true, state.ConnMisconfigured, state.DetailOtherService},
		"unknown path":          {unknownPath, true, state.ConnMisconfigured, state.DetailOtherService},
		"no ping":               {errNotJDownloader, false, state.ConnMisconfigured, state.DetailOtherService},
		"subscription lost":     {errSubscriptionLost, true, state.ConnSyncing, ""},
		"internal error":        {internal, true, state.ConnOffline, state.DetailFailing(url)},
	} {
		t.Run(name, func(t *testing.T) {
			got, detail := classify(tc.err, tc.wasUp, url)
			if got != tc.want || detail != tc.detail {
				t.Errorf("classify = %q, %q", got, detail)
			}
		})
	}
}

func TestPingError(t *testing.T) {
	conn := &jd.Error{Type: jd.TypeConnection}
	internal := &jd.Error{Type: jd.TypeInternal}
	notFound := &jd.Error{Type: jd.TypeFileNotFound, Status: 404}
	if err := pingError(conn, false); !jd.IsConnection(err) {
		t.Errorf("connection = %v", err)
	}
	if err := pingError(internal, true); !jd.IsInternal(err) {
		t.Errorf("internal, was up = %v", err)
	}
	for _, err := range []error{pingError(internal, false), pingError(notFound, true)} {
		if !errors.Is(err, errNotJDownloader) {
			t.Errorf("err = %v", err)
		}
	}
}
