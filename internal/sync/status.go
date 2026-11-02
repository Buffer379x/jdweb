package sync

import (
	"context"
	"errors"
	"time"

	"github.com/Buffer379x/jdweb/internal/jd"
	"github.com/Buffer379x/jdweb/internal/state"
)

var (
	// errNotJDownloader means something answers that does not behave like JDownloader.
	errNotJDownloader = errors.New("sync: the answer is not from JDownloader")
	// errSubscriptionLost means the event subscription is gone or useless:
	// events/listen answered 404, getsubscriptionstatus subscriptionid -1, or
	// the long-poll failed maxListenFailures times in a row.
	errSubscriptionLost = errors.New("sync: the event subscription is lost")
	// errNoSubscription means events/subscribe answered without an id.
	errNoSubscription = errors.New("sync: JDownloader answered a subscribe without an id")
)

// isFatal reports errors that end a running session: nobody answers any more.
// Whether something answers but is not JDownloader is for the ping to say. After
// a good ping any other error is a bad answer of a JDownloader that is there: it
// is logged and tried again, and does not make the browsers see a lost connection.
func isFatal(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || jd.IsConnection(err)
}

// pingError judges a failed ping. A ping cannot fail in JDownloader, so any
// answer but a yes comes from something else. Only a JDownloader we already know
// gets the benefit of the doubt for an internal error, it may be just starting.
func pingError(err error, wasUp bool) error {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded), jd.IsConnection(err):
		return err
	case jd.IsInternal(err) && wasUp:
		return err
	}
	return errNotJDownloader
}

// classify turns the end of a session into the state the browsers see
// (Review Focus 3). Only a failed ping says another service answers. The detail
// never holds response text of JDownloader: stacktraces stay in the log.
func classify(err error, wasUp bool, url string) (state.Connection, string) {
	switch {
	case errors.Is(err, errSubscriptionLost):
		return state.ConnSyncing, ""
	case errors.Is(err, errNotJDownloader):
		return state.ConnMisconfigured, state.DetailOtherService
	case jd.IsConnection(err) && !wasUp:
		return state.ConnMisconfigured, state.DetailNotReachable(url)
	case jd.IsConnection(err):
		return state.ConnOffline, state.DetailOffline(url)
	}
	return state.ConnOffline, state.DetailFailing(url)
}

type connectionPayload struct {
	Connection state.Connection `json:"connection"`
	Detail     string           `json:"detail"`
}

type countPayload struct {
	Count int `json:"count"`
}

// update changes the status of the current address and tells the browsers what
// changed. A session of an older address changes nothing any more.
func (e *Engine) update(gen uint64, fn func(*state.Status)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if gen != e.gen {
		return
	}
	before := e.status
	fn(&e.status)
	e.announce(before, e.status)
}

// announce publishes the differences. It runs under e.mu, which keeps the events
// in the order of the changes; the hub never blocks.
func (e *Engine) announce(before, after state.Status) {
	if before == after {
		return
	}
	if before.Connection != after.Connection || before.ConnectionDetail != after.ConnectionDetail {
		e.publish("connection", connectionPayload{after.Connection, after.ConnectionDetail})
	}
	if before.Captchas != after.Captchas {
		e.publish("captcha", countPayload{after.Captchas})
	}
	if before.Dialogs != after.Dialogs {
		e.publish("dialog", countPayload{after.Dialogs})
	}
	e.publish("status", after)
}

// refreshTotals brings the list sums of the status up to date with the mirror.
func (e *Engine) refreshTotals(gen uint64) {
	dl, lg := e.store.Totals()
	e.update(gen, func(st *state.Status) {
		st.Downloads = dl
		lg.Crawling = st.Linkgrabber.Crawling
		st.Linkgrabber = lg
	})
}

// pollStatus asks JDownloader for everything that does not arrive as an event.
// What fails with an error of JDownloader keeps its last value and is logged.
func (s *session) pollStatus(ctx context.Context) error {
	e := s.e
	var apply []func(*state.Status)
	// soft returns the error only if it ends the session.
	soft := func(what string, err error) (ok bool, fatal error) {
		switch {
		case err == nil:
			return true, nil
		case isFatal(err):
			return false, err
		}
		e.log.Warn("status poll: "+what+" failed", "err", err)
		return false, nil
	}

	tb, err := s.c.Toolbar(ctx)
	ok, fatal := soft("toolbar", err)
	if fatal != nil {
		return fatal
	}
	if ok {
		s.idle = state.NormalizeController(tb.State) == string(jd.ControllerIdle)
		apply = append(apply, func(st *state.Status) {
			st.Controller = state.NormalizeController(tb.State)
			st.Speed = max(tb.Speed, 0)
			st.Toggles.Reconnect, st.Toggles.Premium = tb.Reconnect, tb.Premium
			st.Toggles.Clipboard, st.Toggles.StopAfter = tb.Clipboard, tb.StopAfter
		})
	}

	// The limit comes from the config: during a pause the toolbar shows the pause speed.
	on, limit, err := s.c.SpeedLimit(ctx)
	ok, fatal = soft("speed limit", err)
	if fatal != nil {
		return fatal
	}
	if ok {
		apply = append(apply, func(st *state.Status) { st.Toggles.Limit, st.Toggles.LimitBytes = on, limit })
	}

	collecting, err := s.c.IsCollecting(ctx)
	ok, fatal = soft("collecting", err)
	if fatal != nil {
		return fatal
	}
	crawling, err := s.c.IsCrawling(ctx)
	ok2, fatal := soft("crawling", err)
	if fatal != nil {
		return fatal
	}
	if ok && ok2 {
		apply = append(apply, func(st *state.Status) { st.Linkgrabber.Crawling = collecting || crawling })
	}

	captchas, err := s.c.Captchas(ctx)
	ok, fatal = soft("captchas", err)
	if fatal != nil {
		return fatal
	}
	if ok {
		apply = append(apply, func(st *state.Status) { st.Captchas = len(captchas) })
	}

	dialogs, err := s.c.DialogIDs(ctx)
	ok, fatal = soft("dialogs", err)
	if fatal != nil {
		return fatal
	}
	if ok {
		apply = append(apply, func(st *state.Status) { st.Dialogs = len(dialogs) })
	}

	s.lastPoll = time.Now()
	e.update(s.gen, func(st *state.Status) {
		for _, fn := range apply {
			fn(st)
		}
	})
	return nil
}
