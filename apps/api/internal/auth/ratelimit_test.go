package auth

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type lockedClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *lockedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *lockedClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newTestLimits(t *testing.T) (*Limits, *lockedClock) {
	t.Helper()
	clk := &lockedClock{t: t0}
	l := NewLimits(LimitsConfig{Now: clk.Now, JanitorInterval: -1})
	t.Cleanup(l.Close)
	return l, clk
}

func TestKeyedLimiterBucket(t *testing.T) {
	tests := []struct {
		rule Rule
	}{
		{RuleLoginEmail}, {RuleLoginIP}, {RuleRegisterIP}, {RuleRefresh},
	}
	for _, tc := range tests {
		clk := &lockedClock{t: t0}
		k := NewKeyedLimiter(tc.rule, clk.Now)
		for i := 0; i < tc.rule.Limit; i++ {
			if ok, _ := k.Allow("key"); !ok {
				t.Fatalf("%+v: attempt %d denied", tc.rule, i+1)
			}
		}
		ok, wait := k.Allow("key")
		if ok || wait <= 0 {
			t.Fatalf("%+v: over limit = %v,%v; want denied with retryAfter > 0", tc.rule, ok, wait)
		}
		perToken := tc.rule.Window / time.Duration(tc.rule.Limit)
		if wait > perToken {
			t.Fatalf("%+v: retryAfter %v > one refill interval %v", tc.rule, wait, perToken)
		}
		// A denied call must not consume: the wait does not grow.
		if _, wait2 := k.Allow("key"); wait2 != wait {
			t.Fatalf("%+v: denied call changed retryAfter %v → %v", tc.rule, wait, wait2)
		}
		// Other keys are independent.
		if ok, _ := k.Allow("other"); !ok {
			t.Fatalf("%+v: independent key denied", tc.rule)
		}
		// After retryAfter exactly one more is allowed.
		clk.Advance(wait)
		if ok, _ := k.Allow("key"); !ok {
			t.Fatalf("%+v: denied after retryAfter", tc.rule)
		}
		if ok, _ := k.Allow("key"); ok {
			t.Fatalf("%+v: second allowed right after one refill", tc.rule)
		}
		// After a full window the whole burst is back.
		clk.Advance(tc.rule.Window)
		for i := 0; i < tc.rule.Limit; i++ {
			if ok, _ := k.Allow("key"); !ok {
				t.Fatalf("%+v: after window, attempt %d denied", tc.rule, i+1)
			}
		}
	}
}

func TestKeyedLimiterCheckDoesNotConsume(t *testing.T) {
	clk := &lockedClock{t: t0}
	k := NewKeyedLimiter(Rule{Limit: 2, Window: time.Minute}, clk.Now)
	for i := 0; i < 10; i++ {
		if ok, _ := k.Check("a"); !ok {
			t.Fatal("Check denied on a fresh key")
		}
	}
	if k.Len() != 0 {
		t.Fatal("Check on an unknown key must not allocate a bucket")
	}
	k.Allow("a")
	k.Allow("a")
	for i := 0; i < 3; i++ {
		if ok, wait := k.Check("a"); ok || wait <= 0 {
			t.Fatalf("Check on empty bucket = %v,%v", ok, wait)
		}
	}
	clk.Advance(30 * time.Second)
	if ok, _ := k.Check("a"); !ok {
		t.Fatal("Check denied after refill")
	}
	if ok, _ := k.Check("a"); !ok {
		t.Fatal("Check consumed the token")
	}
}

func TestLoginLimits(t *testing.T) {
	l, clk := newTestLimits(t)

	// The email bucket counts failures (LoginFailed); LoginAttempt only checks it.
	for i := 0; i < 5; i++ {
		if ok, _ := l.LoginAttempt("User@Example.com", "10.0.0.1"); !ok {
			t.Fatalf("attempt %d denied", i+1)
		}
		l.LoginFailed("user@example.COM ") // case/space-insensitive key
	}
	ok, wait := l.LoginAttempt("user@example.com", "10.0.0.2") // different IP, same email
	if ok || wait <= 0 {
		t.Fatalf("6th attempt after 5 failures = %v,%v; want 429", ok, wait)
	}
	if wait > 3*time.Minute { // 15 min / 5 = one token every 3 min
		t.Fatalf("retryAfter %v too long", wait)
	}
	// Other emails are unaffected.
	if ok, _ := l.LoginAttempt("other@example.com", "10.0.0.3"); !ok {
		t.Fatal("other email denied")
	}
	// Reset after the window.
	clk.Advance(15 * time.Minute)
	if ok, _ := l.LoginAttempt("user@example.com", "10.0.0.4"); !ok {
		t.Fatal("email still blocked after the window")
	}
}

