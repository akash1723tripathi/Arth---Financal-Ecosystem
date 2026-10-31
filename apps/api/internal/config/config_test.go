package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const testKey = "0123456789abcdef0123456789abcdef-test-only"

func mapLookup(m map[string]string) LookupFunc {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func base() map[string]string {
	return map[string]string{
		"DATABASE_URL":                  "postgres://u:supersecretpw@127.0.0.1:5432/arth?sslmode=disable",
		"AUTH_ACCESS_TOKEN_SIGNING_KEY": testKey,
	}
}

func TestLoadDefaults(t *testing.T) {
	c, err := LoadFrom(mapLookup(base()))
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	checks := []struct {
		name      string
		got, want any
	}{
		{"AppEnv", c.AppEnv, "local"},
		{"Port", c.Port, 8080},
		{"LogLevel", c.LogLevel, slog.LevelInfo},
		{"MigrationsDatabaseURL falls back", c.MigrationsDatabaseURL, c.DatabaseURL},
		{"AccessTokenTTL", c.AccessTokenTTL, 15 * time.Minute},
		{"RefreshTokenTTL", c.RefreshTokenTTL, 720 * time.Hour},
		{"SessionMaxAge", c.SessionMaxAge, 2160 * time.Hour},
		{"AIDriver", c.AIDriver, "fake"},
		{"EmailDriver", c.EmailDriver, "log"},
		{"StorageDriver", c.StorageDriver, "none"},
		{"ShutdownTimeout", c.ShutdownTimeout, 15 * time.Second},
		{"Signing key", string(c.AccessTokenSigningKey), testKey},
	}
	for _, ck := range checks {
		if !reflect.DeepEqual(ck.got, ck.want) {
			t.Errorf("%s = %v, want %v", ck.name, ck.got, ck.want)
		}
	}
	if c.CORSAllowedOrigins != nil {
		t.Errorf("CORSAllowedOrigins = %v, want nil", c.CORSAllowedOrigins)
	}
}

func TestLoadOverrides(t *testing.T) {
	m := base()
	m["APP_ENV"] = "Production"
	m["PORT"] = "9090"
	m["LOG_LEVEL"] = "debug"
	m["MIGRATIONS_DATABASE_URL"] = "postgresql://m:pw@db:5432/arth"
	m["AUTH_ACCESS_TOKEN_TTL"] = "10m"
	m["AUTH_REFRESH_TOKEN_TTL"] = "48h"
	m["AUTH_SESSION_MAX_AGE"] = "96h"
	m["CORS_ALLOWED_ORIGINS"] = " https://app.example.com/ , http://localhost:3000,,"
	m["APP_PUBLIC_URL"] = "https://app.example.com/"
	m["AI_DRIVER"] = "none"
	m["EMAIL_DRIVER"] = "none"
	m["STORAGE_DRIVER"] = "memory"
	m["SHUTDOWN_TIMEOUT"] = "3s"
	c, err := LoadFrom(mapLookup(m))
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if c.AppEnv != "production" || c.Port != 9090 || c.LogLevel != slog.LevelDebug {
		t.Errorf("env/port/level = %q/%d/%v", c.AppEnv, c.Port, c.LogLevel)
	}
	if c.MigrationsDatabaseURL != m["MIGRATIONS_DATABASE_URL"] {
		t.Errorf("MigrationsDatabaseURL not used")
	}
	if c.AccessTokenTTL != 10*time.Minute || c.RefreshTokenTTL != 48*time.Hour || c.SessionMaxAge != 96*time.Hour {
		t.Errorf("ttls = %v %v %v", c.AccessTokenTTL, c.RefreshTokenTTL, c.SessionMaxAge)
	}
	if want := []string{"https://app.example.com", "http://localhost:3000"}; !reflect.DeepEqual(c.CORSAllowedOrigins, want) {
		t.Errorf("origins = %v, want %v", c.CORSAllowedOrigins, want)
	}
	if c.AppPublicURL != "https://app.example.com" {
		t.Errorf("AppPublicURL = %q", c.AppPublicURL)
	}
	if c.AIDriver != "none" || c.EmailDriver != "none" || c.StorageDriver != "memory" || c.ShutdownTimeout != 3*time.Second {
		t.Errorf("drivers/shutdown = %s %s %s %v", c.AIDriver, c.EmailDriver, c.StorageDriver, c.ShutdownTimeout)
	}
	if c.Addr() != ":9090" || c.IsLocal() {
		t.Errorf("Addr/IsLocal = %q %v", c.Addr(), c.IsLocal())
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(m map[string]string)
		wantMsg string
	}{
		{"missing db url", func(m map[string]string) { delete(m, "DATABASE_URL") }, "DATABASE_URL is required"},
		{"bad db url", func(m map[string]string) { m["DATABASE_URL"] = "mysql://u:supersecretpw@h/db" }, "DATABASE_URL must be a postgres:// URL"},
		{"missing key", func(m map[string]string) { delete(m, "AUTH_ACCESS_TOKEN_SIGNING_KEY") }, "AUTH_ACCESS_TOKEN_SIGNING_KEY is required"},
		{"short key", func(m map[string]string) { m["AUTH_ACCESS_TOKEN_SIGNING_KEY"] = "shortsecretvalue" }, "at least 32 bytes"},
		{"bad port", func(m map[string]string) { m["PORT"] = "70000" }, "PORT must be a port number"},
		{"bad level", func(m map[string]string) { m["LOG_LEVEL"] = "loud" }, "LOG_LEVEL must be"},
		{"bad ttl", func(m map[string]string) { m["AUTH_ACCESS_TOKEN_TTL"] = "15" }, "AUTH_ACCESS_TOKEN_TTL must be a positive Go duration"},
		{"negative ttl", func(m map[string]string) { m["SHUTDOWN_TIMEOUT"] = "-1s" }, "SHUTDOWN_TIMEOUT must be a positive"},
		{"refresh <= access", func(m map[string]string) { m["AUTH_REFRESH_TOKEN_TTL"] = "10m" }, "AUTH_REFRESH_TOKEN_TTL must be longer"},
		{"max age < refresh", func(m map[string]string) { m["AUTH_SESSION_MAX_AGE"] = "24h" }, "AUTH_SESSION_MAX_AGE must be at least"},
		{"bad driver", func(m map[string]string) { m["AI_DRIVER"] = "gemini" }, "AI_DRIVER must be one of fake|none"},
		{"bad storage", func(m map[string]string) { m["STORAGE_DRIVER"] = "s3" }, "STORAGE_DRIVER must be one of none|memory"},
		{"bad env", func(m map[string]string) { m["APP_ENV"] = "dev" }, "APP_ENV must be one of"},
		{"bad origin", func(m map[string]string) { m["CORS_ALLOWED_ORIGINS"] = "app.example.com" }, "CORS_ALLOWED_ORIGINS entry"},
		{"origin with path", func(m map[string]string) { m["CORS_ALLOWED_ORIGINS"] = "https://a.example.com/x" }, "CORS_ALLOWED_ORIGINS entry"},
		{"wildcard in prod", func(m map[string]string) { m["APP_ENV"] = "production"; m["CORS_ALLOWED_ORIGINS"] = "*" }, "must not contain *"},
		{"bad public url", func(m map[string]string) { m["APP_PUBLIC_URL"] = "/relative" }, "APP_PUBLIC_URL must be an absolute"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := base()
			tt.mutate(m)
			_, err := LoadFrom(mapLookup(m))
			if err == nil {
				t.Fatalf("expected error containing %q", tt.wantMsg)
			}
			msg := err.Error()
			if !strings.Contains(msg, tt.wantMsg) {
				t.Errorf("error %q does not contain %q", msg, tt.wantMsg)
			}
			for _, secret := range []string{"supersecretpw", "shortsecretvalue", testKey} {
				if strings.Contains(msg, secret) {
					t.Errorf("error message leaks a secret value: %q", msg)
				}
			}
		})
	}
}

