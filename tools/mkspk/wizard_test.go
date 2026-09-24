package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"wankarr/internal/config"
)

// spkDir locates the packaging skeleton relative to this test.
func spkDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join("..", "..", "spk")
	if _, err := os.Stat(filepath.Join(dir, "INFO")); err != nil {
		t.Skipf("spk skeleton not found at %s: %v", dir, err)
	}
	return dir
}

// The install wizard must stay valid JSON in the exact shape DSM parses:
// an array of steps, each with items, each with subitems keyed wizard_*.
func TestInstallUifileShape(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(spkDir(t), "WIZARD_UIFILES", "install_uifile"))
	if err != nil {
		t.Fatal(err)
	}
	var steps []struct {
		StepTitle string `json:"step_title"`
		Items     []struct {
			Type     string `json:"type"`
			SubItems []struct {
				Key          string `json:"key"`
				Desc         string `json:"desc"`
				DefaultValue any    `json:"defaultValue"`
			} `json:"subitems"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &steps); err != nil {
		t.Fatalf("install_uifile is not valid JSON: %v", err)
	}
	if len(steps) == 0 {
		t.Fatal("install_uifile has no steps")
	}
	seen := map[string]bool{}
	for _, s := range steps {
		if s.StepTitle == "" {
			t.Error("step without step_title")
		}
		for _, it := range s.Items {
			if len(it.SubItems) == 0 {
				t.Errorf("item without subitems in step %q", s.StepTitle)
			}
			for _, sub := range it.SubItems {
				if !strings.HasPrefix(sub.Key, "wizard_") {
					t.Errorf("subitem key %q lacks wizard_ prefix", sub.Key)
				}
				if sub.Desc == "" {
					t.Errorf("subitem %q has no desc", sub.Key)
				}
				if seen[sub.Key] {
					t.Errorf("duplicate wizard key %q", sub.Key)
				}
				seen[sub.Key] = true
			}
		}
	}
	// Every variable service_postinst reads must be asked for.
	for _, want := range []string{
		"wizard_xbvr_url", "wizard_rss_feeds",
		"wizard_jackett_url", "wizard_jackett_api_key", "wizard_torznab_paths",
		"wizard_transmission_url", "wizard_transmission_user",
		"wizard_transmission_pass", "wizard_transmission_dir",
	} {
		if !seen[want] {
			t.Errorf("wizard key %q not asked for", want)
		}
	}
}

// service_postinst must turn wizard answers into a .env the config loader
// accepts, quoting values that would otherwise break dotenv parsing, and
// must never overwrite an existing .env.
func TestServicePostinstWritesEnv(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	dir := spkDir(t)
	pkgvar := t.TempDir()

	runPostinst := func(env map[string]string) error {
		cmd := exec.Command("sh", "-c", `. ./service-setup; service_postinst`)
		cmd.Dir = filepath.Join(dir, "scripts")
		cmd.Env = []string{"SYNOPKG_PKGVAR=" + pkgvar, "PATH=/usr/bin:/bin"}
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("postinst output: %s", out)
		}
		return err
	}

	tricky := "p@ss #with 'quotes'"
	wizard := map[string]string{
		"wizard_xbvr_url":         "http://xbvr:9999",
		"wizard_rss_feeds":        "https://tracker/feed?auth=abc,https://other/feed?auth=def",
		"wizard_jackett_url":      "http://nas:9117",
		"wizard_jackett_api_key":  tricky,
		"wizard_torznab_paths":    "/api/v2.0/indexers/empornium/results/torznab/",
		"wizard_transmission_url": "http://nas:9091/transmission/rpc",
		"wizard_transmission_user": "tom",
		"wizard_transmission_pass": tricky,
		"wizard_transmission_dir": "/mnt/media/vr",
	}
	if err := runPostinst(wizard); err != nil {
		t.Fatalf("postinst failed: %v", err)
	}
	dotenv := filepath.Join(pkgvar, ".env")
	st, err := os.Stat(dotenv)
	if err != nil {
		t.Fatalf(".env not written: %v", err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf(".env mode = %o, want 600", st.Mode().Perm())
	}

	cfg, err := config.Load(dotenv)
	if err != nil {
		t.Fatalf("generated .env does not parse: %v", err)
	}
	if cfg.XBVRURL != "http://xbvr:9999" {
		t.Errorf("XBVRURL = %q", cfg.XBVRURL)
	}
	if len(cfg.RSSFeeds) != 2 || cfg.RSSFeeds[0] != "https://tracker/feed?auth=abc" {
		t.Errorf("RSSFeeds = %q", cfg.RSSFeeds)
	}
	if cfg.JackettAPIKey != tricky {
		t.Errorf("JackettAPIKey = %q, want tricky value intact", cfg.JackettAPIKey)
	}
	if cfg.TransmissionPass != tricky {
		t.Errorf("TransmissionPass = %q, want tricky value intact", cfg.TransmissionPass)
	}
	if cfg.TransmissionDownloadDir != "/mnt/media/vr" {
		t.Errorf("TransmissionDownloadDir = %q", cfg.TransmissionDownloadDir)
	}

	// A second run (e.g. reinstall over existing data) keeps the user's file.
	sentinel := []byte("XBVR_URL=http://untouched:9999\n")
	if err := os.WriteFile(dotenv, sentinel, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runPostinst(wizard); err != nil {
		t.Fatalf("second postinst failed: %v", err)
	}
	kept, _ := os.ReadFile(dotenv)
	if string(kept) != string(sentinel) {
		t.Errorf("postinst overwrote existing .env:\n%s", kept)
	}
}
