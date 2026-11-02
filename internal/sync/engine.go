// Package sync keeps the mirror in state up to date with one JDownloader and
// tells the browsers through sse: one event subscription, a long-poll, a full
// load at the start, debounced reloads on structure events, bundled patches, a
// status poll, and self-healing with backoff when JDownloader goes away.
//
// One goroutine (the session loop) does all of it except the long-poll. Events
// and reloads therefore never overlap: a batch of events that arrives while a
// reload runs is applied after it. The other order would let the older answer
// of the reload overwrite the newer event, and a link that stopped would stay
// "running" until the next reload.
package sync

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/Buffer379x/jdweb/internal/jd"
	"github.com/Buffer379x/jdweb/internal/sse"
	"github.com/Buffer379x/jdweb/internal/state"
)

// ErrRunning is returned by Run when the engine runs already.
var ErrRunning = errors.New("sync: engine is already running")

// unsubscribeTimeout limits the goodbye to a JDownloader that is being left.
const unsubscribeTimeout = 2 * time.Second

// pollSoon is how long a status poll asked for by an event waits, so that a few
// events in a row cause one poll, and the least time between two such polls.
const pollSoon = 250 * time.Millisecond

// Config holds the timing of the engine. A zero field means its default, see
// DefaultConfig.
type Config struct {
	PatchInterval  time.Duration // how often patches go to the browsers
	Debounce       time.Duration // quiet time after a structure event before the reload
	MinReloadGap   time.Duration // least time between two reloads of a list
	StructureCheck time.Duration // how often the safety net looks at counters and subscription
	PollAttached   time.Duration // status poll with a browser open
	PollDetached   time.Duration // status poll without a browser
	EventsAttached time.Duration // JDownloader's status event interval with a browser open
	EventsDetached time.Duration // the same without a browser
	BackoffMin     time.Duration // first wait after a failed connection
	BackoffMax     time.Duration // longest wait
	StableAfter    time.Duration // a connection this old starts the backoff over when it is lost
	ListenRetry    time.Duration // wait after a long-poll that failed with an error of JDownloader
	Logger         *slog.Logger  // slog.Default() when nil
	// ClientOptions are used for the clients SetURL creates.
	ClientOptions []jd.Option
}

// DefaultConfig returns the timing of Concept 5.3, 5.5 and 10.8.
func DefaultConfig() Config {
	return Config{
		PatchInterval:  500 * time.Millisecond,
		Debounce:       300 * time.Millisecond,
		MinReloadGap:   2 * time.Second,
		StructureCheck: 30 * time.Second,
		PollAttached:   2 * time.Second,
		PollDetached:   10 * time.Second,
		EventsAttached: time.Second,
		EventsDetached: 5 * time.Second,
		BackoffMin:     time.Second,
		BackoffMax:     15 * time.Second,
		StableAfter:    10 * time.Second,
		ListenRetry:    time.Second,
	}
}

func (c Config) withDefaults() Config {
	d := DefaultConfig()
	fill := func(v *time.Duration, def time.Duration) {
		if *v <= 0 {
			*v = def
		}
	}
	fill(&c.PatchInterval, d.PatchInterval)
	fill(&c.Debounce, d.Debounce)
	fill(&c.MinReloadGap, d.MinReloadGap)
	fill(&c.StructureCheck, d.StructureCheck)
	fill(&c.PollAttached, d.PollAttached)
	fill(&c.PollDetached, d.PollDetached)
	fill(&c.EventsAttached, d.EventsAttached)
	fill(&c.EventsDetached, d.EventsDetached)
	fill(&c.BackoffMin, d.BackoffMin)
	fill(&c.BackoffMax, d.BackoffMax)
	fill(&c.StableAfter, d.StableAfter)
	fill(&c.ListenRetry, d.ListenRetry)
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return c
}

// backoff is the wait between connection attempts: it doubles up to max.
type backoff struct {
	min, max time.Duration
	cur      time.Duration
}

func (b *backoff) next() time.Duration {
	if b.cur == 0 {
		b.cur = b.min
	} else {
		b.cur = min(b.cur*2, b.max)
	}
	return b.cur
}

