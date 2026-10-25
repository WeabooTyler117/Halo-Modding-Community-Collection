package main

// Installing a project from its official GitHub release. The launcher never
// hosts anything itself: it reads the project's latest release, downloads the
// project's own file from github.com, checks it against the project's own
// checksum file when there is one, and unpacks it into a folder the player
// chose.

import (
	"archive/zip"
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type InstallJob struct {
	ID       string `json:"id"`
	Dir      string `json:"dir"`
	Phase    string `json:"phase"` // downloading, checking, unpacking, done, failed
	Done     int64  `json:"done"`
	Total    int64  `json:"total"`
	Version  string `json:"version"`
	Verified bool   `json:"verified"`
	Error    string `json:"error"`
	Finished bool   `json:"finished"`
}

var (
	jobMu sync.Mutex
	job   *InstallJob
)

func currentJob() *InstallJob {
	jobMu.Lock()
	defer jobMu.Unlock()
	if job == nil {
		return nil
	}
	j := *job
	return &j
}

func setJob(f func(j *InstallJob)) {
	jobMu.Lock()
	f(job)
	jobMu.Unlock()
}

func ghAPI() string {
	if v := os.Getenv("HCC_GH_API"); testHooks && v != "" { // test builds only, never on Windows
		return v
	}
	return "https://api.github.com"
}

type ghAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

type ghRelease struct {
	Tag    string    `json:"tag_name"`
	Assets []ghAsset `json:"assets"`
}

// Limits that stop a broken or hostile download from filling the disk.
const (
	maxDownload   = 4 << 30  // 4 GB for one release file
	maxUnpacked   = 16 << 30 // 16 GB unpacked
	maxZipEntries = 200000
)

// httpClient follows GitHub's redirects to its download servers, but only
// over HTTPS (test builds talk to a local test server).
var httpClient = &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("too many redirects")
	}
	if req.URL.Scheme != "https" && !testHooks {
		return errors.New("refused a non-HTTPS redirect")
	}
	return nil
}}

