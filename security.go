package main

// Safety checks shared by the rest of the launcher.

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ---------- game folders ----------

// sharedDirs are folders that hold many unrelated programs or personal files.
// A game folder must never be one of these (or a drive root), because the
// launcher closes "the game's" programs by looking at where they run from.
var (
	sharedOnce  sync.Once
	sharedCache []string
)

func sharedDirs() []string {
	sharedOnce.Do(func() { sharedCache = findSharedDirs() })
	return sharedCache
}

func findSharedDirs() []string {
	var out []string
	for _, env := range []string{"SystemRoot", "windir", "ProgramFiles", "ProgramFiles(x86)", "ProgramW6432",
		"ProgramData", "APPDATA", "LOCALAPPDATA", "USERPROFILE", "PUBLIC", "TEMP", "TMP"} {
		if v := os.Getenv(env); v != "" {
			out = append(out, v)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		out = append(out, home)
		for _, s := range []string{"Desktop", "Downloads", "Documents", "Music", "Pictures", "Videos", "OneDrive"} {
			out = append(out, filepath.Join(home, s))
		}
	}
	if p := steamPath(); p != "" {
		out = append(out, p, filepath.Join(p, "steamapps"), filepath.Join(p, "steamapps", "common"))
	}
	return out
}

func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// checkGameDir says why a folder can't be used as a game folder, or nil.
func checkGameDir(p string) error {
	if p == "" || !filepath.IsAbs(p) {
		return errors.New("choose a full folder path")
	}
	c := filepath.Clean(p)
	if c == filepath.VolumeName(c)+string(filepath.Separator) || filepath.Dir(c) == c {
		return errors.New("that's a whole drive — choose the game's own folder")
	}
	for _, s := range sharedDirs() {
		if samePath(c, s) {
			return errors.New("that folder is shared with other programs — choose the game's own folder")
		}
	}
	for _, env := range []string{"SystemRoot", "windir"} {
		if w := os.Getenv(env); w != "" && (samePath(c, w) || underDir(c, w)) {
			return errors.New("that's a Windows system folder — choose the game's own folder")
		}
	}
	return nil
}

// belongsToGame decides whether a running program is part of a game: its exe
// must sit directly in the game's folder or in a bin-style subfolder of it
// (some projects start a helper from there). Programs anywhere deeper are
// left alone, so a broad folder can never sweep up unrelated apps.
func belongsToGame(exePath, gameDir string) bool {
	if exePath == "" || gameDir == "" || checkGameDir(gameDir) != nil {
		return false
	}
	parent := filepath.Dir(filepath.Clean(exePath))
	if samePath(parent, gameDir) {
		return true
	}
	switch strings.ToLower(filepath.Base(parent)) {
	case "bin", "binaries", "x64", "win64":
		return samePath(filepath.Dir(parent), gameDir)
	}
	return false
}

// ---------- local server ----------

func tokenOK(t string) bool {
	return t != "" && subtle.ConstantTimeCompare([]byte(t), []byte(token)) == 1
}

// readOnlyAPI lists the API calls that only read; everything else changes
// something and must be a POST (so it can't be triggered by a plain link).
var readOnlyAPI = map[string]bool{"/api/state": true, "/api/ping": true}

// secureHeaders stops other sites from framing the menu, keeps the session
// token out of Referer headers, and limits what the page can load.
func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; "+
				"script-src 'self' 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; "+
				"base-uri 'none'; form-action 'none'; object-src 'none'")
		next.ServeHTTP(w, r)
	})
}