func (b *backoff) reset() { b.cur = 0 }

// debouncer decides when a reload of a list is due: after a quiet time without
// structure events, at the latest gap after the first of them so a steady
// stream cannot starve it, and never sooner than gap after the last run.
type debouncer struct {
	quiet, gap        time.Duration
	first, last, done time.Time
	pending           bool
}

func (d *debouncer) note(now time.Time) {
	if !d.pending {
		d.first, d.pending = now, true
	}
	d.last = now
}

func (d *debouncer) ran(now time.Time) {
	d.done, d.pending = now, false
}

func (d *debouncer) due() (time.Time, bool) {
	if !d.pending {
		return time.Time{}, false
	}
	t := d.last.Add(d.quiet)
	if latest := d.first.Add(d.gap); latest.Before(t) {
		t = latest
	}
	if earliest := d.done.Add(d.gap); t.Before(earliest) {
		t = earliest
	}
	return t, true
}

// Engine connects a store and a hub to one JDownloader at a time.
type Engine struct {
	store *state.Store
	hub   *sse.Hub
	cfg   Config
	log   *slog.Logger

	attach  chan struct{} // a browser count crossed zero
	resync  chan state.List
	refresh chan struct{} // the status should be read again soon
	bg      sync.WaitGroup

	mu       sync.Mutex
	client   *jd.Client
	gen      uint64 // the number of the current address, grows with each SetURL
	cleared  uint64 // the address the mirror belongs to
	clearCh  chan struct{}
	cancel   context.CancelFunc // ends the current session
	running  bool
	everUp   bool // JDownloader answered since the address was set
	browsers int
	status   state.Status
	unknown  map[string]bool // event ids already logged as unknown
	lostLog  repeatLog       // keeps a down JDownloader from filling the log
}

// New returns an engine. client is nil when no address is set yet: the engine
// then waits for SetURL and reports misconfigured.
func New(client *jd.Client, store *state.Store, hub *sse.Hub, cfg Config) *Engine {
	e := &Engine{
		store: store, hub: hub, cfg: cfg.withDefaults(), attach: make(chan struct{}, 1), resync: make(chan state.List, 8), refresh: make(chan struct{}, 1),
		client: client, clearCh: make(chan struct{}), unknown: map[string]bool{},
	}
	e.log = e.cfg.Logger
	e.status = freshStatus(client)
	return e
}

func freshStatus(c *jd.Client) state.Status {
	st := state.Status{Connection: state.ConnSyncing, Controller: state.NormalizeController(jd.ControllerIdle)}
	st.Downloads.ETA = state.ETAUnknown
	if c != nil {
		st.JD.URL = c.BaseURL()
	}
	return st
}

// Status returns the current status. It is always a consistent copy.
func (e *Engine) Status() state.Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.status
}

// Client returns the client of the current address, nil if none is set.
func (e *Engine) Client() *jd.Client {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.client
}

// Attach counts an open browser. With one open the engine polls faster and asks
// JDownloader for more frequent status events.
func (e *Engine) Attach() {
	e.mu.Lock()
	e.browsers++
	cross := e.browsers == 1
	e.mu.Unlock()
	if cross {
		e.signalAttach()
	}
}

// Detach counts a closed browser.
func (e *Engine) Detach() {
	e.mu.Lock()
	cross := e.browsers == 1
	if e.browsers > 0 {
		e.browsers--
	}
	e.mu.Unlock()
	if cross {
		e.signalAttach()
	}
}

// Resync asks for a reload of one list. The API layer calls it after an action:
// when JDownloader has dropped an entry on its own it answers an action on it
// with success and sends no event, and the mirror would otherwise keep the entry
// until the next safety check.
//
// It never blocks. The reload follows the usual debounce (a short quiet time
// and at least MinReloadGap after the last reload of that list), so a burst of
// actions causes one reload. Up to eight requests wait in a queue, more are
// dropped because a reload is on its way anyway. While no session runs
// (JDownloader is away) the requests wait and the next session serves them, which
// costs one extra reload after the connection is back.
func (e *Engine) Resync(list state.List) {
	select {
	case e.resync <- list:
	default: // a reload is queued anyway
	}
}

