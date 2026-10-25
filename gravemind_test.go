package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The real Project Gravemind folder: h2gravemind.exe next to batch files
// and text files. Only h2gravemind.exe may be picked.
func TestGravemindExeFound(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"crear_partida.bat", "h2gravemind.exe", "LEEME.txt", "OFL-Michroma.txt", "unirse.bat"} {
		os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644)
	}
	if got := gravemindExe(dir); filepath.Base(got) != "h2gravemind.exe" {
		t.Fatalf("got %q", got)
	}
}
