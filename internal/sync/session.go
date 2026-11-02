package sync

import (
	"context"
	"time"

	"github.com/Buffer379x/jdweb/internal/jd"
	"github.com/Buffer379x/jdweb/internal/state"
)

// maxListenFailures is how many long-polls in a row may fail with an answer
// before the subscription is given up. A failure without an answer ends the
// session at once anyway.
const maxListenFailures = 5

// emptyPollPause is the least time one empty long-poll takes.
const emptyPollPause = 100 * time.Millisecond

// session holds what one connection needs. Only the session loop touches it,
// except for the fields listen reads, which never change.
type session struct {
	e   *Engine
	c   *jd.Client
	gen uint64
	sub int64

	catchUp chan struct{} // the long-poll failed, events may have been missed

	dl, lg   debouncer
	counters struct{ dl, lg int64 }
	idle     bool      // the last toolbar said the controller does not run
	lastIdle time.Time // when that last caused a reload
	fast     bool      // the browser state the interval and the poll rate are set for
	lastPoll time.Time
	pollDue  time.Time
	poll     *time.Timer
	reload   *time.Timer
}

// load does what comes before connected: the status event interval, both lists,
// the status. Events that happen meanwhile wait in the subscription and are
// applied afterwards.
func (s *session) load(ctx context.Context) error {
	s.fast = s.e.attached()
	if err := s.setInterval(ctx); err != nil {
		return err
	}
	now := time.Now()
	if _, err := s.reloadList(ctx, state.Downloads); err != nil {
		return err
	}
	if _, err := s.reloadList(ctx, state.Linkgrabber); err != nil {
		return err
	}
	s.dl.ran(now)
	s.lg.ran(now)
	// Whatever browsers hold now is out of date, changed or not.
	s.e.store.Drain()
	s.e.publishStructure()
	s.e.refreshTotals(s.gen)
	if err := s.pollStatus(ctx); err != nil {
		return err
	}
	return nil
}

func (s *session) setInterval(ctx context.Context) error {
	d := s.e.cfg.EventsDetached
	if s.fast {
		d = s.e.cfg.EventsAttached
	}
	if err := s.c.SetStatusEventInterval(ctx, s.sub, d.Milliseconds()); err != nil {
		if isFatal(err) {
			return err
		}
		s.e.log.Warn("cannot set the status event interval", "err", err)
	}
	return nil
}

func (s *session) pollInterval() time.Duration {
	if s.fast {
		return s.e.cfg.PollAttached
	}
	return s.e.cfg.PollDetached
}

// reloadList loads one list. The counter is read first: a change during the
// load then shows at the next check instead of going unnoticed. It only counts
// as seen once the load worked, so a failed load is tried again by the next check.
func (s *session) reloadList(ctx context.Context, list state.List) (structure bool, err error) {
	if list == state.Downloads {
		counter, _, err := s.c.DownloadsStructureChanged(ctx, s.counters.dl)
		if err != nil {
			return false, err
		}
		pkgs, err := s.c.DownloadPackages(ctx, jd.AllDownloadPackageFields())
		if err != nil {
			return false, err
		}
		links, err := s.c.DownloadLinks(ctx, jd.AllDownloadLinkFields())
		if err != nil {
			return false, err
		}
		s.counters.dl = counter
		return s.e.store.ReplaceDownloads(pkgs, links), nil
	}
	counter, _, err := s.c.GrabberStructureChanged(ctx, s.counters.lg)
	if err != nil {
		return false, err
	}
	pkgs, err := s.c.GrabberPackages(ctx, jd.AllGrabberPackageFields())
	if err != nil {
		return false, err
	}
	links, err := s.c.GrabberLinks(ctx, jd.AllGrabberLinkFields())
	if err != nil {
		return false, err
	}
	s.counters.lg = counter
	return s.e.store.ReplaceLinkgrabber(pkgs, links), nil
}

