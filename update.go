package main

// Updating the launcher itself from its own GitHub release. Nothing happens
// until the player clicks "Update now". The new exe must come from this
// project's own release page, must match the SHA-256 in the release's
// SHA256SUMS.txt (no checksum file, no update), and must look like a Windows
// program. The running exe is renamed to <exe>.old, the new one takes its
// place, and the launcher restarts itself. If any step fails, the old exe is
// put back.

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	selfRepo     = "WeabooTyler117/Halo-Modding-Community-Collection"
	selfAsset    = "Halo-Modding-Community-Collection.exe"
	selfSums     = "SHA256SUMS.txt"
	maxSelfExe   = 200 << 20 // no launcher build is anywhere near this
	updateTmp    = ".hcc-update.tmp"
	afterUpdateF = "--after-update"
)

type SelfUpdate struct {
	Available bool   `json:"available"`
	Version   string `json:"version"`
	URL       string `json:"url"`
	Phase     string `json:"phase"` // "", downloading, checking, restarting, failed
	Done      int64  `json:"done"`
	Total     int64  `json:"total"`
	Error     string `json:"error"`
	Updated   bool   `json:"updated"` // this process was started by an update
}

var (
	updMu       sync.Mutex
	upd         SelfUpdate
	justUpdated bool
)

// selfDownloadPrefix is the only place a launcher update may come from.
func selfDownloadPrefix() string {
	if v := os.Getenv("HCC_GH_API"); testHooks && v != "" { // test builds only
		return strings.TrimRight(v, "/") + "/" + selfRepo + "/releases/download/"
	}
	return "https://github.com/" + selfRepo + "/releases/download/"
}

func selfOfficialURL(u string) bool {
	return strings.HasPrefix(strings.ToLower(u), strings.ToLower(selfDownloadPrefix()))
}

// updateState is what the page sees: whether a newer release exists and how
// an update in progress is going.
func updateState() SelfUpdate {
	updMu.Lock()
	s := upd
	s.Updated = justUpdated
	updMu.Unlock()
	if s.Phase == "" {
		if r, ok := getRelease(selfRepo); ok && r.Tag != "" && compareVersions(r.Tag, appVersion) > 0 {
			s.Available, s.Version, s.URL = true, r.Tag, r.URL
		}
	}
	return s
}

func setUpd(f func(u *SelfUpdate)) {
	updMu.Lock()
	f(&upd)
	updMu.Unlock()
}

func startSelfUpdate() error {
	if j := currentJob(); j != nil && !j.Finished {
		return errors.New("wait for the game install to finish first")
	}
	updMu.Lock()
	defer updMu.Unlock()
	if upd.Phase != "" && upd.Phase != "failed" {
		return errors.New("the update is already running")
	}
	upd = SelfUpdate{Phase: "downloading"}
	go func() {
		if err := runSelfUpdate(); err != nil {
			log.Println("update failed:", err)
			setUpd(func(u *SelfUpdate) { u.Phase, u.Error = "failed", err.Error() })
		}
	}()
	return nil
}

// Swappable for tests.
var (
	selfExePath = func() (string, error) {
		p, err := os.Executable()
		if err != nil {
			return "", err
		}
		return filepath.EvalSymlinks(p)
	}
	restartSelf = restartIntoNewExe
)

