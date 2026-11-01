package auth

import (
	"sort"
	"sync"
	"time"
)

// Failed logins are throttled per source address and globally. From the fifth
// failure the wait doubles with every further failure up to maxDelay. The
// global limit is higher so that a few mistyped passwords on several devices
// do not lock everyone out.
const (
	ipThreshold     = 5
	globalThreshold = 20
	ipBaseDelay     = 30 * time.Second
	maxDelay        = 15 * time.Minute
	forgetAfter     = time.Hour // failures older than this no longer count
	maxEntries      = 10_000
)

type attempts struct {
	fails int
	last  time.Time
	until time.Time
}

// limiter keeps its state in memory only. It is not persisted on purpose.
type limiter struct {
	mu     sync.Mutex
	now    func() time.Time
	ips    map[string]*attempts
	global attempts
}

func newLimiter(now func() time.Time) *limiter {
	return &limiter{now: now, ips: map[string]*attempts{}}
}

func (a *attempts) record(now time.Time, threshold int) {
	if now.Sub(a.last) > forgetAfter {
		a.fails = 0
	}
	a.fails++
	a.last = now
	if a.fails >= threshold {
		shift := min(a.fails-threshold, 16)
		a.until = now.Add(min(ipBaseDelay<<shift, maxDelay))
	}
}

func (a *attempts) wait(now time.Time) time.Duration {
	if now.Before(a.until) {
		return a.until.Sub(now)
	}
	return 0
}

// check returns how long ip must still wait; zero means go ahead.
func (l *limiter) check(ip string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	w := l.global.wait(now)
	if a := l.ips[ip]; a != nil {
		w = max(w, a.wait(now))
	}
	return w
}

// fail records a failed attempt from ip.
func (l *limiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	a := l.ips[ip]
	if a == nil {
		l.makeRoom(now)
		a = &attempts{}
		l.ips[ip] = a
	}
	a.record(now, ipThreshold)
	l.global.record(now, globalThreshold)
}

// success clears the counters of ip and the global counter.
func (l *limiter) success(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.ips, ip)
	l.global = attempts{}
}

func (l *limiter) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.ips)
}

// makeRoom keeps the map below maxEntries: first by dropping stale entries,
// then by dropping the older half.
func (l *limiter) makeRoom(now time.Time) {
	if len(l.ips) < maxEntries {
		return
	}
	for k, a := range l.ips {
		if now.Sub(a.last) > forgetAfter && now.After(a.until) {
			delete(l.ips, k)
		}
	}
	if len(l.ips) < maxEntries {
		return
	}
	keys := make([]string, 0, len(l.ips))
	for k := range l.ips {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return l.ips[keys[i]].last.Before(l.ips[keys[j]].last) })
	for _, k := range keys[:len(keys)/2] {
		delete(l.ips, k)
	}
}