// listen is the long-poll. It hands batches to the session loop and blocks while
// the loop is busy, so events keep their order and wait in JDownloader's queue.
//
// A failed poll may have cost events, so each failure asks the loop to reload
// both lists. A poll that keeps failing while the rest of the session looks fine
// ends in a new subscription, and the ping of the new session finds out who answers.
func (s *session) listen(ctx context.Context, events chan<- []jd.Event, failed chan<- error) {
	failures := 0
	for {
		started := time.Now()
		batch, err := s.c.Listen(ctx, s.sub)
		switch {
		case ctx.Err() != nil:
			return
		case jd.IsNotFound(err):
			failed <- errSubscriptionLost
			return
		case err != nil && isFatal(err):
			failed <- err
			return
		case err != nil:
			failures++
			s.e.log.Warn("the JDownloader event poll failed", "failures", failures, "err", err)
			if failures >= maxListenFailures {
				failed <- errSubscriptionLost
				return
			}
			select {
			case s.catchUp <- struct{}{}:
			default:
			}
			if !wait(ctx, s.e.cfg.ListenRetry) {
				return
			}
			continue
		}
		failures = 0
		if len(batch) == 0 {
			// JDownloader holds an empty poll for 25 s. One that comes back at once
			// must not turn this into a hot loop.
			if !wait(ctx, emptyPollPause-time.Since(started)) {
				return
			}
			continue
		}
		select {
		case events <- batch:
		case <-ctx.Done():
			return
		}
	}
}

// wait returns false when ctx ended first. A wait of zero or less returns at once.
func wait(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// follow is the session loop. It returns why the session ended.
func (s *session) follow(ctx context.Context, events <-chan []jd.Event, failed <-chan error) error {
	e := s.e
	patches := time.NewTicker(e.cfg.PatchInterval)
	defer patches.Stop()
	safety := time.NewTicker(e.cfg.StructureCheck)
	defer safety.Stop()
	s.poll = time.NewTimer(s.pollInterval())
	defer s.poll.Stop()
	s.pollDue = time.Now().Add(s.pollInterval())
	s.lastPoll = time.Now()
	s.reload = time.NewTimer(time.Hour)
	s.reload.Stop()
	defer s.reload.Stop()

	for {
		var err error
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err = <-failed:
		case batch := <-events:
			s.apply(batch)
		case <-patches.C:
			e.flush(s.gen)
		case <-s.reload.C:
			err = s.reloadDue(ctx)
		case <-s.poll.C:
			err = s.pollStatus(ctx)
			s.noteIfStuck()
			s.armPoll(time.Now().Add(s.pollInterval()), true)
		case <-s.catchUp:
			now := time.Now()
			s.dl.note(now)
			s.lg.note(now)
			s.armReload()
		case <-safety.C:
			err = s.safetyNet(ctx)
		case <-e.attach:
			err = s.browsersChanged(ctx)
		case list := <-e.resync:
			s.noteResync(list)
		case <-e.refresh:
			s.armPoll(s.lastPoll.Add(pollSoon), false)
		}
		if err != nil {
			return err
		}
	}
}

func (s *session) apply(batch []jd.Event) {
	now := time.Now()
	wantPoll := false
	for _, ev := range batch {
		eff := s.e.store.ApplyEvent(ev)
		switch eff.Reload {
		case state.Downloads:
			s.dl.note(now)
		case state.Linkgrabber:
			s.lg.note(now)
		}
		wantPoll = wantPoll || eff.Poll
		if eff.Unknown {
			s.e.logUnknown(ev)
		}
	}
	s.armReload()
	if wantPoll {
		due := now.Add(pollSoon)
		if earliest := s.lastPoll.Add(pollSoon); due.Before(earliest) {
			due = earliest
		}
		s.armPoll(due, false)
	}
}

// armPoll sets the next poll. Unless force is set it only ever brings it forward.
func (s *session) armPoll(due time.Time, force bool) {
	if !force && !due.Before(s.pollDue) {
		return
	}
	s.pollDue = due
	s.poll.Reset(time.Until(due))
}

