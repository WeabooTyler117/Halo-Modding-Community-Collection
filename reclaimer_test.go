package main

import (
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

func mkFiles(t *testing.T, dir string, names ...string) {
	for _, n := range names {
		p := filepath.Join(dir, n)
		if strings.HasSuffix(n, "/") {
			os.MkdirAll(p, 0o755)
			continue
		}
		os.WriteFile(p, []byte("x"), 0o644)
	}
}

// A player's D:\reclaimer: Project Reclaimer's dedicated server. It must
// never be started as the game.
func TestReclaimerServerIsNotTheGame(t *testing.T) {
	dir := t.TempDir()
	mkFiles(t, dir, "content/", "data/", "game/", "playlists/", "bans.json", "project-reclaimer.exe", "project-reclaimer.version")
	if got := reclaimerExe(dir); got != "" {
		t.Fatalf("server picked as the game: %s", got)
	}
	if why := gameByID("h3").WrongFolder(dir); !strings.Contains(why, "server") {
		t.Fatalf("no server explanation: %q", why)
	}
	// The client placed in the same folder is fine.
	mkFiles(t, dir, "project-reclaimer-client-v0.9.23.exe")
	if got := filepath.Base(reclaimerExe(dir)); got != "project-reclaimer-client-v0.9.23.exe" {
		t.Fatalf("got %q", got)
	}
	if why := gameByID("h3").WrongFolder(dir); why != "" {
		t.Fatalf("client folder flagged: %q", why)
	}
}

func TestReclaimerClientFolders(t *testing.T) {
	for _, names := range [][]string{
		{"project-reclaimer-client-v0.9.18.exe"},
		{"project-reclaimer-client-v0.9.23.exe", "reclaimer-rcon-v0.9.23.exe"},
		{"project-reclaimer-v0.9.10.exe"},
	} {
		dir := t.TempDir()
		mkFiles(t, dir, names...)
		if got := filepath.Base(reclaimerExe(dir)); got != names[0] {
			t.Errorf("%v: got %q", names, got)
		}
	}
	dir := t.TempDir()
	mkFiles(t, dir, "project-reclaimer-server-v0.9.23.exe")
	if got := reclaimerExe(dir); got != "" {
		t.Errorf("server exe picked: %s", got)
	}
}

// "Download it here": the latest client goes into the chosen folder even when
// it already has files (here, a Reclaimer server), and is then the game.
func TestReclaimerClientDownloadedIntoChosenFolder(t *testing.T) {
	client := []byte("MZ reclaimer client")
	sum := sha256.Sum256(client)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.9.23", "assets": []map[string]any{
				{"name": "reclaimer-rcon-v0.9.23.exe", "browser_download_url": srv.URL + "/dl/rcon", "size": 3},
				{"name": "project-reclaimer-client-v0.9.23.exe", "browser_download_url": srv.URL + "/dl/client",
					"size": len(client), "digest": "sha256:" + hex.EncodeToString(sum[:])},
			}})
		case r.URL.Path == "/dl/client":
			w.Write(client)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("HCC_GH_API", srv.URL)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	mkFiles(t, dir, "playlists/", "bans.json", "project-reclaimer.exe")
	t.Cleanup(func() {
		cfgMu.Lock()
		delete(cfg.Paths, "h3")
		cfgMu.Unlock()
		jobMu.Lock()
		job = nil
		jobMu.Unlock()
	})
	if err := startInstall(gameByID("h3"), dir); err != nil {
		t.Fatal(err)
	}
	var j *InstallJob
	for i := 0; i < 100; i++ {
		if j = currentJob(); j != nil && j.Finished {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if j == nil || j.Phase != "done" || !j.Verified {
		t.Fatalf("job %+v", j)
	}
	if got := filepath.Base(reclaimerExe(dir)); got != "project-reclaimer-client-v0.9.23.exe" {
		t.Fatalf("got %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "bans.json")); err != nil {
		t.Fatal("existing files were touched")
	}
}

func TestZipGamesStillNeedAnEmptyFolder(t *testing.T) {
	dir := t.TempDir()
	mkFiles(t, dir, "something.txt")
	if err := startInstall(gameByID("reach"), dir); err == nil || !strings.Contains(err.Error(), "isn't empty") {
		t.Fatalf("got %v", err)
	}
}
