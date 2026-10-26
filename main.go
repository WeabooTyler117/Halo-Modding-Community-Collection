package main

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

//go:embed ui
var uiFS embed.FS

const (
	appName = "Halo Modding Community Collection"
	port    = 47231
)

// appVersion is a var so test builds can set it with -ldflags -X main.appVersion=...
var appVersion = "0.3.7"

// ---------- config ----------

type Config struct {
	Paths         map[string]string `json:"paths"`
	LastPlayed    string            `json:"lastPlayed"`
	ShortcutAsked bool              `json:"shortcutAsked"`
}

var (
	cfgMu sync.Mutex
	cfg   = Config{Paths: map[string]string{}}
)

func dataDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	d := filepath.Join(base, "HaloCommunityCollection")
	os.MkdirAll(d, 0o755)
	return d
}

func loadConfig() {
	b, err := os.ReadFile(filepath.Join(dataDir(), "config.json"))
	if err == nil {
		json.Unmarshal(b, &cfg)
	}
	if cfg.Paths == nil {
		cfg.Paths = map[string]string{}
	}
	for id, p := range cfg.Paths {
		if gameByID(id) == nil || checkGameDir(p) != nil {
			delete(cfg.Paths, id) // never treat a drive or shared folder as a game folder
		}
	}
}

func saveConfigLocked() {
	b, _ := json.MarshalIndent(cfg, "", "  ")
	tmp := filepath.Join(dataDir(), "config.json.tmp")
	if os.WriteFile(tmp, b, 0o644) == nil {
		os.Rename(tmp, filepath.Join(dataDir(), "config.json"))
	}
}

// ---------- detection ----------

var scanKeywords = map[string][]string{
	"reach": {"reach"},
	"ce":    {"opence", "open ce", "halo ce", "halo open ce", "combat evolved"},
	"h2":    {"gravemind"},
	"h3":    {"reclaimer"},
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// autoDetect fills in paths for games that have none yet. It only looks in
// a few shallow, likely places so start-up stays fast.
func autoDetect() {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	changed := false
	var roots []string
	for _, d := range candidateRoots() {
		if dirExists(d) {
			roots = append(roots, d)
		}
	}
	for _, g := range games {
		if p := cfg.Paths[g.ID]; p != "" && g.ExeMatch(p) != "" {
			continue
		}
		found := ""
		for _, p := range g.DefaultPaths {
			if dirExists(p) && checkGameDir(p) == nil && g.ExeMatch(p) != "" {
				found = p
				break
			}
		}
		if found == "" {
			found = scanFor(g, roots)
		}
		if found != "" && checkGameDir(found) == nil && cfg.Paths[g.ID] != found {
			cfg.Paths[g.ID] = found
			changed = true
		}
	}
	if changed {
		saveConfigLocked()
	}
}

func scanFor(g *GameDef, roots []string) string {
	kws := scanKeywords[g.ID]
	match := func(name string) bool {
		n := strings.ToLower(name)
		for _, k := range kws {
			if strings.Contains(n, k) {
				return true
			}
		}
		return false
	}
	for _, r := range roots {
		ents, err := os.ReadDir(r)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if !e.IsDir() {
				continue
			}
			p := filepath.Join(r, e.Name())
			if match(e.Name()) && g.ExeMatch(p) != "" {
				return p
			}
			// one level deeper (e.g. a zip extracted into a same-named folder)
			if match(e.Name()) {
				sub, _ := os.ReadDir(p)
				for _, s := range sub {
					if s.IsDir() && g.ExeMatch(filepath.Join(p, s.Name())) != "" {
						return filepath.Join(p, s.Name())
					}
				}
			}
		}
	}
	return ""
}

// ---------- Steam / MCC ----------

type mccInfo struct {
	SteamFound bool
	MCCDir     string
	at         time.Time
}

var (
	mccMu    sync.Mutex
	mccCache mccInfo
)

var vdfPathRe = regexp.MustCompile(`"path"\s+"([^"]+)"`)
var acfDirRe = regexp.MustCompile(`"installdir"\s+"([^"]+)"`)