func (s *session) armReload() {
	next, ok := s.dl.due()
	if t, lgOK := s.lg.due(); lgOK && (!ok || t.Before(next)) {
		next, ok = t, true
	}
	if !ok {
		s.reload.Stop()
		return
	}
	s.reload.Reset(time.Until(next))
}

// reloadDue loads the lists whose time has come. An error of JDownloader is
// logged and the reload tried again later; only a connection problem ends the session.
func (s *session) reloadDue(ctx context.Context) error {
	now := time.Now()
	for _, r := range []struct {
		list state.List
		d    *debouncer
	}{{state.Downloads, &s.dl}, {state.Linkgrabber, &s.lg}} {
		if t, ok := r.d.due(); !ok || t.After(now) {
			continue
		}
		r.d.ran(now)
		if err := s.reloadAndAnnounce(ctx, r.list); err != nil {
			if isFatal(err) {
				return err
			}
			s.e.log.Warn("cannot reload a list, trying again", "list", r.list, "err", err)
			r.d.note(now)
		}
	}
	s.armReload()
	return nil
}

func (s *session) reloadAndAnnounce(ctx context.Context, list state.List) error {
	structure, err := s.reloadList(ctx, list)
	if err != nil {
		return err
	}
	if structure {
		s.e.publish("structure", structurePayload{list})
	}
	s.e.refreshTotals(s.gen)
	return nil
}

// safetyNet catches what events missed: a subscription JDownloader forgot, and
// structure changes nobody announced (Concept 5.3, step 7).
func (s *session) safetyNet(ctx context.Context) error {
	st, err := s.c.SubscriptionStatus(ctx, s.sub)
	switch {
	case err != nil && isFatal(err):
		return err
	case err != nil:
		s.e.log.Warn("cannot read the subscription status", "err", err)
	case !st.Exists(s.sub):
		return errSubscriptionLost
	}

	_, dlChanged, err := s.c.DownloadsStructureChanged(ctx, s.counters.dl)
	if err == nil && dlChanged {
		s.dl.ran(time.Now())
		err = s.reloadAndAnnounce(ctx, state.Downloads)
	}
	if err != nil && isFatal(err) {
		return err
	}
	if err != nil {
		s.e.log.Warn("the download list check failed", "err", err)
	}
	_, lgChanged, err := s.c.GrabberStructureChanged(ctx, s.counters.lg)
	if err == nil && lgChanged {
		s.lg.ran(time.Now())
		err = s.reloadAndAnnounce(ctx, state.Linkgrabber)
	}
	if err != nil && isFatal(err) {
		return err
	}
	if err != nil {
		s.e.log.Warn("the linkgrabber check failed", "err", err)
	}
	s.armReload()
	return nil
}

// noteIfStuck catches what no event reported: the controller does not run, yet
// the mirror holds running links. A reload settles it. If JDownloader really
// reports that combination the next reload waits for the next structure check,
// so it cannot turn into a reload per poll.
func (s *session) noteIfStuck() {
	if !s.idle {
		return
	}
	if dl, _ := s.e.store.Totals(); dl.Running == 0 {
		return
	}
	now := time.Now()
	if !s.lastIdle.IsZero() && now.Sub(s.lastIdle) < s.e.cfg.StructureCheck {
		return
	}
	s.lastIdle = now
	s.dl.note(now)
	s.armReload()
}

// noteResync schedules the reload a call of Resync asked for.
func (s *session) noteResync(list state.List) {
	now := time.Now()
	if list == state.Linkgrabber {
		s.lg.note(now)
	} else {
		s.dl.note(now)
	}
	s.armReload()
}

// browsersChanged follows a browser count that crossed zero.
func (s *session) browsersChanged(ctx context.Context) error {
	fast := s.e.attached()
	if fast == s.fast {
		return nil
	}
	s.fast = fast
	if err := s.setInterval(ctx); err != nil {
		return err
	}
	if fast {
		err := s.pollStatus(ctx)
		s.noteIfStuck()
		s.armPoll(time.Now().Add(s.pollInterval()), true)
		return err
	}
	s.armPoll(time.Now().Add(s.pollInterval()), true)
	return nil
}
