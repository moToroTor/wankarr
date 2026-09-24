package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// DSM's Open button goes to http://host:adminport/adminurl. The app serves
// its UI at / (FileServer) with /api/* beside it, so any adminurl prefix
// (e.g. "wankarr/") 404s. Like Radarr/Jackett, declare only adminport so
// DSM opens the root.
func TestInfoOpensAtRoot(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(spkDir(t), "INFO"))
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			t.Errorf("INFO line without =: %q", line)
			continue
		}
		fields[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"`)
	}
	if fields["adminport"] != "8060" {
		t.Errorf("adminport = %q, want 8060", fields["adminport"])
	}
	if u, ok := fields["adminurl"]; ok && u != "" {
		t.Errorf("adminurl = %q, want absent (DSM would open a 404 path)", u)
	}
}
