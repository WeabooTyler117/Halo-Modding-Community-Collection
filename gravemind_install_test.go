package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// gravemindZip is laid out like the real h2gravemind_beta.zip.
func gravemindZip(t *testing.T) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"crear_partida.bat": "x", "h2gravemind.exe": "MZ gravemind", "LEEME.txt": "x",
		"OFL-Michroma.txt": "x", "unirse.bat": "x",
	} {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	return buf.Bytes()
}

func installGravemind(t *testing.T, digest string, body []byte) (*InstallJob, string) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.18.458", "assets": []map[string]any{
				{"name": "h2gravemind.exe", "browser_download_url": srv.URL + "/dl/h2gravemind.exe", "size": 12},
				{"name": "h2gravemind_beta.zip", "browser_download_url": srv.URL + "/dl/h2gravemind_beta.zip", "size": len(body), "digest": digest},
			}})
		case r.URL.Path == "/dl/h2gravemind_beta.zip":
			w.Write(body)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("HCC_GH_API", srv.URL)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := filepath.Join(t.TempDir(), "Project Gravemind")
	t.Cleanup(func() {
		cfgMu.Lock()
		delete(cfg.Paths, "h2")
		cfgMu.Unlock()
		jobMu.Lock()
		job = nil
		jobMu.Unlock()
	})
	if err := startInstall(gameByID("h2"), dir); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if j := currentJob(); j != nil && j.Finished {
			return j, dir
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("install didn't finish")
	return nil, ""
}

func TestGravemindInstallsFromGitHub(t *testing.T) {
	body := gravemindZip(t)
	sum := sha256.Sum256(body)
	j, dir := installGravemind(t, "sha256:"+hex.EncodeToString(sum[:]), body)
	if j.Phase != "done" || !j.Verified {
		t.Fatalf("phase %q verified %v error %q", j.Phase, j.Verified, j.Error)
	}
	if filepath.Base(gravemindExe(dir)) != "h2gravemind.exe" {
		t.Fatal("h2gravemind.exe not installed at the folder's top level")
	}
	cfgMu.Lock()
	got := cfg.Paths["h2"]
	cfgMu.Unlock()
	if got != dir {
		t.Fatalf("folder not saved: %q", got)
	}
}

func TestGravemindInstallRefusesWrongChecksum(t *testing.T) {
	body := gravemindZip(t)
	j, dir := installGravemind(t, "sha256:"+strings.Repeat("0", 64), body)
	if j.Phase != "failed" || !strings.Contains(j.Error, "checksum") {
		t.Fatalf("phase %q error %q", j.Phase, j.Error)
	}
	if _, err := os.Stat(filepath.Join(dir, "h2gravemind.exe")); err == nil {
		t.Fatal("game files left after a refused download")
	}
}

func TestGitHubDigestParsing(t *testing.T) {
	good := strings.Repeat("ab", 32)
	for in, want := range map[string]string{
		"sha256:" + good: good, "SHA256:" + strings.ToUpper(good): good,
		"": "", "md5:" + good: "", "sha256:xyz": "", "sha256:" + good + "00": "",
	} {
		a := &ghAsset{Digest: in}
		if got := a.githubSHA256(); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}
