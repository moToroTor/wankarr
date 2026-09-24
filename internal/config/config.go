// Package config loads Wankarr settings from a dotenv file plus environment.
//
// Precedence: process environment > dotenv file > built-in defaults.
// The dotenv file (default ".env" next to the working directory) holds
// secrets and endpoints only; non-secret tuning lives in config.yaml
// (not yet implemented). Values are never logged.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config is the full Wankarr runtime configuration.
//
// Tracker feeds are generic: any number of RSS notification feeds from any
// private trackers (their URLs carry their own auth), and any number of
// Jackett Torznab indexer paths. No site is enshrined in config.
type Config struct {
	XBVRURL string

	RSSFeeds []string

	JackettURL          string
	JackettAPIKey       string
	JackettTorznabPaths []string

	TransmissionURL  string
	TransmissionUser string
	TransmissionPass string
	// Optional Transmission download directory (torrent-add
	// "download-dir"). Empty means use the server default.
	TransmissionDownloadDir string

	DBPath   string
	HTTPPort int

	PollInterval time.Duration
}

// Defaults applied when neither environment nor dotenv file provides a value.
const (
	DefaultXBVRURL         = "http://127.0.0.1:9999"
	DefaultTorznabPath     = "/api/v2.0/indexers/empornium/results/torznab/"
	DefaultTransmissionURL = "http://192.168.0.10:9091/transmission/rpc"
	DefaultDBPath          = "wankarr.db"
	DefaultHTTPPort        = 8060
	DefaultDotenvName      = ".env"
)

// Load reads the dotenv file at path ("" disables file loading) and layers
// process-environment overrides on top. Missing file is not an error;
// missing values fall back to defaults, except secrets which stay empty.
func Load(dotenvPath string) (*Config, error) {
	fileVals := map[string]string{}
	if dotenvPath != "" {
		vals, err := parseDotenv(dotenvPath)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("read dotenv %s: %w", dotenvPath, err)
		}
		fileVals = vals
	}

	get := func(key string) string {
		if v, ok := os.LookupEnv(key); ok {
			return v
		}
		return fileVals[key]
	}

	paths := splitCSV(get("JACKETT_TORZNAB_PATHS"))
	if len(paths) == 0 {
		paths = []string{DefaultTorznabPath}
	}

	cfg := &Config{
		XBVRURL:                 firstNonEmpty(get("XBVR_URL"), DefaultXBVRURL),
		RSSFeeds:                splitCSV(get("RSS_FEEDS")),
		JackettURL:              get("JACKETT_URL"),
		JackettAPIKey:           get("JACKETT_API_KEY"),
		JackettTorznabPaths:     paths,
		TransmissionURL:         firstNonEmpty(get("TRANSMISSION_URL"), DefaultTransmissionURL),
		TransmissionUser:        get("TRANSMISSION_USER"),
		TransmissionPass:        get("TRANSMISSION_PASS"),
		TransmissionDownloadDir: strings.TrimSpace(get("TRANSMISSION_DOWNLOAD_DIR")),
		DBPath:                  firstNonEmpty(get("DB_PATH"), DefaultDBPath),
		HTTPPort:                parsePort(get("HTTP_PORT"), DefaultHTTPPort),
		PollInterval:            parseDuration(get("POLL_INTERVAL"), 4*time.Hour),
	}
	return cfg, nil
}

// parsePort accepts a TCP port number; blank or invalid falls back to def.
func parsePort(s string, def int) int {
	if s == "" {
		return def
	}
	var p int
	if _, err := fmt.Sscanf(s, "%d", &p); err != nil || p <= 0 || p > 65535 {
		return def
	}
	return p
}

// parseDuration accepts Go duration strings ("30m", "4h"); blank or
// invalid values fall back to def so a typo can't cause hot polling.
func parseDuration(s string, def time.Duration) time.Duration {
	if s == "" {
		return def
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return def
	}
	return d
}

// DefaultDotenvPath returns dir/.env for dir, or ".env" when dir is empty.
func DefaultDotenvPath(dir string) string {
	if dir == "" {
		return DefaultDotenvName
	}
	return filepath.Join(dir, DefaultDotenvName)
}

// parseDotenv reads KEY=VALUE lines. It supports blank lines, `#` comments,
// and single- or double-quoted values. It is intentionally minimal.
func parseDotenv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	vals := map[string]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.Contains(line, "=") {
			continue
		}
		// Strip inline comments that are preceded by whitespace and outside quotes.
		key, raw, _ := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		val := strings.TrimSpace(stripInlineComment(raw))
		vals[key] = unquote(val)
	}
	return vals, sc.Err()
}

// stripInlineComment cuts a trailing " #..." comment when the # is outside
// quotes and preceded by whitespace.
func stripInlineComment(s string) string {
	var quote rune
	for i, r := range s {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote == 0 && (r == '"' || r == '\''):
			quote = r
		case quote == 0 && r == '#' && i > 0 && (s[i-1] == ' ' || s[i-1] == '\t'):
			return strings.TrimRight(s[:i], " \t")
		}
	}
	return s
}

func unquote(s string) string {
	if len(s) >= 2 {
		if s[0] == '"' && s[len(s)-1] == '"' {
			return s[1 : len(s)-1]
		}
		if s[0] == '\'' && s[len(s)-1] == '\'' {
			// Bash-style embedded quote: '\'' closes the quote,
			// adds a literal quote, and reopens it.
			return strings.ReplaceAll(s[1:len(s)-1], `'\''`, "'")
		}
	}
	return s
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
