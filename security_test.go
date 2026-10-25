package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGameDirRefusesDrivesAndSharedFolders(t *testing.T) {
	home, _ := os.UserHomeDir()
	bad := []string{"", "relative/path", string(filepath.Separator), home, filepath.Join(home, "Downloads"), filepath.Join(home, "Desktop")}
	for _, p := range bad {
		if checkGameDir(p) == nil {
			t.Errorf("accepted %q as a game folder", p)
		}
	}
	good := filepath.Join(t.TempDir(), "ReachRenaissance")
	if err := checkGameDir(good); err != nil {
		t.Errorf("refused a normal game folder: %v", err)
	}
}

func TestOnlyTheGamesOwnProgramsCount(t *testing.T) {
	game := filepath.Join(t.TempDir(), "Games", "ReachRenaissance")
	other := filepath.Join(filepath.Dir(game), "SomeOtherApp")
	cases := map[string]bool{
		filepath.Join(game, "Reach Renaissance.exe"):             true,
		filepath.Join(game, "bin", "helper.exe"):                 true,
		filepath.Join(game, "tools", "deep", "x.exe"):            false, // deeper folders are left alone
		filepath.Join(game, "mods", "something.exe"):             false,
		filepath.Join(other, "app.exe"):                          false,
		filepath.Join(game+"2", "Reach Renaissance.exe"):         false, // similar-looking folder name
		filepath.Join(filepath.Dir(game), "launcher-nearby.exe"): false,
	}
	for exe, want := range cases {
		if got := belongsToGame(exe, game); got != want {
			t.Errorf("belongsToGame(%q) = %v, want %v", exe, got, want)
		}
	}
	// A whole drive or shared folder never claims any program.
	home, _ := os.UserHomeDir()
	if belongsToGame(filepath.Join(home, "x.exe"), home) {
		t.Error("a shared folder claimed a program")
	}
}

func TestAPIRequiresTokenPostAndRightHost(t *testing.T) {
	token = "test-token-123"
	h := secureHeaders(routes())
	do := func(method, path, host, tok string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(`{"id":"reach"}`))
		r.Host = host
		if tok != "" {
			r.Header.Set("X-HCC-Token", tok)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	host := "127.0.0.1:47231"
	if c := do("GET", "/api/state", host, "").Code; c != 403 {
		t.Errorf("no token: got %d, want 403", c)
	}
	if c := do("GET", "/api/state", host, "wrong").Code; c != 403 {
		t.Errorf("wrong token: got %d, want 403", c)
	}
	if c := do("GET", "/api/state", "evil.example:47231", token).Code; c != 403 {
		t.Errorf("wrong host: got %d, want 403", c)
	}
	if c := do("GET", "/api/close", host, token).Code; c != 405 {
		t.Errorf("GET on an action: got %d, want 405", c)
	}
	if c := do("GET", "/api/state?t="+token, host, "").Code; c != 403 {
		t.Errorf("token in the URL accepted outside /art/: got %d", c)
	}
	if c := do("POST", "/api/open-url", host, token).Code; c != 400 {
		t.Errorf("open-url with no allowed URL: got %d, want 400", c)
	}
	w := do("GET", "/", host, "")
	for _, hdr := range []string{"X-Frame-Options", "Content-Security-Policy", "Referrer-Policy", "X-Content-Type-Options"} {
		if w.Header().Get(hdr) == "" {
			t.Errorf("missing security header %s", hdr)
		}
	}
}

func TestOpenURLAllowList(t *testing.T) {
	for _, u := range []string{"https://evil.example", "file:///C:/Windows/System32/calc.exe", "https://github.com/someone-else/repo", "javascript:alert(1)"} {
		if allowedURL(u) {
			t.Errorf("allowed %q", u)
		}
	}
	if !allowedURL("https://github.com/OpenCommunityEdition/OpenCE") {
		t.Error("refused a project page")
	}
}

func TestInstallRefusesSharedFolder(t *testing.T) {
	home, _ := os.UserHomeDir()
	if err := startInstall(gameByID("reach"), home); err == nil {
		t.Error("installed into the home folder")
	}
}

func TestGravemindNeverPicksAnUnrelatedExe(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "SomethingElse.exe"), make([]byte, 5000), 0o644)
	if p := gravemindExe(d); p != "" {
		t.Errorf("picked %q", p)
	}
	os.WriteFile(filepath.Join(d, "Project Gravemind.exe"), []byte("x"), 0o644)
	if p := gravemindExe(d); filepath.Base(p) != "Project Gravemind.exe" {
		t.Errorf("got %q", p)
	}
}

func TestZipSizeLimit(t *testing.T) {
	if maxUnpacked <= maxDownload || maxDownload <= 0 {
		t.Fatal("bad limits")
	}
	_ = http.StatusOK
}

func TestRemoveBackgroundOnlyTouchesLauncherArt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
	victim := filepath.Join(home, "photo.jpg")
	os.WriteFile(victim, []byte("keep me"), 0o644)
	rel, _ := filepath.Rel(filepath.Join(dataDir(), "art"), filepath.Join(home, "photo"))
	clearArt(rel) // a crafted "game id" pointing at the photo
	if _, err := os.Stat(victim); err != nil {
		t.Fatal("clearArt deleted a file outside the launcher's art folder")
	}
}

func TestUnzipRefusesReservedAndAbsoluteNames(t *testing.T) {
	d := t.TempDir()
	for _, bad := range []string{"/etc/evil", "a/../../evil.txt", "..\\evil.txt"} {
		z := filepath.Join(d, "z.zip")
		writeZip(t, z, map[string]string{"ok.txt": "x", bad: "y"})
		if err := unzipInto(z, filepath.Join(d, "out")); err == nil {
			t.Errorf("accepted archive entry %q", bad)
		}
	}
}