// RefreshStatus asks for the status (controller, speed, switches, speed limit,
// captchas, dialogs) to be read from JDownloader again soon, within a few hundred
// milliseconds. The API layer calls it after it changed a switch, the speed limit
// or the controller, so that the browsers see the result without waiting for the
// next poll. It never blocks and does nothing while no session runs.
func (e *Engine) RefreshStatus() {
	select {
	case e.refresh <- struct{}{}:
	default: // a refresh is queued anyway
	}
}

func (e *Engine) signalAttach() {
	select {
	case e.attach <- struct{}{}:
	default:
	}
}

func (e *Engine) attached() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.browsers > 0
}

// SetURL switches to another JDownloader: it stops the current session, empties
// the mirror and connects anew. The address goes through jd.NormalizeURL, an
// invalid one is an error wrapping jd.ErrInvalidURL and changes nothing. When
// SetURL returns the old list is gone.
func (e *Engine) SetURL(raw string) error {
	url, err := jd.NormalizeURL(raw)
	if err != nil {
		return err
	}
	client := jd.New(url, e.cfg.ClientOptions...)

	e.mu.Lock()
	old := e.client
	e.client = client
	e.gen++
	gen := e.gen
	e.everUp = false
	before := e.status
	e.status = freshStatus(client)
	e.announce(before, e.status)
	if e.cancel != nil {
		e.cancel()
	}
	if !e.running {
		// No loop that could still be writing to the mirror.
		e.store.Clear()
		e.cleared = gen
		e.mu.Unlock()
		e.publishStructure()
		closeIdle(old)
		return nil
	}
	// The running loop clears the mirror when the old session has ended.
	for e.running && e.cleared < gen {
		ch := e.clearCh
		e.mu.Unlock()
		<-ch
		e.mu.Lock()
	}
	e.mu.Unlock()
	return nil
}

func closeIdle(c *jd.Client) {
	if c != nil {
		c.CloseIdleConnections()
	}
}

// Run does the work until ctx ends, and returns ctx's error then. It returns
// ErrRunning if the engine runs already.
func (e *Engine) Run(ctx context.Context) error {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return ErrRunning
	}
	e.running = true
	e.mu.Unlock()

	defer func() {
		e.bg.Wait()
		e.mu.Lock()
		e.running = false
		if e.cleared != e.gen { // a SetURL the loop did not get to any more
			e.store.Clear()
			e.cleared = e.gen
		}
		close(e.clearCh) // releases a SetURL that waits for a loop that ended
		e.clearCh = make(chan struct{})
		client := e.client
		e.mu.Unlock()
		closeIdle(client)
	}()

	bo := backoff{min: e.cfg.BackoffMin, max: e.cfg.BackoffMax}
	var last *jd.Client
	for {
		sctx, cancel, client, gen := e.nextSession(ctx)
		if ctx.Err() != nil {
			cancel()
			return ctx.Err()
		}
		if changed := e.acknowledge(gen); changed {
			bo.reset()
		}
		if last != nil && last != client {
			closeIdle(last)
		}
		last = client

		if client == nil {
			e.update(gen, func(st *state.Status) {
				st.Connection, st.ConnectionDetail = state.ConnMisconfigured, state.DetailNoURL
			})
			<-sctx.Done()
			cancel()
			continue
		}

		res := e.session(sctx, client, gen)
		if sctx.Err() != nil { // SetURL or the end of Run
			cancel()
			continue
		}

		stable := !res.connectedAt.IsZero() && time.Since(res.connectedAt) >= e.cfg.StableAfter
		if stable {
			bo.reset()
		}
		e.lost(gen, client, res.err)
		if stable && errors.Is(res.err, errSubscriptionLost) {
			cancel()
			continue // JDownloader is there, no reason to wait
		}
		timer := time.NewTimer(bo.next())
		select {
		case <-timer.C:
		case <-sctx.Done():
			timer.Stop()
		}
		cancel()
	}
}

