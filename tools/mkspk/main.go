// Command mkspk assembles a DSM 7 .spk from a built wankarr binary and
// the spk/ skeleton (INFO, scripts, conf, icons).
//
// Usage:
//
//	go run ./tools/mkspk --binary dist/wankarr-linux-amd64 --arch avoton \
//	    --version 1.0.0 --rev 1 --fw 7.1-42661 --spk-dir spk \
//	    --env-example .env.example --out dist
//
// Output: dist/wankarr_{arch}-{fwshort}_{version}-{rev}.spk plus a
// <name>.manifest.json fragment (filename, arch, version, size, md5)
// for the catalog generator.
package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var fixedTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func main() {
	binary := flag.String("binary", "", "built wankarr binary (GOOS/GOARCH matched)")
	arch := flag.String("arch", "", "DSM arch code, e.g. avoton, x64, aarch64")
	version := flag.String("version", "", "package version, e.g. 1.0.0")
	rev := flag.String("rev", "1", "spk revision")
	fw := flag.String("fw", "7.1-42661", "firmware string")
	spkDir := flag.String("spk-dir", "spk", "skeleton directory")
	envExample := flag.String("env-example", ".env.example", ".env template to seed on install")
	out := flag.String("out", "dist", "output directory")
	flag.Parse()
	if *binary == "" || *arch == "" || *version == "" {
		fmt.Fprintln(os.Stderr, "binary, arch and version are required")
		os.Exit(1)
	}
	if err := run(*binary, *arch, *version, *rev, *fw, *spkDir, *envExample, *out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(binary, arch, version, rev, fw, spkDir, envExample, out string) error {
	stage, err := os.MkdirTemp("", "wankarr-spk")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)

	read := func(rel string) []byte {
		b, err := os.ReadFile(filepath.Join(spkDir, rel))
		if err != nil {
			fmt.Fprintf(os.Stderr, "missing %s: %v\n", rel, err)
			os.Exit(1)
		}
		return b
	}

	// package.tgz payload: the binary plus the .env seed template.
	pkgFiles := []tarFile{
		{Name: "bin/wankarr", Mode: 0o755, Data: mustRead(binary)},
		{Name: "share/wankarr/dot-env-example", Mode: 0o644, Data: mustRead(envExample)},
	}
	pkgTgz := filepath.Join(stage, "package.tgz")
	if err := writeTar(pkgTgz, pkgFiles, true); err != nil {
		return err
	}

	fwShort := fw
	if i := strings.Index(fw, "-"); i >= 0 {
		fwShort = fw[:i]
	}
	info := strings.NewReplacer(
		"__VERSION__", version+"-"+rev,
		"__ARCH__", arch,
	).Replace(string(read("INFO")))

	iconDir := filepath.Join(stage, "icons")
	if err := os.MkdirAll(iconDir, 0o755); err != nil {
		return err
	}
	if err := genIcons(iconDir); err != nil {
		return fmt.Errorf("icons: %w", err)
	}

	spkName := fmt.Sprintf("wankarr_%s-%s_%s-%s.spk", arch, fwShort, version, rev)
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	spkPath := filepath.Join(out, spkName)
	files := []tarFile{
		{Name: "INFO", Mode: 0o644, Data: []byte(info)},
		{Name: "scripts", Mode: 0o755, Dir: true},
		{Name: "conf", Mode: 0o755, Dir: true},
		{Name: "WIZARD_UIFILES", Mode: 0o755, Dir: true},
		{Name: "PACKAGE_ICON.PNG", Mode: 0o644, Data: mustRead(filepath.Join(iconDir, "PACKAGE_ICON.PNG"))},
		{Name: "PACKAGE_ICON_256.PNG", Mode: 0o644, Data: mustRead(filepath.Join(iconDir, "PACKAGE_ICON_256.PNG"))},
		{Name: "package.tgz", Mode: 0o644, Data: mustRead(pkgTgz)},
		{Name: "scripts/installer", Mode: 0o755, Data: read("scripts/installer")},
		{Name: "scripts/start-stop-status", Mode: 0o755, Data: read("scripts/start-stop-status")},
		{Name: "scripts/service-setup", Mode: 0o644, Data: read("scripts/service-setup")},
		{Name: "conf/privilege", Mode: 0o644, Data: read("conf/privilege")},
		{Name: "WIZARD_UIFILES/install_uifile", Mode: 0o755, Data: read("WIZARD_UIFILES/install_uifile")},
	}
	// The outer .spk is a plain (uncompressed) tar like SynoCommunity
	// builds; only the inner package.tgz is gzipped. DSM rejects a
	// gzipped outer package as "Invalid file format".
	if err := writeTar(spkPath, files, false); err != nil {
		return err
	}

	sum, size, err := digest(spkPath)
	if err != nil {
		return err
	}
	manifest, _ := json.MarshalIndent(map[string]any{
		"filename": spkName, "arch": arch, "fw": fw,
		"version": version + "-" + rev, "size": size, "md5": sum,
	}, "", "  ")
	manifestPath := filepath.Join(out, strings.TrimSuffix(spkName, ".spk")+".manifest.json")
	if err := os.WriteFile(manifestPath, append(manifest, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s (%d bytes, md5 %s)\n", spkPath, size, sum)
	return nil
}

type tarFile struct {
	Name string
	Mode int64
	Data []byte
	Dir  bool
}

// genIcons runs the icon generator into dir, locating the module root by
// walking up from the working directory.
func genIcons(dir string) error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	cmd := exec.Command("go", "run", "./tools/icon", dir)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v: %s", err, out)
	}
	return nil
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found")
		}
		dir = parent
	}
}

func writeTar(path string, files []tarFile, gz bool) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var tw *tar.Writer
	var gw *gzip.Writer
	if gz {
		gw = gzip.NewWriter(f)
		defer gw.Close()
		tw = tar.NewWriter(gw)
	} else {
		tw = tar.NewWriter(f)
	}
	defer tw.Close()
	for _, tf := range files {
		hdr := &tar.Header{
			Name: tf.Name, Mode: tf.Mode, Size: int64(len(tf.Data)),
			ModTime: fixedTime, Format: tar.FormatUSTAR,
			Uid: 0, Gid: 0, Uname: "root", Gname: "root",
		}
		if tf.Dir {
			hdr.Typeflag = tar.TypeDir
			hdr.Name += "/"
			hdr.Size = 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if !tf.Dir {
			if _, err := tw.Write(tf.Data); err != nil {
				return err
			}
		}
	}
	return nil
}

func mustRead(path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read %s: %v\n", path, err)
		os.Exit(1)
	}
	return b
}

func digest(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := md5.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
