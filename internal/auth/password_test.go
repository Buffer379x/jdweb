package auth

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// productionCost switches the Argon2 parameters back to the shipped values.
func productionCost(t *testing.T) {
	t.Helper()
	old := argonCost
	argonCost.time, argonCost.memory = argonTime, argonMemory
	t.Cleanup(func() { argonCost = old })
}

func TestHashPasswordIsArgon2id(t *testing.T) {
	productionCost(t)
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("unexpected format: %s", h)
	}
	if strings.Contains(h, "correct horse") {
		t.Fatal("hash contains the password")
	}
	if !VerifyPassword(h, "correct horse battery") {
		t.Fatal("correct password rejected")
	}
	if VerifyPassword(h, "correct horse batterY") || VerifyPassword(h, "") {
		t.Fatal("wrong password accepted")
	}
	h2, _ := HashPassword("correct horse battery")
	if h == h2 {
		t.Fatal("two hashes of the same password are identical: salt missing")
	}
}

func TestVerifyPasswordRejectsMalformedHashes(t *testing.T) {
	for _, h := range []string{
		"", "plain", "$argon2i$v=19$m=19456,t=2,p=1$AAAA$AAAA",
		"$argon2id$v=19$m=x,t=2,p=1$AAAA$AAAA",
		"$argon2id$v=19$m=19456,t=2,p=1$!!!$AAAA",
		// Parameters that would make verification a denial-of-service lever.
		"$argon2id$v=19$m=4194304,t=2,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
	} {
		if VerifyPassword(h, "x") {
			t.Errorf("accepted %q", h)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	cases := []struct {
		pw   string
		want error
	}{
		{"elevenchars", ErrPasswordTooShort},
		{"twelve chars", nil},
		{strings.Repeat("ä", 11), ErrPasswordTooShort}, // counts characters, not bytes
		{strings.Repeat("ä", 12), nil},
		{strings.Repeat("a", 256), nil},
		{strings.Repeat("a", 257), ErrPasswordTooLong},
		{"", ErrPasswordTooShort},
	}
	for _, c := range cases {
		if err := ValidatePassword(c.pw); !errors.Is(err, c.want) {
			t.Errorf("ValidatePassword(%d runes) = %v, want %v", len([]rune(c.pw)), err, c.want)
		}
	}
}

func TestValidateUsername(t *testing.T) {
	for _, u := range []string{"admin", "Max M.", "ü"} {
		if err := ValidateUsername(u); err != nil {
			t.Errorf("ValidateUsername(%q) = %v", u, err)
		}
	}
	for _, u := range []string{"", "   ", "a\nb", strings.Repeat("x", 65), "a\x00"} {
		if err := ValidateUsername(u); !errors.Is(err, ErrInvalidUsername) {
			t.Errorf("ValidateUsername(%q) = %v, want ErrInvalidUsername", u, err)
		}
	}
}

func TestHashQueueIsBoundedAndHonoursContext(t *testing.T) {
	h, _ := HashPassword("correct horse battery")
	// Occupy every hashing slot so that new callers have to queue.
	for i := 0; i < cap(hashSlots); i++ {
		hashSlots <- struct{}{}
	}
	released := false
	release := func() {
		if !released {
			released = true
			for i := 0; i < cap(hashSlots); i++ {
				<-hashSlots
			}
		}
	}
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	errs := make(chan error, maxHashQueue)
	for i := 0; i < maxHashQueue; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := VerifyPasswordContext(ctx, h, "x")
			errs <- err
		}()
	}
	deadline := time.Now().Add(5 * time.Second)
	for hashPending.Load() < maxHashQueue {
		if time.Now().After(deadline) {
			t.Fatal("queue did not fill")
		}
		time.Sleep(time.Millisecond)
	}
	// The queue is full: further callers fail fast instead of piling up.
	if _, err := VerifyPasswordContext(context.Background(), h, "x"); !errors.Is(err, ErrBusy) {
		t.Fatalf("verify with full queue: %v, want ErrBusy", err)
	}
	if _, err := HashPasswordContext(context.Background(), "x"); !errors.Is(err, ErrBusy) {
		t.Fatalf("hash with full queue: %v, want ErrBusy", err)
	}
	// Cancelling releases the waiting callers without running Argon2.
	cancel()
	wg.Wait()
	close(errs)
	for err := range errs {
		if !errors.Is(err, context.Canceled) {
			t.Errorf("queued caller: %v, want context.Canceled", err)
		}
	}
	if n := hashPending.Load(); n != 0 {
		t.Fatalf("pending = %d after cancel", n)
	}
	release()
	if ok, err := VerifyPasswordContext(context.Background(), h, "correct horse battery"); err != nil || !ok {
		t.Fatalf("verify after the queue drained: %v %v", ok, err)
	}
}

// The memory of a hash goes back to the system at once: a container that
// logged in once must not sit ~35 MiB above its idle size for minutes.
func TestHashPasswordReleasesItsMemory(t *testing.T) {
	productionCost(t)
	for range 2 {
		if _, err := HashPassword("correct horse battery"); err != nil {
			t.Fatal(err)
		}
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	if kept := (m.HeapIdle - m.HeapReleased) >> 20; kept > 8 {
		t.Fatalf("%d MiB of heap are idle but still held after hashing; want them released", kept)
	}
}
