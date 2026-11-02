package sync

import (
	"context"
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
		"refused, never up": {conn, false, state.ConnMisconfigured, state.DetailNotReachable(url)},
		"refused, was up":   {conn, true, state.ConnOffline, state.DetailOffline(url)},
		// Only a failed ping says another service answers.
		"no ping":                 {errNotJDownloader, false, state.ConnMisconfigured, state.DetailOtherService},
		"no ping, was up":         {errNotJDownloader, true, state.ConnMisconfigured, state.DetailOtherService},
		"bad answer after ping":   {other, true, state.ConnOffline, state.DetailFailing(url)},
		"unknown path after ping": {unknownPath, true, state.ConnOffline, state.DetailFailing(url)},
		"no subscription id":      {errNoSubscription, true, state.ConnOffline, state.DetailFailing(url)},
		"subscription lost":       {errSubscriptionLost, true, state.ConnSyncing, ""},
		"internal error":          {internal, true, state.ConnOffline, state.DetailFailing(url)},
	} {
		t.Run(name, func(t *testing.T) {
			got, detail := classify(tc.err, tc.wasUp, url)
			if got != tc.want || detail != tc.detail {
				t.Errorf("classify = %q, %q", got, detail)
			}
		})
	}
}

func TestOnlyLostConnectionsEndASession(t *testing.T) {
	for _, err := range []error{
		&jd.Error{Type: jd.TypeInvalidResponse, Status: 200},
		&jd.Error{Type: jd.TypeNotImplemented, Status: 501},
		&jd.Error{Type: jd.TypeInternal, Status: 500},
		&jd.Error{Type: jd.TypeBadParameters, Status: 400},
	} {
		if isFatal(err) {
			t.Errorf("%v ends a session", err)
		}
	}
	for _, err := range []error{&jd.Error{Type: jd.TypeConnection}, context.Canceled, context.DeadlineExceeded} {
		if !isFatal(err) {
			t.Errorf("%v does not end a session", err)
		}
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

// A JDownloader that stays away must not fill the log: the same state is
// logged once, then at most once per minute, and a changed state at once.
func TestRepeatLogThrottles(t *testing.T) {
	var r repeatLog
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if !r.allow("offline|a", t0) {
		t.Fatal("the first message must pass")
	}
	for i := 1; i < 60; i++ {
		if r.allow("offline|a", t0.Add(time.Duration(i)*time.Second)) {
			t.Fatalf("the same state passed again after %d s", i)
		}
	}
	if !r.allow("offline|a", t0.Add(time.Minute)) {
		t.Error("the same state must be logged again after a minute")
	}
	if r.allow("offline|a", t0.Add(time.Minute+time.Second)) {
		t.Error("the minute starts over after a message")
	}
	if !r.allow("misconfigured|b", t0.Add(time.Minute+2*time.Second)) {
		t.Error("a changed state must pass at once")
	}
	r.reset()
	if !r.allow("misconfigured|b", t0.Add(time.Minute+3*time.Second)) {
		t.Error("after a working connection the next loss must be logged")
	}
}
