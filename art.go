package main

// Per-game background art. Built-in art ships inside the exe (ui/art), used
// under Microsoft's Game Content Usage Rules. A player's own pick ("Change
// background", kept in the settings folder) or an "art" folder next to the
// exe takes priority over it.

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

var artExts = []string{".jpg", ".jpeg", ".png", ".webp"}

func artDirs() []string {
	dirs := []string{filepath.Join(dataDir(), "art")}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Join(filepath.Dir(exe), "art"))
	}
	return dirs
}

// findArt returns the image for a game, the player's own pick first.
func findArt(id string) (string, os.FileInfo) {
	for _, d := range artDirs() {
		for _, ext := range artExts {
			p := filepath.Join(d, id+ext)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p, st
			}
		}
	}
	return "", nil
}

func builtinArt(id string) []byte {
	b, err := uiFS.ReadFile("ui/art/" + id + ".jpg")
	if err != nil {
		return nil
	}
	return b
}

// artURL returns the background URL and whether it's the player's own image.
func artURL(id string) (string, bool) {
	p, st := findArt(id)
	if p != "" {
		return fmt.Sprintf("/art/%s?v=%d", id, st.ModTime().Unix()), true
	}
	if builtinArt(id) != nil {
		return "/art/" + id + "?v=builtin-" + appVersion, false
	}
	return "", false
}

func serveArt(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/art/")
	if gameByID(id) == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	if p, _ := findArt(id); p != "" {
		http.ServeFile(w, r, p)
		return
	}
	if b := builtinArt(id); b != nil {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(b)
		return
	}
	http.NotFound(w, r)
}

func setArt(id, src string) error {
	ext := strings.ToLower(filepath.Ext(src))
	ok := false
	for _, e := range artExts {
		if e == ext {
			ok = true
		}
	}
	if !ok {
		return errors.New("choose a .jpg, .png or .webp image")
	}
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	if st.Size() > 25<<20 {
		return errors.New("that image is over 25 MB — choose a smaller one")
	}
	dir := filepath.Join(dataDir(), "art")
	os.MkdirAll(dir, 0o755)
	clearArt(id)
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(filepath.Join(dir, id+ext))
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// adoptArt copies art that sits next to the exe into the launcher's own
// art folder (for games that don't have one there yet), so the backgrounds
// keep showing even when the exe is run from somewhere else later.
func adoptArt() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	src := filepath.Join(filepath.Dir(exe), "art")
	dst := filepath.Join(dataDir(), "art")
	if filepath.Clean(src) == filepath.Clean(dst) || !dirExists(src) {
		return
	}
	for _, g := range games {
		have := false
		for _, ext := range artExts {
			if _, err := os.Stat(filepath.Join(dst, g.ID+ext)); err == nil {
				have = true
			}
		}
		if have {
			continue
		}
		for _, ext := range artExts {
			p := filepath.Join(src, g.ID+ext)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				setArt(g.ID, p)
				break
			}
		}
	}
}

// clearArt removes the player's pick (art next to the exe stays).
func clearArt(id string) {
	if gameByID(id) == nil {
		return // only ever touch the launcher's own art files
	}
	for _, ext := range artExts {
		os.Remove(filepath.Join(dataDir(), "art", id+ext))
	}
}

func registerExtraRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/art/", guard(serveArt))

	mux.HandleFunc("/api/art-pick", guard(func(w http.ResponseWriter, r *http.Request) {
		q := readReq(r)
		g := gameByID(q.ID)
		if g == nil {
			writeJSON(w, 400, map[string]string{"error": "unknown game"})
			return
		}
		p, err := pickImage("Choose a background for " + g.Title)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if p == "" {
			writeJSON(w, 200, map[string]string{"status": "cancelled"})
			return
		}
		if err := setArt(g.ID, p); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ok"})
	}))

	mux.HandleFunc("/api/art-clear", guard(func(w http.ResponseWriter, r *http.Request) {
		q := readReq(r)
		if gameByID(q.ID) == nil {
			writeJSON(w, 400, map[string]string{"error": "unknown game"})
			return
		}
		clearArt(q.ID)
		writeJSON(w, 200, map[string]string{"status": "ok"})
	}))

	mux.HandleFunc("/api/install", guard(func(w http.ResponseWriter, r *http.Request) {
		q := readReq(r)
		g := gameByID(q.ID)
		if g == nil {
			writeJSON(w, 400, map[string]string{"error": "unknown game"})
			return
		}
		dir := strings.TrimSpace(q.Dir)
		if dir == "" {
			dir = suggestInstallDir(g)
		}
		if err := startInstall(g, dir); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"status": "started", "dir": dir})
	}))

	// Lets the player choose where to install: picks a parent folder and
	// returns <parent>\<ProjectFolder> unless the chosen folder is empty.
	mux.HandleFunc("/api/install-pick", guard(func(w http.ResponseWriter, r *http.Request) {
		q := readReq(r)
		g := gameByID(q.ID)
		if g == nil {
			writeJSON(w, 400, map[string]string{"error": "unknown game"})
			return
		}
		start := filepath.Dir(suggestInstallDir(g))
		p, err := pickFolder("Choose where to install "+g.Project, start)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if p == "" {
			writeJSON(w, 200, map[string]string{"status": "cancelled"})
			return
		}
		if !dirEmptyOrMissing(p) {
			p = filepath.Join(p, g.FolderName)
		}
		writeJSON(w, 200, map[string]string{"status": "ok", "dir": p})
	}))

	mux.HandleFunc("/api/install-dismiss", guard(func(w http.ResponseWriter, r *http.Request) {
		jobMu.Lock()
		if job != nil && job.Finished {
			job = nil
		}
		jobMu.Unlock()
		writeJSON(w, 200, map[string]string{"status": "ok"})
	}))
}
