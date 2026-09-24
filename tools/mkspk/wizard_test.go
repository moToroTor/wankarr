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

type wizardStep struct {
	StepTitle string `json:"step_title"`
	Items     []struct {
		Type     string `json:"type"`
		SubItems []struct {
			Key          string `json:"key"`
			Desc         string `json:"desc"`
			DefaultValue string `json:"defaultValue"`
			Validator    map[string]any `json:"validator"`
		} `json:"subitems"`
	} `json:"items"`
}

// The wizard DSM renders must be an array of steps, each with items, each
// with subitems keyed wizard_*. Applies to static files and .sh output.
func assertWizardShape(t *testing.T, steps []wizardStep) map[string]string {
	t.Helper()
	if len(steps) == 0 {
		t.Fatal("wizard has no steps")
	}
	seen := map[string]string{}
	for _, s := range steps {
		if s.StepTitle == "" {
			t.Error("step without step_title")
		}
		for _, it := range s.Items {
			for _, sub := range it.SubItems {
				if !strings.HasPrefix(sub.Key, "wizard_") {
					t.Errorf("subitem key %q lacks wizard_ prefix", sub.Key)
				}
				if sub.Desc == "" {
					t.Errorf("subitem %q has no desc", sub.Key)
				}
				if _, dup := seen[sub.Key]; dup {
					t.Errorf("duplicate wizard key %q", sub.Key)
				}
				seen[sub.Key] = sub.DefaultValue
			}
		}
	}
	// Every variable the service scripts read must be asked for.
	for _, want := range []string{
		"wizard_xbvr_url", "wizard_rss_feeds",
		"wizard_jackett_url", "wizard_jackett_api_key", "wizard_torznab_paths",
		"wizard_transmission_url", "wizard_transmission_user",
		"wizard_transmission_pass", "wizard_transmission_dir",
	} {
		if _, ok := seen[want]; !ok {
			t.Errorf("wizard key %q not asked for", want)
		}
	}
	return seen
}

// runWizard executes a WIZARD_UIFILES/*.sh generator the way DSM does and
// returns the parsed wizard JSON it writes to the temp logfile.
func runWizard(t *testing.T, script, pkgvar string, env map[string]string) []wizardStep {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	out := filepath.Join(t.TempDir(), "wizard.json")
	cmd := exec.Command("bash", script)
	cmd.Env = []string{
		"SYNOPKG_PKGVAR=" + pkgvar,
		"SYNOPKG_TEMP_LOGFILE=" + out,
		"PATH=/usr/bin:/bin",
	}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s failed: %v\n%s", script, err, combined)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("%s wrote no wizard JSON: %v", script, err)
	}
	var steps []wizardStep
	if err := json.Unmarshal(raw, &steps); err != nil {
		t.Fatalf("%s output is not valid JSON: %v\n%s", script, err, raw)
	}
	return steps
}

func TestInstallWizardDefaults(t *testing.T) {
	steps := runWizard(t,
		filepath.Join(spkDir(t), "WIZARD_UIFILES", "install_uifile.sh"),
		t.TempDir(), nil)
	defaults := assertWizardShape(t, steps)
	if defaults["wizard_xbvr_url"] != "http://127.0.0.1:9999" {
		t.Errorf("wizard_xbvr_url default = %q", defaults["wizard_xbvr_url"])
	}
	if defaults["wizard_transmission_url"] != "http://127.0.0.1:9091/transmission/rpc" {
		t.Errorf("wizard_transmission_url default = %q", defaults["wizard_transmission_url"])
	}
	if defaults["wizard_rss_feeds"] != "" {
		t.Errorf("wizard_rss_feeds default = %q, want empty", defaults["wizard_rss_feeds"])
	}
}