// nextSession creates the context of the next session. It is registered under
// the lock together with the address it is for, so SetURL always cancels the
// session of the address it replaces.
func (e *Engine) nextSession(ctx context.Context) (context.Context, context.CancelFunc, *jd.Client, uint64) {
	sctx, cancel := context.WithCancel(ctx)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cancel = cancel
	return sctx, cancel, e.client, e.gen
}

// acknowledge empties the mirror if the address changed. The previous session
// has ended by then, so nothing can write old data into the empty mirror.
func (e *Engine) acknowledge(gen uint64) bool {
	e.mu.Lock()
	changed := e.cleared != gen
	if changed {
		e.store.Clear()
		e.cleared = gen
		close(e.clearCh)
		e.clearCh = make(chan struct{})
	}
	e.mu.Unlock()
	if changed {
		e.publishStructure()
	}
	return changed
}

// publish sends one event to the browsers.
func (e *Engine) publish(event string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		e.log.Error("cannot encode an event", "event", event, "err", err)
		return
	}
	e.hub.Publish(sse.Message{Event: event, Data: b})
}

type structurePayload struct {
	List state.List `json:"list"`
}

// publishStructure tells the browsers to load both lists again.
func (e *Engine) publishStructure() {
	e.publish("structure", structurePayload{state.Downloads})
	e.publish("structure", structurePayload{state.Linkgrabber})
}

// flush sends what changed in the mirror, and the totals that follow from it.
func (e *Engine) flush(gen uint64) {
	patches := e.store.Drain()
	for _, p := range patches {
		e.publish("patch", p)
	}
	if len(patches) > 0 {
		e.refreshTotals(gen)
	}
}

// lost handles the end of a session: the mirror goes stale, everything that
// looked alive stops, and the browsers learn why.
func (e *Engine) lost(gen uint64, c *jd.Client, err error) {
	e.store.MarkStale()
	e.flush(gen)
	e.mu.Lock()
	ever := e.everUp
	e.mu.Unlock()
	conn, detail := classify(err, ever, c.BaseURL())
	if e.lostLog.allow(string(conn)+"|"+detail, time.Now()) {
		e.log.Info("connection to JDownloader ended", "url", c.BaseURL(), "connection", conn, "err", err)
	}
	dl, lg := e.store.Totals()
	e.update(gen, func(st *state.Status) {
		st.Connection, st.ConnectionDetail = conn, detail
		st.Speed, st.Captchas, st.Dialogs = 0, 0, 0
		st.Controller = state.NormalizeController(jd.ControllerIdle)
		st.Downloads = dl
		st.Linkgrabber = lg // not crawling any more either
	})
}

// sessionResult is how a session ended.
type sessionResult struct {
	err         error
	connectedAt time.Time // zero if it never got as far as connected
}

// session is one life of a connection: connect, load, then follow the events.
// All its goroutines have ended when it returns.
func (e *Engine) session(parent context.Context, c *jd.Client, gen uint64) (res sessionResult) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	if err := e.reach(ctx, c, gen); err != nil {
		return sessionResult{err: err}
	}
	sub, err := c.Subscribe(ctx, jd.Publishers())
	if err != nil {
		return sessionResult{err: e.judge(ctx, c, err)}
	}
	if sub.ID <= 0 {
		return sessionResult{err: e.judge(ctx, c, errNoSubscription)}
	}
	defer func() { e.leave(parent, c, sub.ID, res.err) }()

	s := &session{
		e: e, c: c, gen: gen, sub: sub.ID, catchUp: make(chan struct{}, 1),
		dl: debouncer{quiet: e.cfg.Debounce, gap: e.cfg.MinReloadGap},
		lg: debouncer{quiet: e.cfg.Debounce, gap: e.cfg.MinReloadGap},
	}
	if err := s.load(ctx); err != nil {
		return sessionResult{err: e.judge(ctx, c, err)}
	}
	connectedAt := time.Now()
	e.update(gen, func(st *state.Status) { st.Connection, st.ConnectionDetail = state.ConnConnected, "" })
	e.log.Info("connected to JDownloader", "url", c.BaseURL())
	e.lostLog.reset()

	events := make(chan []jd.Event)
	failed := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.listen(ctx, events, failed)
	}()
	defer wg.Wait()
	defer cancel()

	return sessionResult{err: s.follow(ctx, events, failed), connectedAt: connectedAt}
}

