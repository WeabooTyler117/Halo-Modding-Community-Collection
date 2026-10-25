package main

// Desktop shortcut to the launcher. Players are asked once, on their first
// launch; the launcher never adds anything to the desktop without a "yes".

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const shortcutName = "Halo Modding Community Collection.lnk"

type ShortcutState struct {
	Exists    bool `json:"exists"`
	Asked     bool `json:"asked"`
	InTemp    bool `json:"inTemp"`    // running from a temporary folder (e.g. straight out of a zip)
	Downloads bool `json:"downloads"` // running from the Downloads folder
}

var (
	scMu     sync.Mutex
	scExists bool
)

func launcherExe() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	exe = filepath.Clean(exe)
	if !strings.EqualFold(filepath.Ext(exe), ".exe") && !testHooks {
		return "", errors.New("unexpected launcher file")
	}
	return exe, nil
}

func shortcutPath() string {
	d := desktopDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, shortcutName)
}

func exeInTemp(exe string) bool {
	for _, t := range []string{os.TempDir(), os.Getenv("TEMP"), os.Getenv("TMP")} {
		if t != "" && underDir(exe, t) {
			return true
		}
	}
	return false
}

func exeInDownloads(exe string) bool {
	home, err := os.UserHomeDir()
	return err == nil && underDir(exe, filepath.Join(home, "Downloads"))
}

// checkShortcut runs at start-up. If the player's shortcut points at a
// launcher that no longer exists (they moved the exe), it is pointed at
// this copy instead.
func checkShortcut() {
	lnk := shortcutPath()
	exists := false
	if lnk != "" {
		if _, err := os.Stat(lnk); err == nil {
			exists = true
			exe, err := launcherExe()
			if err == nil && !exeInTemp(exe) {
				if target := readShortcutTarget(lnk); target != "" && !samePath(target, exe) {
					if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
						createShortcut(lnk, exe) // repair: the old target is gone
					}
				}
			}
		}
	}
	scMu.Lock()
	scExists = exists
	scMu.Unlock()
}

func shortcutState() ShortcutState {
	exe, _ := launcherExe()
	scMu.Lock()
	exists := scExists
	scMu.Unlock()
	cfgMu.Lock()
	asked := cfg.ShortcutAsked
	cfgMu.Unlock()
	return ShortcutState{Exists: exists, Asked: asked, InTemp: exe != "" && exeInTemp(exe), Downloads: exe != "" && exeInDownloads(exe)}
}

func makeShortcut() error {
	exe, err := launcherExe()
	if err != nil {
		return err
	}
	if exeInTemp(exe) {
		return errors.New("the launcher is running from a temporary folder — move it somewhere like your Desktop or a Games folder first")
	}
	lnk := shortcutPath()
	if lnk == "" {
		return errors.New("couldn't find your desktop")
	}
	if err := createShortcut(lnk, exe); err != nil {
		return errors.New("couldn't create the shortcut")
	}
	scMu.Lock()
	scExists = true
	scMu.Unlock()
	return nil
}

func registerShortcutRoutes(mux *http.ServeMux) {
	// value=true: create the shortcut. value=false: "No thanks" — don't ask again.
	mux.HandleFunc("/api/shortcut", guard(func(w http.ResponseWriter, r *http.Request) {
		q := readReq(r)
		var err error
		if q.Value {
			err = makeShortcut()
		}
		cfgMu.Lock()
		cfg.ShortcutAsked = true
		saveConfigLocked()
		cfgMu.Unlock()
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ok"})
	}))
}
