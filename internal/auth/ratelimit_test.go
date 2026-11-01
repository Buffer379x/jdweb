package auth

import (
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

func TestLimiterGlobalBlocksEveryone(t *testing.T) {
	l := newLimiter(newClock().Now)
	// Four failures per address stay below the per-address limit.
	for i := 0; i < globalThreshold; i++ {
		l.fail(string(rune('a' + i/4)))
	}
	if w := l.check("fresh-address"); w == 0 {
		t.Fatal("global limit did not trigger")
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