// judge decides what a failure between the ping and connected means. One bad
// answer proves nothing about who is listening, so it asks again: as long as the
// ping still answers this is a JDownloader with a problem, not another service.
func (e *Engine) judge(ctx context.Context, c *jd.Client, err error) error {
	if isFatal(err) {
		return err
	}
	ok, pingErr := c.Ping(ctx)
	switch {
	case pingErr != nil:
		return pingError(pingErr, true)
	case !ok:
		return errNotJDownloader
	}
	return err
}

// leave says goodbye to a JDownloader that still has our subscription, in the
// background so that it never holds up a switch to another address.
func (e *Engine) leave(parent context.Context, c *jd.Client, sub int64, err error) {
	if parent.Err() == nil && (errors.Is(err, errSubscriptionLost) || jd.IsConnection(err)) {
		return // it is gone already
	}
	e.bg.Add(1)
	go func() {
		defer e.bg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), unsubscribeTimeout)
		defer cancel()
		_ = c.Unsubscribe(ctx, sub)
		if parent.Err() != nil {
			c.CloseIdleConnections()
		}
	}()
}

// reach asks whether JDownloader answers at this address and reads who it is.
func (e *Engine) reach(ctx context.Context, c *jd.Client, gen uint64) error {
	ok, err := c.Ping(ctx)
	if err != nil {
		return pingError(err, e.wasUp())
	}
	if !ok {
		return errNotJDownloader
	}
	e.mu.Lock()
	if gen == e.gen {
		e.everUp = true
	}
	e.mu.Unlock()
	e.update(gen, func(st *state.Status) { st.Connection, st.ConnectionDetail = state.ConnSyncing, "" })

	// A failure here costs nothing but these two values, the known ones stay.
	rev, err := c.CoreRevision(ctx)
	switch {
	case err == nil:
		e.update(gen, func(st *state.Status) { st.JD.Revision = rev })
	case isFatal(err):
		return err
	default:
		e.log.Warn("cannot read the JDownloader revision", "err", err)
	}
	info, err := c.SystemInfos(ctx)
	switch {
	case err == nil:
		e.update(gen, func(st *state.Status) { st.JD.Docker = info.Docker })
	case isFatal(err):
		return err
	default:
		e.log.Warn("cannot read the JDownloader system infos", "err", err)
	}
	return nil
}

func (e *Engine) wasUp() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.everUp
}

// maxLoggedUnknown caps the memory of what was logged already.
const maxLoggedUnknown = 256

// logUnknown logs an event this version does not understand, once per event id.
func (e *Engine) logUnknown(ev jd.Event) {
	key := ev.Publisher + "/" + ev.EventID
	e.mu.Lock()
	seen := e.unknown[key] || len(e.unknown) >= maxLoggedUnknown
	if !seen {
		e.unknown[key] = true
	}
	e.mu.Unlock()
	if !seen {
		e.log.Debug("ignoring an unknown JDownloader event", "publisher", ev.Publisher, "event", ev.EventID)
	}
}

// repeatLogEvery is how often a state that does not change is logged again.
const repeatLogEvery = time.Minute

// repeatLog lets a message through when its key changed, and otherwise once per
// repeatLogEvery. A JDownloader that stays away retries every few seconds, and
// each retry would say the same.
type repeatLog struct {
	mu  sync.Mutex
	key string
	at  time.Time
}

func (r *repeatLog) allow(key string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if key == r.key && !r.at.IsZero() && now.Sub(r.at) < repeatLogEvery {
		return false
	}
	r.key, r.at = key, now
	return true
}

// reset makes the next message count as new, after a connection that worked.
func (r *repeatLog) reset() {
	r.mu.Lock()
	r.key, r.at = "", time.Time{}
	r.mu.Unlock()
}
