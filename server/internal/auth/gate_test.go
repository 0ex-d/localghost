package auth

import (
	"testing"
	"time"
)

// A fake clock so tests are deterministic.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }

func newTestGate() (*Gate, *clock) {
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	g := NewGate(DefaultPolicy(), NewMemoryStore())
	g.now = c.now
	return g, c
}

// A credential whose PIN is "2468".
func testCred(t *testing.T) Credential {
	t.Helper()
	cred, err := NewCredential("2468")
	if err != nil {
		t.Fatal(err)
	}
	return cred
}

func TestCorrectPinPasses(t *testing.T) {
	g, _ := newTestGate()
	cred := testCred(t)
	if err := g.Verify("dev1", "2468", cred); err != nil {
		t.Fatalf("correct PIN should pass, got %v", err)
	}
}

func TestFreeAttemptsAreImmediate(t *testing.T) {
	g, _ := newTestGate()
	cred := testCred(t)
	for i := 0; i < DefaultPolicy().FreeAttempts; i++ {
		if err := g.Verify("dev1", "0000", cred); err != ErrBadPIN {
			t.Fatalf("attempt %d: want ErrBadPIN, got %v", i, err)
		}
	}
}

func TestCooldownAfterFreeAttempts(t *testing.T) {
	g, c := newTestGate()
	cred := testCred(t)
	p := DefaultPolicy()
	for i := 0; i < p.FreeAttempts; i++ {
		g.Verify("dev1", "0000", cred)
	}
	// Next attempt must be refused without counting (KDF skipped).
	if err := g.Verify("dev1", "0000", cred); err != ErrTooSoon {
		t.Fatalf("want ErrTooSoon during cooldown, got %v", err)
	}
	// After the base delay, it is allowed again.
	c.add(p.BaseDelay)
	if err := g.Verify("dev1", "0000", cred); err != ErrBadPIN {
		t.Fatalf("want ErrBadPIN after cooldown, got %v", err)
	}
}

func TestHardLockout(t *testing.T) {
	g, c := newTestGate()
	cred := testCred(t)
	p := DefaultPolicy()
	for g.store.Get("dev1").Failed < p.MaxAttempts {
		if err := g.Verify("dev1", "0000", cred); err == ErrTooSoon {
			c.add(g.RetryAfter("dev1"))
		}
	}
	// Locked: even the correct PIN is refused.
	if err := g.Verify("dev1", "2468", cred); err != ErrLockedOut {
		t.Fatalf("want ErrLockedOut, got %v", err)
	}
	// After lockout expires, the correct PIN works and resets state.
	c.add(p.LockoutFor + time.Second)
	if err := g.Verify("dev1", "2468", cred); err != nil {
		t.Fatalf("want success after lockout, got %v", err)
	}
	if g.store.Get("dev1").Failed != 0 {
		t.Fatal("success should reset failure count")
	}
}

func TestWaitIsCapped(t *testing.T) {
	g, _ := newTestGate()
	p := DefaultPolicy()
	if w := g.requiredWait(1000); w != p.MaxDelay {
		t.Fatalf("want capped at %v, got %v", p.MaxDelay, w)
	}
}
