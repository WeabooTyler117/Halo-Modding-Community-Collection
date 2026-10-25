package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func writeZip(t *testing.T, path string, files map[string]string) {
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	f.Close()
}

func TestUnzipStripsSingleTopFolder(t *testing.T) {
	d := t.TempDir()
	z := filepath.Join(d, "a.zip")
	writeZip(t, z, map[string]string{"Game-v1/game.exe": "x", "Game-v1/bin/lib.dll": "y"})
	out := filepath.Join(d, "out")
	if err := unzipInto(z, out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "game.exe")); err != nil {
		t.Fatal("expected game.exe at the root:", err)
	}
	if _, err := os.Stat(filepath.Join(out, "bin", "lib.dll")); err != nil {
		t.Fatal(err)
	}
}

func TestUnzipKeepsMixedRoot(t *testing.T) {
	d := t.TempDir()
	z := filepath.Join(d, "a.zip")
	writeZip(t, z, map[string]string{"halo.exe": "x", "maps/readme.txt": "y"})
	out := filepath.Join(d, "out")
	if err := unzipInto(z, out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "halo.exe")); err != nil {
		t.Fatal(err)
	}
}

func TestUnzipRefusesEscape(t *testing.T) {
	d := t.TempDir()
	z := filepath.Join(d, "evil.zip")
	writeZip(t, z, map[string]string{"ok.txt": "x", "../escaped.txt": "y"})
	out := filepath.Join(d, "out")
	if err := unzipInto(z, out); err == nil {
		t.Fatal("expected an error for ../ path")
	}
	if _, err := os.Stat(filepath.Join(d, "escaped.txt")); err == nil {
		t.Fatal("file escaped the install folder")
	}
}

func TestOfficialURL(t *testing.T) {
	os.Unsetenv("HCC_GH_API")
	if !officialURL("A/b", "https://github.com/A/b/releases/download/v1/x.zip") {
		t.Fatal("official release URL rejected")
	}
	for _, u := range []string{
		"https://github.com/A/bad/releases/download/v1/x.zip",
		"https://evil.com/A/b/releases/download/v1/x.zip",
		"http://github.com/A/b/releases/download/v1/x.zip",
	} {
		if officialURL("A/b", u) {
			t.Fatal("accepted", u)
		}
	}
}