func getMCC() mccInfo {
	mccMu.Lock()
	defer mccMu.Unlock()
	if time.Since(mccCache.at) < 60*time.Second {
		return mccCache
	}
	info := mccInfo{at: time.Now()}
	steam := steamPath()
	if steam != "" {
		info.SteamFound = true
		libs := []string{steam}
		if b, err := os.ReadFile(filepath.Join(steam, "steamapps", "libraryfolders.vdf")); err == nil {
			for _, m := range vdfPathRe.FindAllStringSubmatch(string(b), -1) {
				libs = append(libs, strings.ReplaceAll(m[1], `\\`, `\`))
			}
		}
		for _, lib := range libs {
			acf := filepath.Join(lib, "steamapps", "appmanifest_976730.acf") // MCC's Steam app id
			b, err := os.ReadFile(acf)
			if err != nil {
				continue
			}
			dir := "Halo The Master Chief Collection"
			if m := acfDirRe.FindSubmatch(b); m != nil {
				dir = string(m[1])
			}
			p := filepath.Join(lib, "steamapps", "common", dir)
			if dirExists(p) {
				info.MCCDir = p
				break
			}
		}
	}
	mccCache = info
	return info
}

// ---------- GitHub latest release ----------

type Release struct {
	Tag       string    `json:"tag"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Published time.Time `json:"published"`
	Fetched   time.Time `json:"fetched"`
}

var (
	relMu      sync.Mutex
	releases   = map[string]Release{}
	relLoading = map[string]bool{}
)

func loadReleaseCache() {
	b, err := os.ReadFile(filepath.Join(dataDir(), "releases.json"))
	if err == nil {
		json.Unmarshal(b, &releases)
	}
}

func getRelease(repo string) (Release, bool) {
	relMu.Lock()
	defer relMu.Unlock()
	r, ok := releases[repo]
	if (!ok || time.Since(r.Fetched) > 30*time.Minute) && !relLoading[repo] {
		relLoading[repo] = true
		go fetchRelease(repo)
	}
	return r, ok
}

func fetchRelease(repo string) {
	defer func() {
		relMu.Lock()
		relLoading[repo] = false
		relMu.Unlock()
	}()
	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequest("GET", ghAPI()+"/repos/"+repo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "HaloModdingCommunityCollection/"+appVersion)
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		// Rate-limited or offline: keep the old value but don't retry for a while.
		relMu.Lock()
		r := releases[repo]
		r.Fetched = time.Now().Add(-20 * time.Minute)
		if r.Tag != "" {
			releases[repo] = r
		}
		relMu.Unlock()
		return
	}
	var gh struct {
		TagName     string    `json:"tag_name"`
		Name        string    `json:"name"`
		HTMLURL     string    `json:"html_url"`
		PublishedAt time.Time `json:"published_at"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&gh) != nil {
		return
	}
	relMu.Lock()
	releases[repo] = Release{Tag: gh.TagName, Name: gh.Name, URL: gh.HTMLURL, Published: gh.PublishedAt, Fetched: time.Now()}
	b, _ := json.Marshal(releases)
	relMu.Unlock()
	os.WriteFile(filepath.Join(dataDir(), "releases.json"), b, 0o644)
}

// ---------- running games ----------

var (
	runMu     sync.Mutex
	launched  = map[string]int{}    // game id -> pid we started (while alive)
	quickExit = map[string]string{} // game id -> program that closed right after Play
)

// quickExitWindow: a game that closes this soon after Play, with nothing of
// it left running, almost certainly didn't start properly (often Windows
// Defender's Controlled folder access blocking it from saving settings).
const quickExitWindow = 15 * time.Second

func underDir(path, dir string) bool {
	if path == "" || dir == "" {
		return false
	}
	p := strings.ToLower(filepath.Clean(path))
	d := strings.ToLower(filepath.Clean(dir)) + string(filepath.Separator)
	return strings.HasPrefix(p, d)
}

// runningPIDs maps game id -> PIDs of that game's processes.
func runningPIDs() map[string][]int {
	procs := listProcesses()
	self := os.Getpid()
	cfgMu.Lock()
	paths := map[string]string{}
	for k, v := range cfg.Paths {
		paths[k] = v
	}
	cfgMu.Unlock()
	out := map[string][]int{}
	alive := map[int]bool{}
	for _, p := range procs {
		alive[p.PID] = true
		if p.PID == self {
			continue
		}
		for _, g := range games {
			if belongsToGame(p.Path, paths[g.ID]) {
				out[g.ID] = append(out[g.ID], p.PID)
			}
		}
	}
	runMu.Lock()
	for id, pid := range launched {
		if procs != nil && !alive[pid] {
			delete(launched, id)
			continue
		}
		found := false
		for _, x := range out[id] {
			if x == pid {
				found = true
			}
		}
		if !found {
			out[id] = append(out[id], pid)
		}
	}
	runMu.Unlock()
	return out
}

// ---------- state ----------

type Check struct {
	Label    string `json:"label"`
	OK       bool   `json:"ok"`
	Warn     bool   `json:"warn"`
	Detail   string `json:"detail"`
	Optional bool   `json:"optional"`
}

type GameState struct {
	*GameDef
	Path       string   `json:"path"`
	Exe        string   `json:"exe"`
	ExeName    string   `json:"exeName"`
	Installed  bool     `json:"installed"`
	Version    string   `json:"version"`
	Latest     *Release `json:"latest"`
	UpdateMsg  string   `json:"updateMsg"`
	Running    bool     `json:"running"`
	Checks     []Check  `json:"checks"`
	QuickExit  string   `json:"quickExit"`
	WrongDir   string   `json:"wrongDir"`
	Online     *Online  `json:"online"`
	Art        string   `json:"art"`
	ArtCustom  bool     `json:"artCustom"`
	CanInstall bool     `json:"canInstall"`
	SuggestDir string   `json:"suggestDir"`
}

func buildState() map[string]any {
	running := runningPIDs()
	mcc := getMCC()
	cfgMu.Lock()
	paths := map[string]string{}
	for k, v := range cfg.Paths {
		paths[k] = v
	}
	last := cfg.LastPlayed
	cfgMu.Unlock()

	var list []GameState
	runningID := ""
	for _, g := range games {
		s := GameState{GameDef: g, Path: paths[g.ID]}
		if s.Path != "" {
			s.Exe = g.ExeMatch(s.Path)
		}
		s.Installed = s.Exe != ""
		if !s.Installed && s.Path != "" && g.WrongFolder != nil {
			s.WrongDir = g.WrongFolder(s.Path)
		}
		if s.Installed {
			s.ExeName = filepath.Base(s.Exe)
			s.Version = g.Version(s.Path, s.Exe)
		}
		if g.Repo != "" {
			if r, ok := getRelease(g.Repo); ok && r.Tag != "" {
				rr := r
				s.Latest = &rr
				if s.Installed && s.Version != "" {
					switch compareVersions(s.Version, r.Tag) {
					case -1:
						s.UpdateMsg = "Update available — the game offers it when it starts."
					default:
						s.UpdateMsg = "Up to date."
					}
				}
			}
		}
		s.Online = getOnline(g.ID)
		s.Art, s.ArtCustom = artURL(g.ID)
		s.CanInstall = g.AssetRe != nil && g.Repo != "" && !g.ClosedBeta
		if !s.Installed && s.CanInstall {
			s.SuggestDir = suggestInstallDir(g)
		}
		s.Running = len(running[g.ID]) > 0
		if !s.Running {
			runMu.Lock()
			s.QuickExit = quickExit[g.ID]
			runMu.Unlock()
		}
		if s.Running {
			runningID = g.ID
		}

		// requirement checks
		if g.MCCFolder != "" {
			switch {
			case mcc.MCCDir != "":
				s.Checks = append(s.Checks, Check{Label: "MCC on Steam", OK: true, Detail: mcc.MCCDir})
				if dirExists(filepath.Join(mcc.MCCDir, g.MCCFolder)) {
					s.Checks = append(s.Checks, Check{Label: g.MCCName + " installed in MCC", OK: true})
				} else {
					s.Checks = append(s.Checks, Check{Label: g.MCCName + " installed in MCC", Warn: true, Detail: "Not found — install it from MCC's content settings in Steam."})
				}
				for _, x := range g.MCCExtra {
					c := Check{Label: x.Name + " in MCC", Optional: true}
					if dirExists(filepath.Join(mcc.MCCDir, x.Folder)) {
						c.OK = true
					} else {
						c.Detail = x.Detail
					}
					s.Checks = append(s.Checks, c)
				}
			case mcc.SteamFound:
				s.Checks = append(s.Checks, Check{Label: "MCC on Steam", Warn: true, Detail: "Steam found, but not MCC."})
			default:
				s.Checks = append(s.Checks, Check{Label: "MCC on Steam", Warn: true, Detail: "Steam not found."})
			}
		}
		if g.ID == "ce" && s.Installed {
			hasImage := false
			for _, ext := range []string{"*.iso", "*.xiso", "*.ISO", "*.XISO"} {
				if m, _ := filepath.Glob(filepath.Join(s.Path, ext)); len(m) > 0 {
					hasImage = true
				}
			}
			maps, _ := os.ReadDir(filepath.Join(s.Path, "maps"))
			switch {
			case len(maps) > 0:
				s.Checks = append(s.Checks, Check{Label: "Game maps extracted", OK: true})
			case hasImage:
				s.Checks = append(s.Checks, Check{Label: "Disc image found", OK: true, Detail: "Maps are extracted on first start."})
			default:
				s.Checks = append(s.Checks, Check{Label: "Halo CE disc image", Warn: true, Detail: "The game asks for your .iso / .xiso on first start."})
			}
		}
		list = append(list, s)
	}
	return map[string]any{
		"app":        appName,
		"version":    appVersion,
		"games":      list,
		"running":    runningID,
		"lastPlayed": last,
		"job":        currentJob(),
		"shortcut":   shortcutState(),
		"update":     updateState(),
	}
}

// ---------- actions ----------

func launchGame(id string) error {
	g := gameByID(id)
	if g == nil {
		return errors.New("unknown game")
	}
	cfgMu.Lock()
	dir := cfg.Paths[id]
	cfgMu.Unlock()
	exe := ""
	if dir != "" && checkGameDir(dir) == nil {
		exe = g.ExeMatch(dir)
	}
	if exe == "" {
		return errors.New("game not found — choose its folder first")
	}
	cmd := exec.Command(exe)
	cmd.Dir = dir
	prepareGameCmd(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	pid := cmd.Process.Pid
	started := time.Now()
	runMu.Lock()
	launched[id] = pid
	delete(quickExit, id)
	runMu.Unlock()
	go focusGameWhenReady(id)
	go func() {
		cmd.Wait()
		quick := time.Since(started) < quickExitWindow
		runMu.Lock()
		if launched[id] == pid {
			delete(launched, id)
		}
		runMu.Unlock()
		if !quick {
			return
		}
		// Some games hand over to a second program; only warn when nothing
		// of the game is still running a moment later.
		time.Sleep(3 * time.Second)
		if len(runningPIDs()[id]) == 0 {
			log.Printf("%s closed %s after starting", id, time.Since(started).Round(time.Second))
			runMu.Lock()
			if launched[id] == 0 {
				quickExit[id] = filepath.Base(exe)
			}
			runMu.Unlock()
		}
	}()
	cfgMu.Lock()
	cfg.LastPlayed = id
	saveConfigLocked()
	cfgMu.Unlock()
	return nil
}

func closeGame(id string) error {
	pids := runningPIDs()[id]
	if len(pids) == 0 {
		return nil
	}
	closeProcesses(pids, false)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		if len(runningPIDs()[id]) == 0 {
			return nil
		}
	}
	closeProcesses(runningPIDs()[id], true)
	time.Sleep(time.Second)
	if len(runningPIDs()[id]) > 0 {
		return errors.New("the game didn't close")
	}
	return nil
}

// ---------- HTTP ----------

var (
	token    string
	lastPing = time.Now()
	pingMu   sync.Mutex
)

func newToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Only our own page may call the API: right host (blocks DNS
		// rebinding) and the per-session token (blocks other web pages).
		host := r.Host
		if host != fmt.Sprintf("127.0.0.1:%d", port) && host != fmt.Sprintf("localhost:%d", port) {
			http.Error(w, "forbidden", 403)
			return
		}
		t := r.Header.Get("X-HCC-Token")
		if t == "" && r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/art/") {
			t = r.URL.Query().Get("t") // <img> and CSS can't send headers
		}
		if !tokenOK(t) {
			http.Error(w, "forbidden", 403)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") && !readOnlyAPI[r.URL.Path] && r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		pingMu.Lock()
		lastPing = time.Now()
		pingMu.Unlock()
		next(w, r)
	}
}

type idReq struct {
	ID      string `json:"id"`
	Replace bool   `json:"replace"`
	URL     string `json:"url"`
	Dir     string `json:"dir"`
	Value   bool   `json:"value"`
}

func readReq(r *http.Request) idReq {
	var q idReq
	json.NewDecoder(http.MaxBytesReader(nil, r.Body, 4096)).Decode(&q)
	return q
}

const usageRulesURL = "https://www.xbox.com/en-US/developers/rules"

func allowedURL(u string) bool {
	if u == usageRulesURL {
		return true
	}
	for _, g := range games {
		for _, x := range []string{g.ProjectURL, g.DownloadURL, g.CommunityURL} {
			if x != "" && x == u {
				return true
			}
		}
		if strings.HasPrefix(u, "https://github.com/"+selfRepo+"/releases/") {
			return true
		}
		if g.Repo != "" && strings.HasPrefix(u, "https://github.com/"+g.Repo+"/") {
			return true
		}
	}
	return false
}

func routes() *http.ServeMux {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(uiFS, "ui")
	files := http.FileServer(http.FS(sub))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		files.ServeHTTP(w, r)
	})

	mux.HandleFunc("/api/state", guard(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, buildState())
	}))

	mux.HandleFunc("/api/launch", guard(func(w http.ResponseWriter, r *http.Request) {
		q := readReq(r)
		running := runningPIDs()
		for id, pids := range running {
			if id == q.ID || len(pids) == 0 {
				continue
			}
			if !q.Replace {
				writeJSON(w, 409, map[string]string{"running": id})
				return
			}
			if err := closeGame(id); err != nil {
				writeJSON(w, 500, map[string]string{"error": "Couldn't close " + gameByID(id).Short + ": " + err.Error()})
				return
			}
		}
		if len(running[q.ID]) > 0 {
			focusProcess(running[q.ID])
			writeJSON(w, 200, map[string]string{"status": "already running"})
			return
		}
		if err := launchGame(q.ID); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"status": "launched"})
	}))

	mux.HandleFunc("/api/close", guard(func(w http.ResponseWriter, r *http.Request) {
		q := readReq(r)
		if err := closeGame(q.ID); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"status": "closed"})
	}))

	mux.HandleFunc("/api/folder", guard(func(w http.ResponseWriter, r *http.Request) {
		q := readReq(r)
		cfgMu.Lock()
		p := cfg.Paths[q.ID]
		cfgMu.Unlock()
		if p == "" || !dirExists(p) {
			writeJSON(w, 404, map[string]string{"error": "No folder set"})
			return
		}
		openFolder(p)
		writeJSON(w, 200, map[string]string{"status": "ok"})
	}))

	mux.HandleFunc("/api/pick", guard(func(w http.ResponseWriter, r *http.Request) {
		q := readReq(r)
		g := gameByID(q.ID)
		if g == nil {
			writeJSON(w, 400, map[string]string{"error": "unknown game"})
			return
		}
		cfgMu.Lock()
		start := cfg.Paths[q.ID]
		cfgMu.Unlock()
		p, err := pickFolder("Choose the "+g.Project+" folder", start)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if p == "" {
			writeJSON(w, 200, map[string]string{"status": "cancelled"})
			return
		}
		if err := checkGameDir(p); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		if g.ExeMatch(p) == "" {
			msg := "Couldn't find " + g.Project + " in that folder."
			if g.WrongFolder != nil {
				if why := g.WrongFolder(p); why != "" {
					msg = why
				}
			}
			writeJSON(w, 400, map[string]string{"error": msg})
			return
		}
		cfgMu.Lock()
		cfg.Paths[q.ID] = p
		saveConfigLocked()
		cfgMu.Unlock()
		writeJSON(w, 200, map[string]string{"status": "ok"})
	}))

	mux.HandleFunc("/api/forget", guard(func(w http.ResponseWriter, r *http.Request) {
		q := readReq(r)
		if gameByID(q.ID) == nil {
			writeJSON(w, 400, map[string]string{"error": "unknown game"})
			return
		}
		cfgMu.Lock()
		delete(cfg.Paths, q.ID)
		saveConfigLocked()
		cfgMu.Unlock()
		writeJSON(w, 200, map[string]string{"status": "ok"})
	}))

	mux.HandleFunc("/api/open-url", guard(func(w http.ResponseWriter, r *http.Request) {
		q := readReq(r)
		if !allowedURL(q.URL) {
			writeJSON(w, 400, map[string]string{"error": "not allowed"})
			return
		}
		openURL(q.URL)
		writeJSON(w, 200, map[string]string{"status": "ok"})
	}))

	mux.HandleFunc("/api/quick-exit-dismiss", guard(func(w http.ResponseWriter, r *http.Request) {
		q := readReq(r)
		runMu.Lock()
		delete(quickExit, q.ID)
		runMu.Unlock()
		writeJSON(w, 200, map[string]string{"status": "ok"})
	}))

	mux.HandleFunc("/api/ping", guard(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok"})
	}))

	mux.HandleFunc("/api/quit", guard(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "bye"})
		go func() { time.Sleep(300 * time.Millisecond); os.Exit(0) }()
	}))
	registerExtraRoutes(mux)
	registerShortcutRoutes(mux)
	registerUpdateRoutes(mux)
	return mux
}

func main() {
	log.SetFlags(log.LstdFlags)
	if f, err := os.OpenFile(filepath.Join(dataDir(), "launcher.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644); err == nil {
		log.SetOutput(f)
	}
	sessionFile := filepath.Join(dataDir(), "session")

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil && hasArg(afterUpdateF) {
		// Started by an update: the old version is still letting go of the port.
		for i := 0; i < 40 && err != nil; i++ {
			time.Sleep(250 * time.Millisecond)
			ln, err = net.Listen("tcp", addr)
		}
	}
	if err != nil {
		// Already running: open another window on the running launcher.
		if b, e := os.ReadFile(sessionFile); e == nil {
			url := fmt.Sprintf("http://127.0.0.1:%d/?t=%s", port, strings.TrimSpace(string(b)))
			if wait, ok := openWindow(url); ok {
				wait()
			}
			return
		}
		showError(appName, "Another program is using port 47231, so the launcher can't start.")
		return
	}

	if hasArg(afterUpdateF) {
		justUpdated = true
	}
	go cleanupAfterUpdate()

	token = newToken()
	os.WriteFile(sessionFile, []byte(token), 0o600)
	loadConfig()
	loadReleaseCache()
	autoDetect()
	adoptArt()
	checkShortcut()
	getRelease(selfRepo)
	for _, g := range games {
		if g.Repo != "" {
			getRelease(g.Repo)
		}
	}

	srv := &http.Server{Handler: secureHeaders(routes()), ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 64 << 10}
	go srv.Serve(ln)

	url := fmt.Sprintf("http://127.0.0.1:%d/?t=%s", port, token)
	log.Println("launcher", appVersion, "started")
	if os.Getenv("HCC_NO_WINDOW") != "" {
		select {}
	}
	limit := 3 * time.Minute // default browser: tabs in the background ping slowly
	if wait, ok := openWindow(url); ok {
		wait() // the app window's process ended
		limit = 20 * time.Second
	}
	// Quit once the page has stopped checking in (the window was closed).
	for {
		time.Sleep(5 * time.Second)
		pingMu.Lock()
		idle := time.Since(lastPing)
		pingMu.Unlock()
		if idle > limit {
			return
		}
	}
}
