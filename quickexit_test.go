package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A game that quits straight after Play should be flagged, so the launcher
// can explain Controlled folder access; pressing Play again clears it.
func TestQuickExitIsFlagged(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ProjectReclaimer")
	os.MkdirAll(dir, 0o755)
	exe := filepath.Join(dir, "project-reclaimer-client-v0.9.23.exe")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgMu.Lock()
	cfg.Paths["h3"] = dir
	cfgMu.Unlock()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Cleanup(func() {
		cfgMu.Lock()
		delete(cfg.Paths, "h3")
		cfgMu.Unlock()
		runMu.Lock()
		delete(quickExit, "h3")
		runMu.Unlock()
	})

	if err := launchGame("h3"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(8 * time.Second)
	got := ""
	for time.Now().Before(deadline) && got == "" {
		time.Sleep(200 * time.Millisecond)
		runMu.Lock()
		got = quickExit["h3"]
		runMu.Unlock()
	}
	if got != filepath.Base(exe) {
		t.Fatalf("quick exit not flagged, got %q", got)
	}

	// Play again: the flag clears while the game starts.
	os.WriteFile(exe, []byte("#!/bin/sh\nsleep 20\n"), 0o755)
	if err := launchGame("h3"); err != nil {
		t.Fatal(err)
	}
	runMu.Lock()
	got = quickExit["h3"]
	pid := launched["h3"]
	runMu.Unlock()
	if got != "" {
		t.Fatal("flag not cleared on Play")
	}
	if p, err := os.FindProcess(pid); err == nil {
		p.Kill()
	}
}
