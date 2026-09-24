package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempDotenv(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.XBVRURL != DefaultXBVRURL {
		t.Errorf("XBVRURL = %q, want default", cfg.XBVRURL)
	}
	if cfg.TransmissionURL != DefaultTransmissionURL {
		t.Errorf("TransmissionURL = %q, want default", cfg.TransmissionURL)
	}
	if len(cfg.RSSFeeds) != 0 {
		t.Errorf("RSSFeeds = %v, want empty", cfg.RSSFeeds)
	}
	if len(cfg.JackettTorznabPaths) != 1 || cfg.JackettTorznabPaths[0] != DefaultTorznabPath {
		t.Errorf("JackettTorznabPaths = %v, want default", cfg.JackettTorznabPaths)
	}
	if cfg.JackettAPIKey != "" {
		t.Error("JackettAPIKey should be empty by default, never logged or defaulted")
	}
}

func TestLoadDotenvFile(t *testing.T) {
	path := writeTempDotenv(t, `
# comment line
XBVR_URL=http://xbvr:9999
RSS_FEEDS=https://feed-one , https://feed-two
JACKETT_API_KEY=s3cret
TRANSMISSION_URL=http://nas:9091/transmission/rpc
QUOTED="spaced value" # trailing comment
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.XBVRURL != "http://xbvr:9999" {
		t.Errorf("XBVRURL = %q", cfg.XBVRURL)
	}
	if len(cfg.RSSFeeds) != 2 || cfg.RSSFeeds[1] != "https://feed-two" {
		t.Errorf("RSSFeeds = %v", cfg.RSSFeeds)
	}
	if cfg.JackettAPIKey != "s3cret" {
		t.Error("JackettAPIKey not loaded from file")
	}
}

func TestTorznabPathsSplit(t *testing.T) {
	path := writeTempDotenv(t, `
JACKETT_TORZNAB_PATHS=/a/,/b/
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.JackettTorznabPaths) != 2 || cfg.JackettTorznabPaths[1] != "/b/" {
		t.Errorf("JackettTorznabPaths = %v, want split paths", cfg.JackettTorznabPaths)
	}
}

func TestLegacyKeysIgnored(t *testing.T) {
	path := writeTempDotenv(t, `
EMP_RSS_FEEDS=https://old-feed
EMP_BASE_URL=https://old.example
JACKETT_EMP_TORZNAB_PATH=/old/
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.RSSFeeds) != 0 {
		t.Errorf("RSSFeeds = %v, want legacy key ignored", cfg.RSSFeeds)
	}
	if len(cfg.JackettTorznabPaths) != 1 || cfg.JackettTorznabPaths[0] != DefaultTorznabPath {
		t.Errorf("JackettTorznabPaths = %v, want default", cfg.JackettTorznabPaths)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	path := writeTempDotenv(t, "XBVR_URL=http://file:9999\n")
	t.Setenv("XBVR_URL", "http://env:9999")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.XBVRURL != "http://env:9999" {
		t.Errorf("XBVRURL = %q, want env override", cfg.XBVRURL)
	}
}

func TestPortOverride(t *testing.T) {
	path := writeTempDotenv(t, "HTTP_PORT=8061\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPPort != 8061 {
		t.Errorf("HTTPPort = %d, want 8061", cfg.HTTPPort)
	}
	t.Setenv("HTTP_PORT", "bogus")
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPPort != DefaultHTTPPort {
		t.Errorf("HTTPPort = %d, want default on bogus input", cfg.HTTPPort)
	}
}

func TestHostOverride(t *testing.T) {
	path := writeTempDotenv(t, "HTTP_HOST=127.0.0.1\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPHost != "127.0.0.1" {
		t.Errorf("HTTPHost = %q, want 127.0.0.1", cfg.HTTPHost)
	}
	path2 := writeTempDotenv(t, "# no host key\n")
	cfg, err = Load(path2)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPHost != "" {
		t.Errorf("HTTPHost = %q, want empty (all interfaces) by default", cfg.HTTPHost)
	}
}

func TestMissingFileIsNotError(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "does-not-exist")); err != nil {
		t.Fatal(err)
	}
}

func TestTransmissionDownloadDir(t *testing.T) {
	path := writeTempDotenv(t, "TRANSMISSION_DOWNLOAD_DIR=/mnt/media/vr\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TransmissionDownloadDir != "/mnt/media/vr" {
		t.Errorf("TransmissionDownloadDir = %q", cfg.TransmissionDownloadDir)
	}
	// Empty by default: server default is used, download-dir omitted.
	cfg, err = Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TransmissionDownloadDir != "" {
		t.Errorf("TransmissionDownloadDir = %q, want empty default", cfg.TransmissionDownloadDir)
	}
}