// The upgrade wizard must pre-fill every box from the current .env,
// including values with quotes, backslashes and comment-like text.
func TestUpgradeWizardPrefill(t *testing.T) {
	pkgvar := t.TempDir()
	tricky := `p@ss #with 'quotes' and "dbl" and \ backslash`
	dotenv := "XBVR_URL='http://xbvr:9999'\n" +
		"RSS_FEEDS='https://tracker/feed?auth=abc,https://other/feed?auth=def'\n" +
		"JACKETT_URL='http://nas:9117'\n" +
		"JACKETT_API_KEY='" + strings.ReplaceAll(tricky, `'`, `'\''`) + "'\n" +
		"JACKETT_TORZNAB_PATHS='/api/v2.0/indexers/empornium/results/torznab/'\n" +
		"TRANSMISSION_URL='http://nas:9091/transmission/rpc'\n" +
		"TRANSMISSION_USER='tom'\n" +
		"TRANSMISSION_PASS='" + strings.ReplaceAll(tricky, `'`, `'\''`) + "'\n" +
		"TRANSMISSION_DOWNLOAD_DIR='/mnt/media/vr'\n"
	if err := os.WriteFile(filepath.Join(pkgvar, ".env"), []byte(dotenv), 0o600); err != nil {
		t.Fatal(err)
	}

	steps := runWizard(t,
		filepath.Join(spkDir(t), "WIZARD_UIFILES", "upgrade_uifile.sh"),
		pkgvar, nil)
	defaults := assertWizardShape(t, steps)

	want := map[string]string{
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
	for k, v := range want {
		if defaults[k] != v {
			t.Errorf("prefill %s = %q, want %q", k, defaults[k], v)
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
		"wizard_transmission_dir": "",
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
	if cfg.TransmissionDownloadDir != "" {
		t.Errorf("TransmissionDownloadDir = %q, want empty", cfg.TransmissionDownloadDir)
	}
	// Every value must be single-quoted (sed prints nothing for empty
	// input, which once produced bare KEY= lines).
	rawEnv, _ := os.ReadFile(dotenv)
	for _, line := range strings.Split(string(rawEnv), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		_, val, ok := strings.Cut(line, "=")
		if !ok || len(val) < 2 || !strings.HasPrefix(val, "'") || !strings.HasSuffix(val, "'") {
			t.Errorf("unquoted .env line: %q", line)
		}
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

// service_postupgrade rewrites .env from the (pre-filled) upgrade answers,
// but a wizard-less upgrade must leave the file untouched.
func TestServicePostupgrade(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	dir := spkDir(t)

	runHook := func(pkgvar string, env map[string]string, hook string) error {
		cmd := exec.Command("sh", "-c", `. ./service-setup; `+hook)
		cmd.Dir = filepath.Join(dir, "scripts")
		cmd.Env = []string{"SYNOPKG_PKGVAR=" + pkgvar, "PATH=/usr/bin:/bin"}
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("%s output: %s", hook, out)
		}
		return err
	}

	pkgvar := t.TempDir()
	dotenv := filepath.Join(pkgvar, ".env")
	original := "XBVR_URL='http://old:9999'\nRSS_FEEDS='https://old/feed'\n"
	if err := os.WriteFile(dotenv, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	// Wizard ran (required answers present, one value changed).
	if err := runHook(pkgvar, map[string]string{
		"wizard_xbvr_url":  "http://new:9999",
		"wizard_rss_feeds": "https://old/feed",
	}, "service_postupgrade"); err != nil {
		t.Fatalf("postupgrade failed: %v", err)
	}
	cfg, err := config.Load(dotenv)
	if err != nil {
		t.Fatalf("rewritten .env does not parse: %v", err)
	}
	if cfg.XBVRURL != "http://new:9999" {
		t.Errorf("XBVRURL = %q, want updated value", cfg.XBVRURL)
	}
	if len(cfg.RSSFeeds) != 1 || cfg.RSSFeeds[0] != "https://old/feed" {
		t.Errorf("RSSFeeds = %q", cfg.RSSFeeds)
	}

	// No wizard ran (vars unset): existing file is preserved.
	if err := os.WriteFile(dotenv, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runHook(pkgvar, nil, "service_postupgrade"); err != nil {
		t.Fatalf("wizard-less postupgrade failed: %v", err)
	}
	kept, _ := os.ReadFile(dotenv)
	if string(kept) != original {
		t.Errorf("wizard-less postupgrade rewrote .env:\n%s", kept)
	}

	// No wizard ran and no .env exists yet (e.g. upgrading the
	// wizard-less v0.1.3): seed defaults so the package is configurable.
	pkgvar2 := t.TempDir()
	if err := runHook(pkgvar2, nil, "service_postupgrade"); err != nil {
		t.Fatalf("seed postupgrade failed: %v", err)
	}
	seeded, err := config.Load(filepath.Join(pkgvar2, ".env"))
	if err != nil {
		t.Fatalf("seeded .env does not parse: %v", err)
	}
	if seeded.XBVRURL != "http://127.0.0.1:9999" {
		t.Errorf("seeded XBVRURL = %q", seeded.XBVRURL)
	}
}
