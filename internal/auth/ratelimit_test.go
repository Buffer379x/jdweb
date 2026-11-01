package auth

import (
	"fmt"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time          { return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

func newClock() *fakeClock { return &fakeClock{t: time.Unix(1_800_000_000, 0)} }

func TestLimiterBlocksFromFifthFailureWithGrowingWait(t *testing.T) {
	clk := newClock()
	l := newLimiter(clk.Now)
	for i := 1; i <= 4; i++ {
		if w := l.check("1.1.1.1"); w != 0 {
			t.Fatalf("blocked before failure %d: %v", i, w)
		}
		l.fail("1.1.1.1")
	}
	if w := l.check("1.1.1.1"); w != 0 {
		t.Fatalf("blocked after 4 failures: %v", w)
	}
	l.fail("1.1.1.1") // fifth
	w5 := l.check("1.1.1.1")
	if w5 != ipBaseDelay {
		t.Fatalf("wait after 5th failure = %v, want %v", w5, ipBaseDelay)
	}
	clk.Advance(w5 - time.Second)
	if w := l.check("1.1.1.1"); w != time.Second {
		t.Fatalf("remaining wait = %v, want 1s", w)
	}
	clk.Advance(time.Second)
	if w := l.check("1.1.1.1"); w != 0 {
		t.Fatalf("still blocked after the wait: %v", w)
	}
	l.fail("1.1.1.1") // sixth
	if w := l.check("1.1.1.1"); w != 2*ipBaseDelay {
		t.Fatalf("wait after 6th failure = %v, want %v", w, 2*ipBaseDelay)
	}
	clk.Advance(2 * ipBaseDelay)
	l.fail("1.1.1.1") // seventh
	if w := l.check("1.1.1.1"); w != 4*ipBaseDelay {
		t.Fatalf("wait after 7th failure = %v, want %v", w, 4*ipBaseDelay)
	}
}

func TestLimiterWaitIsCapped(t *testing.T) {
	clk := newClock()
	l := newLimiter(clk.Now)
	for i := 0; i < 40; i++ {
		l.fail("a")
		clk.Advance(maxDelay + time.Second)
		if l.check("a") != 0 {
			t.Fatal("blocked after the wait elapsed")
		}
	}
	l.fail("a")
	if w := l.check("a"); w > maxDelay {
		t.Fatalf("wait %v exceeds cap %v", w, maxDelay)
	}
}

func TestLimiterIsPerAddress(t *testing.T) {
	l := newLimiter(newClock().Now)
	for i := 0; i < 5; i++ {
		l.fail("1.1.1.1")
	}
	if l.check("1.1.1.1") == 0 {
		t.Fatal("offender not blocked")
	}
	if w := l.check("2.2.2.2"); w != 0 {
		t.Fatalf("other address blocked: %v", w)
	}
}

func TestLimiterGlobalOnlyThrottles(t *testing.T) {
	clk := newClock()
	l := newLimiter(clk.Now)
	// Four failures per address stay below the per-address limit.
	for i := 0; i < globalThreshold-1; i++ {
		l.fail(fmt.Sprintf("a%d", i/4))
	}
	if p := l.pressure(); p != 0 {
		t.Fatalf("pressure below the threshold: %v", p)
	}
	l.fail("a-last")
	p1 := l.pressure()
	if p1 <= 0 || p1 > maxThrottle {
		t.Fatalf("pressure at threshold = %v", p1)
	}
	if w := l.check("fresh-address"); w != 0 {
		t.Fatalf("global pressure blocks a fresh address: %v", w)
	}
	l.fail("a-more")
	if p2 := l.pressure(); p2 <= p1 {
		t.Fatalf("pressure does not grow: %v -> %v", p1, p2)
	}
	for i := 0; i < 500; i++ {
		l.fail(fmt.Sprintf("flood%d", i))
	}
	if p := l.pressure(); p != maxThrottle {
		t.Fatalf("pressure = %v, want cap %v", p, maxThrottle)
	}
	if w := l.check("fresh-address"); w != 0 {
		t.Fatalf("fresh address blocked under flood: %v", w)
	}
	clk.Advance(forgetAfter + time.Second)
	if p := l.pressure(); p != 0 {
		t.Fatalf("pressure after a quiet hour: %v", p)
	}
}

func TestIPOnlyLimiterHasNoPressure(t *testing.T) {
	l := newIPLimiter(newClock().Now)
	for i := 0; i < 100; i++ {
		l.fail(fmt.Sprintf("a%d", i))
	}
	if p := l.pressure(); p != 0 {
		t.Fatalf("pressure = %v", p)
	}
}

func TestLimiterKeyAggregatesIPv6To64(t *testing.T) {
	same := [][2]string{
		{"2001:db8:1:2::1", "2001:db8:1:2:ffff:ffff:ffff:ffff"},
		{"::ffff:203.0.113.9", "203.0.113.9"},
	}
	for _, p := range same {
		if limiterKey(p[0]) != limiterKey(p[1]) {
			t.Errorf("%s and %s should share a key", p[0], p[1])
		}
	}
	if limiterKey("2001:db8:1:2::1") == limiterKey("2001:db8:1:3::1") {
		t.Error("different /64 networks share a key")
	}
	if limiterKey("203.0.113.9") == limiterKey("203.0.113.10") {
		t.Error("IPv4 addresses must stay separate")
	}
	for _, in := range []string{"", "garbage"} {
		if limiterKey(in) == "" {
			t.Errorf("limiterKey(%q) is empty", in)
		}
	}
}

func TestLimiterReserveCountsInFlightAttempts(t *testing.T) {
	l := newLimiter(newClock().Now)
	for i := 0; i < 5; i++ {
		if _, _, ok := l.reserve("a"); !ok {
			t.Fatalf("reservation %d refused", i+1)
		}
	}
	wait, _, ok := l.reserve("a")
	if ok || wait != ipBaseDelay {
		t.Fatalf("sixth reservation: ok=%v wait=%v", ok, wait)
	}
	// Refunding takes the attempt back, including the block it caused.
	l.refund("a")
	if w := l.check("a"); w != 0 {
		t.Fatalf("still blocked after refund: %v", w)
	}
	if _, _, ok := l.reserve("a"); !ok {
		t.Fatal("reservation refused after refund")
	}
	// A success clears the address.
	l.success("a")
	for i := 0; i < 5; i++ {
		if _, _, ok := l.reserve("a"); !ok {
			t.Fatalf("reservation %d after success refused", i+1)
		}
	}
}

func TestLimiterNeverEvictsActiveBlocks(t *testing.T) {
	clk := newClock()
	l := newLimiter(clk.Now)
	for i := 0; i < maxEntries; i++ {
		k := fmt.Sprintf("blocked%d", i)
		for j := 0; j < ipThreshold; j++ {
			l.fail(k)
		}
	}
	if n := l.size(); n != maxEntries {
		t.Fatalf("entries = %d", n)
	}
	// A new address cannot be tracked now. It must not push out a block ...
	wait, throttle, ok := l.reserve("newcomer")
	if !ok || wait != 0 {
		t.Fatalf("newcomer: ok=%v wait=%v", ok, wait)
	}
	// ... but it pays the full throttle, since nothing limits it individually.
	if throttle != maxThrottle {
		t.Fatalf("throttle = %v, want %v", throttle, maxThrottle)
	}
	if n := l.size(); n != maxEntries {
		t.Fatalf("entries = %d after newcomer", n)
	}
	for _, i := range []int{0, maxEntries / 2, maxEntries - 1} {
		if l.check(fmt.Sprintf("blocked%d", i)) == 0 {
			t.Fatalf("block %d was evicted", i)
		}
	}
	// Once blocks run out the table makes room again.
	clk.Advance(maxDelay + time.Second)
	l.reserve("newcomer")
	if n := l.size(); n >= maxEntries {
		t.Fatalf("entries = %d: expired blocks were not evicted", n)
	}
	if w := l.check("newcomer"); w != 0 {
		t.Fatalf("newcomer blocked: %v", w)
	}
}

func TestLimiterSuccessResets(t *testing.T) {
	clk := newClock()
	l := newLimiter(clk.Now)
	for i := 0; i < 5; i++ {
		l.fail("a")
	}
	clk.Advance(time.Minute)
	l.success("a")
	l.fail("a")
	if w := l.check("a"); w != 0 {
		t.Fatalf("counter not reset by success: %v", w)
	}
}

func TestLimiterForgetsAfterQuietPeriod(t *testing.T) {
	clk := newClock()
	l := newLimiter(clk.Now)
	for i := 0; i < 5; i++ {
		l.fail("a")
	}
	clk.Advance(forgetAfter + time.Second)
	l.fail("a")
	if w := l.check("a"); w != 0 {
		t.Fatalf("old failures still counted: %v", w)
	}
	if n := l.size(); n != 1 {
		t.Fatalf("entries = %d, want 1", n)
	}
}

func TestLimiterMemoryIsBounded(t *testing.T) {
	clk := newClock()
	l := newLimiter(clk.Now)
	for i := 0; i < maxEntries*3; i++ {
		l.fail(string(rune(0x100+i)) + "x")
		if i%1000 == 0 {
			clk.Advance(time.Minute)
		}
	}
	if n := l.size(); n > maxEntries {
		t.Fatalf("entries = %d, cap %d", n, maxEntries)
	}
}
