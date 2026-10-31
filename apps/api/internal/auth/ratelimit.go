package auth

import (
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/time/rate"
)

// Rule is a token bucket: Limit events per Window (burst = Limit, refill Limit/Window).
type Rule struct {
	Limit  int
	Window time.Duration
}

// Starting limits from docs/AUTH.md §5.
var (
	RuleLoginEmail = Rule{Limit: 5, Window: 15 * time.Minute}  // failed logins per email
	RuleLoginIP    = Rule{Limit: 20, Window: 15 * time.Minute} // login attempts per IP
	RuleRegisterIP = Rule{Limit: 5, Window: time.Hour}         // registrations per IP
	RuleRefresh    = Rule{Limit: 60, Window: time.Hour}        // refreshes per session
)

// KeyedLimiter is an in-memory set of token buckets, one per key. Safe for concurrent use.
// Single-instance only (Q-10: Postgres-backed limits once there are several API instances).
type KeyedLimiter struct {
	rule  Rule
	now   func() time.Time
	mu    sync.Mutex
	items map[string]*bucket
}

type bucket struct {
	lim      *rate.Limiter
	lastSeen time.Time
}

// NewKeyedLimiter returns a limiter for rule using the clock now (nil = time.Now).
func NewKeyedLimiter(rule Rule, now func() time.Time) *KeyedLimiter {
	if now == nil {
		now = time.Now
	}
	return &KeyedLimiter{rule: rule, now: now, items: map[string]*bucket{}}
}

func (k *KeyedLimiter) get(key string, now time.Time) *bucket {
	b, ok := k.items[key]
	if !ok {
		every := k.rule.Window / time.Duration(k.rule.Limit)
		b = &bucket{lim: rate.NewLimiter(rate.Every(every), k.rule.Limit)}
		k.items[key] = b
	}
	b.lastSeen = now
	return b
}

// Allow consumes one token for key. When the bucket is empty it consumes nothing and returns how long until
// one token is available.
func (k *KeyedLimiter) Allow(key string) (ok bool, retryAfter time.Duration) {
	now := k.now()
	k.mu.Lock()
	defer k.mu.Unlock()
	r := k.get(key, now).lim.ReserveN(now, 1)
	if !r.OK() {
		return false, k.rule.Window
	}
	if d := r.DelayFrom(now); d > 0 {
		r.CancelAt(now)
		return false, d
	}
	return true, 0
}

// Check reports whether one token is available for key without consuming it.
func (k *KeyedLimiter) Check(key string) (ok bool, retryAfter time.Duration) {
	now := k.now()
	k.mu.Lock()
	defer k.mu.Unlock()
	b, exists := k.items[key]
	if !exists {
		return true, 0 // a fresh bucket is full
	}
	r := b.lim.ReserveN(now, 1)
	if !r.OK() {
		return false, k.rule.Window
	}
	d := r.DelayFrom(now)
	r.CancelAt(now) // peek only: give the token back
	if d > 0 {
		return false, d
	}
	return true, 0
}

// sweep drops buckets idle for at least one window: by then they have fully refilled, so dropping them is
// indistinguishable from keeping them.
func (k *KeyedLimiter) sweep() {
	now := k.now()
	k.mu.Lock()
	defer k.mu.Unlock()
	for key, b := range k.items {
		if now.Sub(b.lastSeen) >= k.rule.Window {
			delete(k.items, key)
		}
	}
}

// Len is the number of tracked keys.
func (k *KeyedLimiter) Len() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.items)
}

// LimitsConfig configures Limits. Zero values take the AUTH.md §5 defaults.
type LimitsConfig struct {
	Now             func() time.Time // injectable clock; default time.Now
	JanitorInterval time.Duration    // how often idle keys are dropped; default 1 min; < 0 disables the janitor
	LoginEmail      Rule
	LoginIP         Rule
	Register        Rule
	Refresh         Rule
}

// Limits holds the auth endpoint limiters (docs/AUTH.md §5). Keys are never logged.
type Limits struct {
	loginEmail *KeyedLimiter
	loginIP    *KeyedLimiter
	register   *KeyedLimiter
	refresh    *KeyedLimiter

	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

// NewLimits builds the limiters and starts the janitor goroutine. Call Close to stop it.
func NewLimits(cfg LimitsConfig) *Limits {
	def := func(r, d Rule) Rule {
		if r.Limit <= 0 || r.Window <= 0 {
			return d
		}
		return r
	}
	l := &Limits{
		loginEmail: NewKeyedLimiter(def(cfg.LoginEmail, RuleLoginEmail), cfg.Now),
		loginIP:    NewKeyedLimiter(def(cfg.LoginIP, RuleLoginIP), cfg.Now),
		register:   NewKeyedLimiter(def(cfg.Register, RuleRegisterIP), cfg.Now),
		refresh:    NewKeyedLimiter(def(cfg.Refresh, RuleRefresh), cfg.Now),
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
	}
	interval := cfg.JanitorInterval
	if interval == 0 {
		interval = time.Minute
	}
	if interval < 0 {
		close(l.done)
		return l
	}
	go l.janitor(interval)
	return l
}

func (l *Limits) janitor(interval time.Duration) {
	defer close(l.done)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-l.stop:
			return
		case <-t.C:
			l.Sweep()
		}
	}
}

// Sweep drops idle keys now (the janitor calls it periodically).
func (l *Limits) Sweep() {
	for _, k := range []*KeyedLimiter{l.loginEmail, l.loginIP, l.register, l.refresh} {
		k.sweep()
	}
}

// Close stops the janitor and waits for it. Safe to call more than once.
func (l *Limits) Close() {
	l.closeOnce.Do(func() { close(l.stop) })
	<-l.done
}

func emailKey(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

// LoginAttempt is called before verifying credentials. Every attempt consumes from the IP bucket; the email
// bucket (which counts failures, see LoginFailed) is only checked. On deny the caller answers 429 with
// Retry-After and must not verify the password.
func (l *Limits) LoginAttempt(email, ip string) (ok bool, retryAfter time.Duration) {
	ipOK, ipWait := l.loginIP.Allow(ip)
	emOK, emWait := l.loginEmail.Check(emailKey(email))
	if ipOK && emOK {
		return true, 0
	}
	return false, max(ipWait, emWait)
}

// LoginFailed records a failed login (wrong password or unknown email) against the email bucket.
func (l *Limits) LoginFailed(email string) {
	l.loginEmail.Allow(emailKey(email))
}

// Register limits registrations per client IP.
func (l *Limits) Register(ip string) (ok bool, retryAfter time.Duration) {
	return l.register.Allow(ip)
}

// Refresh limits token refreshes per session.
func (l *Limits) Refresh(sessionID uuid.UUID) (ok bool, retryAfter time.Duration) {
	return l.refresh.Allow(sessionID.String())
}
