package config

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
)

// LoadDotEnv reads KEY=VALUE lines from path into the process environment.
// Variables already set in the environment win (they are never overridden). A missing file is not an error.
// Supported: blank lines, # comments, an optional "export " prefix, single or double quoted values
// (double quotes understand \n, \", \\), and trailing " # comment" after unquoted values.
func LoadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	vars, err := ParseDotEnv(f)
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	for _, kv := range vars {
		if _, set := os.LookupEnv(kv[0]); set {
			continue
		}
		if err := os.Setenv(kv[0], kv[1]); err != nil {
			return fmt.Errorf("set %s: %w", kv[0], err)
		}
	}
	return nil
}

// ParseDotEnv parses .env content into ordered key/value pairs. Errors mention line numbers, never values.
func ParseDotEnv(r io.Reader) ([][2]string, error) {
	var out [][2]string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if lineNo == 1 {
			line = strings.TrimPrefix(line, string(rune(0xFEFF))) // UTF-8 BOM written by some Windows editors
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || !validKey(key) {
			return nil, fmt.Errorf("line %d: expected KEY=VALUE", lineNo)
		}
		v, err := parseValue(strings.TrimSpace(val))
		if err != nil {
			return nil, fmt.Errorf("line %d (%s): %w", lineNo, key, err)
		}
		out = append(out, [2]string{key, v})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	return out, nil
}

func validKey(k string) bool {
	if k == "" {
		return false
	}
	for i, c := range k {
		switch {
		case c == '_', c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

func parseValue(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	switch q := v[0]; q {
	case '"', '\'':
		end := closingQuote(v, q)
		if end < 0 {
			return "", errors.New("unterminated quoted value")
		}
		rest := strings.TrimSpace(v[end+1:])
		if rest != "" && !strings.HasPrefix(rest, "#") {
			return "", errors.New("unexpected text after closing quote")
		}
		inner := v[1:end]
		if q == '\'' {
			return inner, nil
		}
		r := strings.NewReplacer(`\n`, "\n", `\"`, `"`, `\\`, `\`)
		return r.Replace(inner), nil
	}
	if i := strings.Index(v, " #"); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v), nil
}

func closingQuote(v string, q byte) int {
	for i := 1; i < len(v); i++ {
		if q == '"' && v[i] == '\\' {
			i++
			continue
		}
		if v[i] == q {
			return i
		}
	}
	return -1
}