func TestLoadReportsAllErrors(t *testing.T) {
	_, err := LoadFrom(mapLookup(map[string]string{"PORT": "x"}))
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"DATABASE_URL", "AUTH_ACCESS_TOKEN_SIGNING_KEY", "PORT"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %s", err, want)
		}
	}
}

func TestParseDotEnv(t *testing.T) {
	in := "\uFEFF# comment\n\nA=1\nexport B = two words \nC=\"quoted # not comment\" # trailing\nD='single $X \\n'\nE=\"esc \\\"q\\\" \\n end\"\nF=val # comment\nG=\nH=a=b\n"
	got, err := ParseDotEnv(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ParseDotEnv: %v", err)
	}
	want := [][2]string{
		{"A", "1"},
		{"B", "two words"},
		{"C", "quoted # not comment"},
		{"D", `single $X \n`},
		{"E", "esc \"q\" \n end"},
		{"F", "val"},
		{"G", ""},
		{"H", "a=b"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestParseDotEnvErrors(t *testing.T) {
	for _, in := range []string{"NOEQUALS", "1BAD=x", "A=\"unterminated", "A='x' junk", "BAD-KEY=1"} {
		if _, err := ParseDotEnv(strings.NewReader(in)); err == nil {
			t.Errorf("ParseDotEnv(%q): expected error", in)
		}
	}
	_, err := ParseDotEnv(strings.NewReader("OK=1\nA=\"secretvalue"))
	if err == nil || strings.Contains(err.Error(), "secretvalue") || !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error should name the line, not the value: %v", err)
	}
}

func TestLoadDotEnvDoesNotOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("ARTH_CFG_TEST_SET=fromfile\nARTH_CFG_TEST_NEW=fromfile\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ARTH_CFG_TEST_SET", "fromenv")
	t.Setenv("ARTH_CFG_TEST_NEW", "") // registers cleanup; then unset so the file can fill it
	os.Unsetenv("ARTH_CFG_TEST_NEW")
	if err := LoadDotEnv(path); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}
	if v := os.Getenv("ARTH_CFG_TEST_SET"); v != "fromenv" {
		t.Errorf("existing var overridden: %q", v)
	}
	if v := os.Getenv("ARTH_CFG_TEST_NEW"); v != "fromfile" {
		t.Errorf("new var = %q, want fromfile", v)
	}
	if err := LoadDotEnv(filepath.Join(dir, "missing.env")); err != nil {
		t.Errorf("missing file should not error: %v", err)
	}
}