func TestLoginIPLimit(t *testing.T) {
	l, clk := newTestLimits(t)
	for i := 0; i < 20; i++ {
		if ok, _ := l.LoginAttempt("u"+uuid.NewString()+"@x.com", "203.0.113.9"); !ok {
			t.Fatalf("IP attempt %d denied", i+1)
		}
	}
	ok, wait := l.LoginAttempt("fresh@x.com", "203.0.113.9")
	if ok || wait <= 0 || wait > 45*time.Second { // 15 min / 20 = 45 s
		t.Fatalf("21st IP attempt = %v,%v", ok, wait)
	}
	if ok, _ := l.LoginAttempt("fresh@x.com", "198.51.100.1"); !ok {
		t.Fatal("other IP denied")
	}
	clk.Advance(15 * time.Minute)
	if ok, _ := l.LoginAttempt("fresh@x.com", "203.0.113.9"); !ok {
		t.Fatal("IP still blocked after the window")
	}
}

func TestRegisterAndRefreshLimits(t *testing.T) {
	l, clk := newTestLimits(t)
	for i := 0; i < 5; i++ {
		if ok, _ := l.Register("192.0.2.1"); !ok {
			t.Fatalf("register %d denied", i+1)
		}
	}
	if ok, wait := l.Register("192.0.2.1"); ok || wait <= 0 || wait > 12*time.Minute {
		t.Fatalf("6th register = %v,%v", ok, wait)
	}

	sid := uuid.New()
	for i := 0; i < 60; i++ {
		if ok, _ := l.Refresh(sid); !ok {
			t.Fatalf("refresh %d denied", i+1)
		}
	}
	if ok, wait := l.Refresh(sid); ok || wait <= 0 || wait > time.Minute {
		t.Fatalf("61st refresh = %v,%v", ok, wait)
	}
	if ok, _ := l.Refresh(uuid.New()); !ok {
		t.Fatal("other session denied")
	}
	clk.Advance(time.Hour)
	if ok, _ := l.Register("192.0.2.1"); !ok {
		t.Fatal("register still blocked after an hour")
	}
	if ok, _ := l.Refresh(sid); !ok {
		t.Fatal("refresh still blocked after an hour")
	}
}

func TestSweepDropsIdleKeys(t *testing.T) {
	l, clk := newTestLimits(t)
	l.Register("192.0.2.1")
	l.LoginFailed("a@x.com")
	clk.Advance(30 * time.Minute)
	l.Register("192.0.2.2")
	l.Sweep()
	if n := l.loginEmail.Len(); n != 0 {
		t.Fatalf("email keys after sweep = %d, want 0 (idle ≥ 15 min)", n)
	}
	if n := l.register.Len(); n != 2 {
		t.Fatalf("register keys after sweep = %d, want 2 (window is 1 h)", n)
	}
	clk.Advance(time.Hour)
	l.Sweep()
	if n := l.register.Len(); n != 0 {
		t.Fatalf("register keys after 1.5 h = %d, want 0", n)
	}
}

func TestJanitorRunsAndCloses(t *testing.T) {
	clk := &lockedClock{t: t0}
	l := NewLimits(LimitsConfig{Now: clk.Now, JanitorInterval: 5 * time.Millisecond})
	l.Register("192.0.2.1")
	clk.Advance(2 * time.Hour)
	deadline := time.Now().Add(2 * time.Second)
	for l.register.Len() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("janitor did not drop the idle key")
		}
		time.Sleep(5 * time.Millisecond)
	}
	l.Close()
	l.Close() // idempotent
}

func TestLimitsConcurrent(t *testing.T) {
	l, _ := newTestLimits(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _ := l.Register("192.0.2.50"); ok {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if allowed != 5 {
		t.Fatalf("allowed %d concurrent registers, want exactly 5", allowed)
	}
}