func ghGet(url string, v any) error {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "HaloModdingCommunityCollection/"+appVersion)
	c := &http.Client{Timeout: 20 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return errors.New("couldn't reach GitHub — check your internet connection")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 403 || resp.StatusCode == 429 {
		return errors.New("GitHub is limiting requests right now — try again in a few minutes")
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("GitHub answered %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(v)
}

// officialURL makes sure a download comes from the project's own release.
func officialURL(repo, u string) bool {
	if testHooks && os.Getenv("HCC_GH_API") != "" {
		return true
	}
	return strings.HasPrefix(u, "https://github.com/"+repo+"/releases/download/")
}

// suggestInstallDir puts a new install next to the player's other games,
// or in C:\Games when there are none.
func suggestInstallDir(g *GameDef) string {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	for _, other := range games {
		if p := cfg.Paths[other.ID]; p != "" && dirExists(p) {
			return filepath.Join(filepath.Dir(p), g.FolderName)
		}
	}
	if os.PathSeparator == '\\' {
		return filepath.Join(`C:\Games`, g.FolderName)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Games", g.FolderName)
}

func dirEmptyOrMissing(p string) bool {
	ents, err := os.ReadDir(p)
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	return err == nil && len(ents) == 0
}

func startInstall(g *GameDef, dir string) error {
	if g.AssetRe == nil || g.Repo == "" {
		return errors.New(g.Project + " can't be installed from here yet")
	}
	if err := checkGameDir(dir); err != nil {
		return err
	}
	if !dirEmptyOrMissing(dir) {
		return errors.New("that folder isn't empty — choose an empty or new folder")
	}
	jobMu.Lock()
	defer jobMu.Unlock()
	if job != nil && !job.Finished {
		return errors.New("another install is still running")
	}
	job = &InstallJob{ID: g.ID, Dir: dir, Phase: "downloading"}
	go runInstall(g, dir)
	return nil
}

func fail(err error) {
	setJob(func(j *InstallJob) { j.Phase, j.Error, j.Finished = "failed", err.Error(), true })
}

func runInstall(g *GameDef, dir string) {
	var rel ghRelease
	if err := ghGet(ghAPI()+"/repos/"+g.Repo+"/releases/latest", &rel); err != nil {
		fail(err)
		return
	}
	var asset, sums *ghAsset
	for i := range rel.Assets {
		a := &rel.Assets[i]
		if g.AssetRe.MatchString(a.Name) && asset == nil {
			asset = a
		}
		n := strings.ToLower(a.Name)
		if (strings.Contains(n, "sha256") || strings.Contains(n, "checksum")) && sums == nil {
			sums = a
		}
	}
	if asset == nil {
		fail(fmt.Errorf("the latest %s release (%s) has no Windows download the launcher recognises — get it from the project page", g.Project, rel.Tag))
		return
	}
	if !officialURL(g.Repo, asset.URL) {
		fail(errors.New("the download isn't on the project's GitHub page, so it wasn't used"))
		return
	}
	setJob(func(j *InstallJob) { j.Total, j.Version = asset.Size, rel.Tag })

	// Expected hash from the project's checksum file, when it publishes one.
	want := ""
	if sums != nil && officialURL(g.Repo, sums.URL) {
		want = expectedHash(sums.URL, asset.Name)
	}

	created := !dirExists(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fail(fmt.Errorf("couldn't create the folder: %v", err))
		return
	}
	defer func() {
		// Don't leave behind an empty folder we made for a failed install.
		if j := currentJob(); created && j != nil && j.Phase == "failed" {
			os.Remove(dir) // only succeeds when empty
		}
	}()
	part := filepath.Join(dir, ".hcc-download.part")
	got, err := download(asset.URL, part)
	if err != nil {
		os.Remove(part)
		fail(err)
		return
	}

	setJob(func(j *InstallJob) { j.Phase = "checking" })
	if want != "" {
		if !strings.EqualFold(want, got) {
			os.Remove(part)
			fail(errors.New("the download didn't match the project's checksum, so it was deleted — try again"))
			return
		}
		setJob(func(j *InstallJob) { j.Verified = true })
	}

	if g.AssetIsZip {
		setJob(func(j *InstallJob) { j.Phase = "unpacking" })
		err = unzipInto(part, dir)
		os.Remove(part)
		if err != nil {
			fail(fmt.Errorf("couldn't unpack the download: %v", err))
			return
		}
	} else {
		if err := os.Rename(part, filepath.Join(dir, asset.Name)); err != nil {
			fail(err)
			return
		}
	}

	if g.ExeMatch(dir) == "" {
		fail(errors.New("installed, but the game's program wasn't where expected — choose its folder manually"))
		return
	}
	cfgMu.Lock()
	cfg.Paths[g.ID] = dir
	saveConfigLocked()
	cfgMu.Unlock()
	setJob(func(j *InstallJob) { j.Phase, j.Finished = "done", true })
}

var sumLineRe = regexp.MustCompile(`^([0-9a-fA-F]{64})\s+\*?(.+)$`)

func expectedHash(url, name string) string {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "HaloModdingCommunityCollection/"+appVersion)
	c := &http.Client{Timeout: 30 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 1<<20))
	for sc.Scan() {
		m := sumLineRe.FindStringSubmatch(strings.TrimSpace(sc.Text()))
		if m != nil && filepath.Base(strings.TrimSpace(m[2])) == name {
			return m[1]
		}
	}
	return ""
}

type progressWriter struct{ n int64 }

func (p *progressWriter) Write(b []byte) (int, error) {
	p.n += int64(len(b))
	n := p.n
	setJob(func(j *InstallJob) { j.Done = n })
	return len(b), nil
}

func download(url, dest string) (string, error) {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "HaloModdingCommunityCollection/"+appVersion)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", errors.New("the download stopped — check your internet connection")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("the download failed (GitHub answered %d)", resp.StatusCode)
	}
	if resp.ContentLength > maxDownload {
		return "", errors.New("the download is larger than any game release should be, so it was stopped")
	}
	if resp.ContentLength > 0 {
		setJob(func(j *InstallJob) { j.Total = resp.ContentLength })
	}
	f, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h, &progressWriter{}), io.LimitReader(resp.Body, maxDownload+1))
	if err == nil && n > maxDownload {
		err = errors.New("too large")
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", errors.New("the download stopped — check your internet connection")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// unzipInto extracts a zip, dropping a single top-level folder if every entry
// shares one, and refuses entries that would land outside dir.
func unzipInto(zipPath, dir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	if len(r.File) > maxZipEntries {
		return errors.New("the archive has too many files")
	}
	var total uint64
	for _, f := range r.File {
		total += f.UncompressedSize64
		if total > maxUnpacked {
			return errors.New("the archive unpacks to more than 16 GB")
		}
	}

	prefix := ""
	first := true
	for _, f := range r.File {
		name := strings.ReplaceAll(f.Name, `\`, "/")
		top := name
		if i := strings.Index(name, "/"); i >= 0 {
			top = name[:i+1]
		} else {
			top = "" // a file at the root: nothing to strip
		}
		if first {
			prefix, first = top, false
		} else if top != prefix {
			prefix = ""
			break
		}
	}

	root := filepath.Clean(dir) + string(os.PathSeparator)
	for _, f := range r.File {
		name := strings.TrimPrefix(strings.ReplaceAll(f.Name, `\`, "/"), prefix)
		if name == "" {
			continue
		}
		if clean := strings.TrimSuffix(name, "/"); clean != "" && !filepath.IsLocal(filepath.FromSlash(clean)) {
			return fmt.Errorf("unsafe path in archive: %s", f.Name) // "..", absolute, or a reserved name like NUL or COM1
		}
		target := filepath.Join(dir, filepath.FromSlash(name))
		if !strings.HasPrefix(filepath.Clean(target)+string(os.PathSeparator), root) || filepath.Clean(target) == filepath.Clean(dir) {
			if filepath.Clean(target) == filepath.Clean(dir) {
				continue
			}
			return fmt.Errorf("unsafe path in archive: %s", f.Name)
		}
		if f.FileInfo().IsDir() || strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, io.LimitReader(rc, int64(f.UncompressedSize64)+1)) // never more than the archive claims
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
