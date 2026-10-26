package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
