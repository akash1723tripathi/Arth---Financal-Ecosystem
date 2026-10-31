// Package config reads the API/worker configuration from environment variables and validates it at boot.
// Error messages name the variable but never print secret values.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// MinSigningKeyBytes is the minimum length of AUTH_ACCESS_TOKEN_SIGNING_KEY.
const MinSigningKeyBytes = 32

// Config is the validated runtime configuration.
type Config struct {
	AppEnv                string // local | test | staging | production
	Port                  int
	LogLevel              slog.Level
	DatabaseURL           string
	MigrationsDatabaseURL string // falls back to DatabaseURL when unset

	AccessTokenSigningKey []byte
	AccessTokenTTL        time.Duration
	RefreshTokenTTL       time.Duration
	SessionMaxAge         time.Duration

	CORSAllowedOrigins []string
	AppPublicURL       string

	AIDriver      string // fake | none
	EmailDriver   string // log | none
	StorageDriver string // none | memory

	ShutdownTimeout time.Duration
}

// IsLocal reports whether the app runs on a developer machine.
func (c Config) IsLocal() bool { return c.AppEnv == "local" }

// Addr is the HTTP listen address.
func (c Config) Addr() string { return ":" + strconv.Itoa(c.Port) }

// LookupFunc matches os.LookupEnv; tests pass a map-backed one.
type LookupFunc func(key string) (string, bool)

// Load reads and validates the configuration from the process environment.
func Load() (Config, error) { return LoadFrom(os.LookupEnv) }

// LoadFrom reads and validates the configuration using lookup. All problems are reported together.
func LoadFrom(lookup LookupFunc) (Config, error) {
	p := parser{lookup: lookup}
	c := Config{
		AppEnv:                p.oneOf("APP_ENV", "local", "local", "test", "staging", "production"),
		Port:                  p.port("PORT", 8080),
		LogLevel:              p.logLevel("LOG_LEVEL", slog.LevelInfo),
		DatabaseURL:           p.dbURL("DATABASE_URL", true),
		MigrationsDatabaseURL: p.dbURL("MIGRATIONS_DATABASE_URL", false),
		AccessTokenSigningKey: p.signingKey("AUTH_ACCESS_TOKEN_SIGNING_KEY"),
		AccessTokenTTL:        p.duration("AUTH_ACCESS_TOKEN_TTL", 15*time.Minute),
		RefreshTokenTTL:       p.duration("AUTH_REFRESH_TOKEN_TTL", 720*time.Hour),
		SessionMaxAge:         p.duration("AUTH_SESSION_MAX_AGE", 2160*time.Hour),
		CORSAllowedOrigins:    p.origins("CORS_ALLOWED_ORIGINS"),
		AppPublicURL:          p.publicURL("APP_PUBLIC_URL"),
		AIDriver:              p.oneOf("AI_DRIVER", "fake", "fake", "none"),
		EmailDriver:           p.oneOf("EMAIL_DRIVER", "log", "log", "none"),
		StorageDriver:         p.oneOf("STORAGE_DRIVER", "none", "none", "memory"),
		ShutdownTimeout:       p.duration("SHUTDOWN_TIMEOUT", 15*time.Second),
	}
	if c.MigrationsDatabaseURL == "" {
		c.MigrationsDatabaseURL = c.DatabaseURL
	}
	if c.AccessTokenTTL > 0 && c.RefreshTokenTTL > 0 && c.RefreshTokenTTL <= c.AccessTokenTTL {
		p.fail("AUTH_REFRESH_TOKEN_TTL", "must be longer than AUTH_ACCESS_TOKEN_TTL")
	}
	if c.RefreshTokenTTL > 0 && c.SessionMaxAge > 0 && c.SessionMaxAge < c.RefreshTokenTTL {
		p.fail("AUTH_SESSION_MAX_AGE", "must be at least AUTH_REFRESH_TOKEN_TTL")
	}
	if c.AppEnv == "production" || c.AppEnv == "staging" {
		for _, o := range c.CORSAllowedOrigins {
			if o == "*" {
				p.fail("CORS_ALLOWED_ORIGINS", "must not contain * outside local/test")
			}
		}
	}
	if err := errors.Join(p.errs...); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}
	return c, nil
}

type parser struct {
	lookup LookupFunc
	errs   []error
}

func (p *parser) get(key string) (string, bool) {
	v, ok := p.lookup(key)
	v = strings.TrimSpace(v)
	return v, ok && v != ""
}

func (p *parser) fail(key, msg string) { p.errs = append(p.errs, fmt.Errorf("%s %s", key, msg)) }

func (p *parser) oneOf(key, def string, allowed ...string) string {
	v, ok := p.get(key)
	if !ok {
		return def
	}
	v = strings.ToLower(v)
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	p.fail(key, fmt.Sprintf("must be one of %s (got %q)", strings.Join(allowed, "|"), v))
	return def
}

func (p *parser) port(key string, def int) int {
	v, ok := p.get(key)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > 65535 {
		p.fail(key, fmt.Sprintf("must be a port number 1-65535 (got %q)", v))
		return def
	}
	return n
}

func (p *parser) logLevel(key string, def slog.Level) slog.Level {
	v, ok := p.get(key)
	if !ok {
		return def
	}
	var l slog.Level
	if err := l.UnmarshalText([]byte(v)); err != nil {
		p.fail(key, fmt.Sprintf("must be debug|info|warn|error (got %q)", v))
		return def
	}
	return l
}

func (p *parser) duration(key string, def time.Duration) time.Duration {
	v, ok := p.get(key)
	if !ok {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		p.fail(key, fmt.Sprintf("must be a positive Go duration such as 15m or 720h (got %q)", v))
		return def
	}
	return d
}

// dbURL validates a postgres URL. The value is a secret (it contains the password), so it is never echoed.
func (p *parser) dbURL(key string, required bool) string {
	v, ok := p.get(key)
	if !ok {
		if required {
			p.fail(key, "is required")
		}
		return ""
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" {
		p.fail(key, "must be a postgres:// URL (value not shown)")
		return ""
	}
	return v
}

// signingKey uses the raw bytes of the variable. Its value and length are never echoed.
func (p *parser) signingKey(key string) []byte {
	v, ok := p.lookup(key)
	if !ok || v == "" {
		p.fail(key, "is required (generate one with: openssl rand -base64 48)")
		return nil
	}
	if len(v) < MinSigningKeyBytes {
		p.fail(key, fmt.Sprintf("must be at least %d bytes (value not shown)", MinSigningKeyBytes))
		return nil
	}
	return []byte(v)
}

func (p *parser) origins(key string) []string {
	v, ok := p.get(key)
	if !ok {
		return nil
	}
	var out []string
	for _, o := range strings.Split(v, ",") {
		o = strings.TrimRight(strings.TrimSpace(o), "/")
		if o == "" {
			continue
		}
		if o != "*" {
			u, err := url.Parse(o)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || (u.Path != "" && u.Path != "/") {
				p.fail(key, fmt.Sprintf("entry %q must be an origin like https://app.example.com", o))
				continue
			}
		}
		out = append(out, o)
	}
	return out
}

func (p *parser) publicURL(key string) string {
	v, ok := p.get(key)
	if !ok {
		return ""
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		p.fail(key, fmt.Sprintf("must be an absolute http(s) URL (got %q)", v))
		return ""
	}
	return strings.TrimRight(v, "/")
}
