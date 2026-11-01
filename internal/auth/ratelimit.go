package auth

import (
	"net/netip"
	"sort"
	"sync"
	"time"
)

// Failed logins are limited in two ways.
//
// Per source address the limiter blocks: from the fifth failure the wait is
// 30 s and doubles with every further failure up to 15 min. IPv6 addresses
// count per /64 network.
//
// Globally it only throttles: once many addresses have failed, every login
// attempt is delayed by up to maxThrottle, but it is still evaluated. The
// correct password therefore always works, so someone on the LAN cannot lock
// the owner out by spraying wrong passwords from many addresses.
//
// An attempt is reserved before the slow password check and refunded when it
// turns out not to be a failure, so parallel requests cannot overtake the cap.
const (
	ipThreshold     = 5
	globalThreshold = 20
	ipBaseDelay     = 30 * time.Second
	maxDelay        = 15 * time.Minute
	throttleStep    = 250 * time.Millisecond
	maxThrottle     = 5 * time.Second
	forgetAfter     = time.Hour // failures older than this no longer count
	maxEntries      = 10_000
)

type attempts struct {
	fails int
	last  time.Time
	until time.Time // blocked until; per-address entries only
}

func blockFor(fails int) time.Duration {
	shift := min(fails-ipThreshold, 16)
	return min(ipBaseDelay<<shift, maxDelay)
}

func (a *attempts) record(now time.Time) {
	if now.Sub(a.last) > forgetAfter {
		a.fails = 0
	}
	a.fails++
	a.last = now
}

func (a *attempts) refund() {
	if a.fails > 0 {
		a.fails--
	}
}

// settle recomputes the block from the failure count.
func (a *attempts) settle() {
	if a.fails >= ipThreshold {
		a.until = a.last.Add(blockFor(a.fails))
	} else {
		a.until = time.Time{}
	}
}

func (a *attempts) wait(now time.Time) time.Duration {
	if now.Before(a.until) {
		return a.until.Sub(now)
	}
	return 0
}

// limiterKey maps an address to its limiter key: IPv6 addresses to their /64
// network, IPv4-mapped IPv6 addresses to the IPv4 address, anything else
// unchanged.
func limiterKey(ip string) string {
	if ip == "" {
		return "unknown"
	}
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	a = a.WithZone("").Unmap()
	if a.Is6() {
		return netip.PrefixFrom(a, 64).Masked().String()
	}
	return a.String()
}

// limiter keeps its state in memory only. It is not persisted on purpose.
type limiter struct {
	mu        sync.Mutex
	now       func() time.Time
	ips       map[string]*attempts
	global    attempts
	useGlobal bool
}

// newLimiter limits per address and tracks global pressure.
func newLimiter(now func() time.Time) *limiter {
	return &limiter{now: now, ips: map[string]*attempts{}, useGlobal: true}
}

// newIPLimiter limits per address only.
func newIPLimiter(now func() time.Time) *limiter {
	return &limiter{now: now, ips: map[string]*attempts{}}
}

// check returns how long the address must still wait; zero means go ahead.
func (l *limiter) check(ip string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.checkLocked(ip, l.now())
}

func (l *limiter) checkLocked(ip string, now time.Time) time.Duration {
	if a := l.ips[ip]; a != nil {
		return a.wait(now)
	}
	return 0
}

// pressure is the delay to put before every attempt while many addresses fail.
func (l *limiter) pressure() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.pressureLocked(l.now())
}

func (l *limiter) pressureLocked(now time.Time) time.Duration {
	if !l.useGlobal || now.Sub(l.global.last) > forgetAfter || l.global.fails < globalThreshold {
		return 0
	}
	return min(throttleStep*time.Duration(l.global.fails-globalThreshold+1), maxThrottle)
}

// recordLocked counts an attempt. It reports false when the table is full of
// active blocks and the address cannot be tracked individually.
func (l *limiter) recordLocked(ip string, now time.Time) bool {
	if l.useGlobal {
		l.global.record(now)
	}
	a := l.ips[ip]
	if a == nil {
		l.makeRoom(now)
		if len(l.ips) >= maxEntries {
			return false
		}
		a = &attempts{}
		l.ips[ip] = a
	}
	a.record(now)
	a.settle()
	return true
}

// fail records a failed attempt that was not reserved.
func (l *limiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.recordLocked(ip, l.now())
}

// reserve counts an attempt before it is evaluated. ok is false while the
// address is blocked. throttle is the delay to apply before evaluating. Call
// refund if the attempt turns out not to be a failure, success if it worked.
func (l *limiter) reserve(ip string) (wait, throttle time.Duration, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if w := l.checkLocked(ip, now); w > 0 {
		return w, 0, false
	}
	throttle = l.pressureLocked(now)
	if !l.recordLocked(ip, now) {
		throttle = maxThrottle // untracked address: nothing else limits it
	}
	return 0, throttle, true
}

// refund takes back a reserved attempt that was not a failure.
func (l *limiter) refund(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.useGlobal {
		l.global.refund()
	}
	if a := l.ips[ip]; a != nil {
		a.refund()
		if a.fails == 0 {
			delete(l.ips, ip)
		} else {
			a.settle()
		}
	}
}

// success clears the address. The reserved attempt is also taken back from
// the global count, which is otherwise kept so an owner's login cannot wipe
// the pressure an attacker built up.
func (l *limiter) success(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.ips, ip)
	if l.useGlobal {
		l.global.refund()
	}
}

func (l *limiter) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.ips)
}

// makeRoom keeps the map below maxEntries without ever dropping an active
// block: first stale entries, then the older half of the unblocked ones.
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
	var keys []string
	for k, a := range l.ips {
		if !now.Before(a.until) {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return l.ips[keys[i]].last.Before(l.ips[keys[j]].last) })
	for _, k := range keys[:(len(keys)+1)/2] {
		delete(l.ips, k)
	}
}