func runSelfUpdate() error {
	var rel ghRelease
	if err := ghGet(ghAPI()+"/repos/"+selfRepo+"/releases/latest", &rel); err != nil {
		return err
	}
	if compareVersions(rel.Tag, appVersion) <= 0 {
		return errors.New("you already have the newest version")
	}
	var exeA, sumA *ghAsset
	for i := range rel.Assets {
		switch rel.Assets[i].Name {
		case selfAsset:
			exeA = &rel.Assets[i]
		case selfSums:
			sumA = &rel.Assets[i]
		}
	}
	if exeA == nil || sumA == nil {
		return fmt.Errorf("the %s release is missing its files — download it from the project page", rel.Tag)
	}
	if !selfOfficialURL(exeA.URL) || !selfOfficialURL(sumA.URL) {
		return errors.New("the update isn't on the project's GitHub page, so it wasn't used")
	}
	if exeA.Size > maxSelfExe {
		return errors.New("the update is far larger than the launcher should be, so it wasn't used")
	}
	want := expectedHash(sumA.URL, selfAsset)
	if want == "" {
		return errors.New("the update has no checksum to check it against, so it wasn't used")
	}
	setUpd(func(u *SelfUpdate) { u.Version, u.Total = rel.Tag, exeA.Size })

	exe, err := selfExePath()
	if err != nil {
		return errors.New("couldn't find the launcher's own file")
	}
	dir := filepath.Dir(exe)
	tmp := filepath.Join(dir, updateTmp)
	os.Remove(tmp)
	got, err := download(exeA.URL, tmp, maxSelfExe, func(done, total int64) {
		setUpd(func(u *SelfUpdate) {
			u.Done = done
			if total > 0 {
				u.Total = total
			}
		})
	})
	if err != nil {
		os.Remove(tmp)
		if strings.Contains(err.Error(), "denied") {
			return errors.New("couldn't save the update next to the launcher — move the launcher to a folder you own (like Desktop or C:\\Games)")
		}
		return err
	}

	setUpd(func(u *SelfUpdate) { u.Phase = "checking" })
	if !strings.EqualFold(got, want) {
		os.Remove(tmp)
		return errors.New("the update didn't match its checksum, so it was deleted")
	}
	if !looksLikeProgram(tmp) {
		os.Remove(tmp)
		return errors.New("the update isn't a Windows program, so it was deleted")
	}

	old := exe + ".old"
	os.Remove(old) // a leftover from an earlier update
	if err := os.Rename(exe, old); err != nil {
		os.Remove(tmp)
		return errors.New("couldn't replace the launcher file — download the new version from the project page")
	}
	if err := os.Rename(tmp, exe); err != nil {
		os.Rename(old, exe) // put the old one back
		os.Remove(tmp)
		return errors.New("couldn't replace the launcher file — download the new version from the project page")
	}
	os.Chmod(exe, 0o755) // matters off Windows only

	setUpd(func(u *SelfUpdate) { u.Phase = "restarting" })
	time.Sleep(800 * time.Millisecond) // let the page show "Restarting…"
	if err := restartSelf(exe); err != nil {
		// Couldn't start the new version: undo the swap so the next start works.
		os.Rename(exe, tmp)
		if os.Rename(old, exe) == nil {
			os.Remove(tmp)
		}
		return errors.New("couldn't start the new version, so the old one was kept")
	}
	return nil
}

// looksLikeProgram checks the file starts like a Windows exe ("MZ"). Test
// builds off Windows also accept the local program format.
func looksLikeProgram(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	b := make([]byte, 4)
	if _, err := io.ReadFull(f, b); err != nil {
		return false
	}
	if b[0] == 'M' && b[1] == 'Z' {
		return true
	}
	return testHooks && string(b) == "\x7fELF"
}

func restartIntoNewExe(exe string) error {
	cmd := exec.Command(exe, afterUpdateF)
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err != nil {
		return err
	}
	log.Println("update installed, restarting")
	cmd.Process.Release()
	closeWindow()
	os.Exit(0)
	return nil
}

// cleanupAfterUpdate removes what the previous version left behind. The old
// process may take a moment to let go of its file, so it retries.
func cleanupAfterUpdate() {
	exe, err := selfExePath()
	if err != nil {
		return
	}
	os.Remove(filepath.Join(filepath.Dir(exe), updateTmp))
	old := exe + ".old"
	for i := 0; i < 20; i++ {
		if err := os.Remove(old); err == nil || errors.Is(err, os.ErrNotExist) {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func hasArg(flag string) bool {
	for _, a := range os.Args[1:] {
		if a == flag {
			return true
		}
	}
	return false
}

func registerUpdateRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/self-update", guard(func(w http.ResponseWriter, r *http.Request) {
		if err := startSelfUpdate(); err != nil {
			writeJSON(w, 409, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"status": "started"})
	}))
	mux.HandleFunc("/api/self-update-dismiss", guard(func(w http.ResponseWriter, r *http.Request) {
		setUpd(func(u *SelfUpdate) {
			if u.Phase == "failed" {
				*u = SelfUpdate{}
			}
		})
		writeJSON(w, 200, map[string]string{"status": "ok"})
	}))
}
