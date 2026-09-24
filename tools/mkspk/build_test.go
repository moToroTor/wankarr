package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The DSM 7 glibc on this NAS line (2.26) predates the CI builders', so a
// cgo binary dies at startup with "GLIBC_2.3x not found" (seen in the
// package log). Every DSM build path must force a static binary.
func TestStaticBuildEnforced(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, f := range []string{
		"Makefile",
		filepath.Join(".github", "workflows", "release.yml"),
	} {
		raw, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if !strings.Contains(string(raw), "CGO_ENABLED=0") {
			t.Errorf("%s does not force CGO_ENABLED=0; DSM builds would link current glibc", f)
		}
	}
}
