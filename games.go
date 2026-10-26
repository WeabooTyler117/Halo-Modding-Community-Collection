package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// GameDef describes one community project. Nothing here is copied from the
// projects themselves; the launcher only finds and starts what the player
// has already installed from each project's official release page.
type GameDef struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Short        string   `json:"short"`
	Project      string   `json:"project"`
	Year         string   `json:"year"`
	Tagline      string   `json:"tagline"`
	Accent       string   `json:"accent"`
	Repo         string   `json:"repo"`        // owner/name of the releases repo, "" if none
	ProjectURL   string   `json:"projectUrl"`  // where players get it
	DownloadURL  string   `json:"downloadUrl"` // release page
	CommunityURL string   `json:"communityUrl"`
	Highlights   []string `json:"highlights"`
	Notes        []string `json:"notes"`
	ClosedBeta   bool     `json:"closedBeta"`
	// GetViaDiscord: the download is a file posted in the project's Discord
	// (CommunityURL is the invite, DownloadURL the message with the file).
	GetViaDiscord bool   `json:"getViaDiscord"`
	Requires      string `json:"requires"` // shown in bold until the game is fully set up
	// Installing from the official GitHub release.
	AssetRe      *regexp.Regexp `json:"-"` // which release file to download
	AssetIsZip   bool           `json:"-"`
	FolderName   string         `json:"-"` // suggested install folder name
	MCCFolder    string         `json:"-"` // folder inside the MCC install this project relies on, "" if not MCC-based
	MCCName      string         `json:"-"`
	MCCExtra     []MCCPart      `json:"-"` // optional MCC content (e.g. ODST), shown but not required
	DefaultPaths []string       `json:"-"`
	// Install describes the official release download, nil if the launcher
	// can't install this game.
	Install *InstallSpec `json:"-"`
	// ExeMatch picks the game's executable inside its folder.
	ExeMatch func(dir string) string `json:"-"`
	// Version reads the installed version, "" when unknown.
	Version func(dir, exe string) string `json:"-"`
}

// MCCPart is optional content inside the MCC install.
type MCCPart struct{ Folder, Name, Detail string }

// InstallSpec says which file in a project's latest GitHub release is the
// Windows download and how to unpack it.
type InstallSpec struct {
	DirName string                 // folder created under the chosen location
	Asset   func(name string) bool // picks the release file
	Zip     bool                   // zip to extract; otherwise a single exe
}

func assetPrefix(prefix, ext string, exclude ...string) func(string) bool {
	return func(n string) bool {
		l := strings.ToLower(n)
		for _, x := range exclude {
			if strings.Contains(l, x) {
				return false
			}
		}
		return strings.HasPrefix(l, strings.ToLower(prefix)) && strings.HasSuffix(l, ext)
	}
}

var games = []*GameDef{
	{
		ID:           "reach",
		Title:        "HALO: REACH",
		Short:        "Reach",
		Project:      "Reach Renaissance",
		Year:         "2010",
		Tagline:      "Multiplayer, Firefight and Campaign with the Xbox 360 menus, online without opening MCC.",
		Accent:       "#e0873a",
		Repo:         "NETF1ix/reach-renaissance-releases",
		ProjectURL:   "https://github.com/NETF1ix/reach-renaissance-releases",
		DownloadURL:  "https://github.com/NETF1ix/reach-renaissance-releases/releases/latest",
		Highlights:   []string{"Xbox 360 menus", "Campaign, Firefight & multiplayer online", "Public server list, no router setup", "Updates offered at start-up"},
		Notes:        []string{"Needs MCC on Steam with Halo: Reach installed (Store / Game Pass not supported).", "After an MCC update it won't start until a new release supports it.", "Everyone in a lobby needs the same Reach Renaissance version."},
		MCCFolder:    "haloreach",
		MCCName:      "Halo: Reach",
		DefaultPaths: []string{`D:\ReachRenaissance`, `C:\ReachRenaissance`, `C:\Games\ReachRenaissance`},
		ExeMatch:     exactExe("Reach Renaissance.exe"),
		AssetRe:      regexp.MustCompile(`(?i)^ReachRenaissance-v[\d.]+\.zip$`),
		AssetIsZip:   true,
		FolderName:   "ReachRenaissance",
		Install:      &InstallSpec{DirName: "ReachRenaissance", Asset: assetPrefix("ReachRenaissance-v", ".zip"), Zip: true},
		Version: func(dir, exe string) string {
			b, err := os.ReadFile(filepath.Join(dir, "VERSION.txt"))
			if err != nil {
				return ""
			}
			m := regexp.MustCompile(`\d+\.\d+\.\d+`).Find(b)
			return string(m)
		},
	},
	{
		ID:           "ce",
		Title:        "HALO: COMBAT EVOLVED",
		Short:        "Combat Evolved",
		Project:      "OpenCE",
		Year:         "2001",
		Tagline:      "The original, ported natively to PC with huge lobbies, online co-op and Custom Edition maps.",
		Accent:       "#5fbf8f",
		Repo:         "OpenCommunityEdition/OpenCE",
		ProjectURL:   "https://github.com/OpenCommunityEdition/OpenCE",
		DownloadURL:  "https://github.com/OpenCommunityEdition/OpenCE/releases/latest",
		CommunityURL: "https://discord.gg/9gqcHyr5km",
		Highlights:   []string{"Up to 128 players", "Online co-op campaign", "Server browser & invite links", "Custom Edition maps download automatically", "Cross-play with Linux, Android & browser"},
		Notes:        []string{"Not MCC-based: it uses your own Halo CE Xbox disc image (.iso / .xiso).", "Updates itself at start-up."},
		DefaultPaths: []string{`D:\Halo Open CE`, `D:\OpenCE`, `C:\OpenCE`, `C:\Games\OpenCE`},
		Requires:     "You must have your own copy of the Xbox ISO file for Halo: Combat Evolved.",
		ExeMatch:     exactExe("halo.exe"),
		AssetRe:      regexp.MustCompile(`(?i)^halo-windows-release\.zip$`),
		AssetIsZip:   true,
		FolderName:   "OpenCE",
		Install:      &InstallSpec{DirName: "OpenCE", Asset: assetPrefix("halo-windows-release", ".zip"), Zip: true},
		Version: func(dir, exe string) string {
			// The release zip doesn't carry a version file; the game updates itself.
			return ""
		},
	},
	{
		ID:           "h2",
		Title:        "HALO 2",
		Short:        "Halo 2",
		Project:      "Project Gravemind",
		Year:         "2004",
		Tagline:      "MCC's Halo 2 as its own game, with matchmaking playlists and the original 1–50 ranks.",
		Accent:       "#6f8cff",
		ProjectURL:   "https://briroz1.github.io/project-gravemind/index.html",
		Repo:         "BriRoz1/h2gravemind-beta",
		DownloadURL:  "https://github.com/BriRoz1/h2gravemind-beta/releases/latest",
		CommunityURL: "https://discord.gg/7nAmnwnQqb",
		Highlights:   []string{"Matchmaking with Halo 2 ranks", "Dedicated servers, up to 24 players", "Online co-op, up to 6", "Steam Workshop mods"},
		Notes:        []string{"Uses your Steam copy of MCC with Halo 2 installed.", "Beta: updates itself when it starts."},
		// The release zip holds h2gravemind.exe plus its small helper files.
		AssetRe:      regexp.MustCompile(`(?i)^h2gravemind.*\.zip$`),
		AssetIsZip:   true,
		FolderName:   "Project Gravemind",
		MCCFolder:    "halo2",
		MCCName:      "Halo 2",
		DefaultPaths: []string{`D:\ProjectGravemind`, `D:\Project Gravemind`, `D:\Gravemind`, `C:\ProjectGravemind`},
		ExeMatch:     gravemindExe,
		Version:      func(dir, exe string) string { return versionFromName(exe) },
	},
	{
		ID:          "h3",
		Title:       "HALO 3 + ODST",
		Short:       "Halo 3",
		Project:     "Project Reclaimer",
		Year:        "2007 · 2009",
		Tagline:     "MCC's Halo 3 and Halo 3: ODST outside MCC, with dedicated servers and an in-game updater.",
		Accent:      "#d8b44a",
		Repo:        "ProjectReclaimer/project-reclaimer-releases",
		ProjectURL:  "https://projectreclaimer.dev",
		DownloadURL: "https://github.com/ProjectReclaimer/project-reclaimer-releases/releases/latest",
		Highlights:  []string{"Halo 3: ODST campaign & Firefight, co-op up to 16", "Dedicated servers with RCON tools", "In-game updater keeps settings, maps & mods", "Finds Halo 3 in any Steam library"},
		Notes: []string{"Needs MCC on Steam with Halo 3 installed.",
			"Halo 3: ODST is optional: to play it you must own Halo 3: ODST on Steam and install it in MCC (Steam → MCC → Properties → DLC).",
			"Logs: Documents\\My Games\\Project Reclaimer\\Logs."},
		MCCFolder:    "halo3",
		MCCName:      "Halo 3",
		MCCExtra:     []MCCPart{{Folder: "halo3odst", Name: "Halo 3: ODST", Detail: "Only needed to play ODST: you must own Halo 3: ODST on Steam, then turn it on in MCC's DLC settings in Steam."}},
		DefaultPaths: []string{`D:\RECLAIMER`, `D:\ProjectReclaimer`, `C:\ProjectReclaimer`, `C:\Games\ProjectReclaimer`},
		ExeMatch:     reclaimerExe,
		AssetRe:      regexp.MustCompile(`(?i)^project-reclaimer(-client)?-v[\d.]+\.exe$`),
		FolderName:   "ProjectReclaimer",
		Install:      &InstallSpec{DirName: "ProjectReclaimer", Asset: assetPrefix("project-reclaimer", ".exe", "rcon"), Zip: false},
		Version:      func(dir, exe string) string { return versionFromName(exe) },
	},
}

func gameByID(id string) *GameDef {
	for _, g := range games {
		if g.ID == id {
			return g
		}
	}
	return nil
}

func exactExe(name string) func(string) string {
	return func(dir string) string {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
		return ""
	}
}

var verRe = regexp.MustCompile(`v?(\d+)\.(\d+)\.(\d+)`)

func versionFromName(path string) string {
	m := verRe.FindStringSubmatch(filepath.Base(path))
	if m == nil {
		return ""
	}
	return m[1] + "." + m[2] + "." + m[3]
}

// compareVersions returns -1, 0, 1. Unparseable versions compare as equal.
func compareVersions(a, b string) int {
	ma, mb := verRe.FindStringSubmatch(a), verRe.FindStringSubmatch(b)
	if ma == nil || mb == nil {
		return 0
	}
	for i := 1; i <= 3; i++ {
		x, _ := strconv.Atoi(ma[i])
		y, _ := strconv.Atoi(mb[i])
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func listExes(dir string) []string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".exe") {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	return out
}

func isHelperExe(name string) bool {
	n := strings.ToLower(name)
	for _, bad := range []string{"unins", "setup", "install", "rcon", "crash", "update", "redist", "vc_", "dxsetup"} {
		if strings.Contains(n, bad) {
			return true
		}
	}
	return false
}

// reclaimerExe picks the newest Project Reclaimer client in the folder.
// The in-game updater may leave older copies next to the new one.
func reclaimerExe(dir string) string {
	var cands []string
	for _, p := range listExes(dir) {
		n := strings.ToLower(filepath.Base(p))
		if strings.HasPrefix(n, "project-reclaimer") && !isHelperExe(n) {
			cands = append(cands, p)
		}
	}
	if len(cands) == 0 {
		return ""
	}
	sort.SliceStable(cands, func(i, j int) bool {
		vi, vj := versionFromName(cands[i]), versionFromName(cands[j])
		if (vi == "") != (vj == "") {
			return vi != "" // a versioned file beats an unversioned one
		}
		c := compareVersions(vi, vj)
		if c != 0 {
			return c > 0
		}
		si, _ := os.Stat(cands[i])
		sj, _ := os.Stat(cands[j])
		return si != nil && sj != nil && si.ModTime().After(sj.ModTime())
	})
	return cands[0]
}

// gravemindExe finds Project Gravemind's program: the largest exe whose name
// mentions Gravemind. It never picks an unrelated program from the folder.
func gravemindExe(dir string) string {
	best, bestSize := "", int64(-1)
	for _, p := range listExes(dir) {
		n := strings.ToLower(filepath.Base(p))
		if isHelperExe(n) || !strings.Contains(n, "gravemind") {
			continue
		}
		if st, err := os.Stat(p); err == nil && st.Size() > bestSize {
			best, bestSize = p, st.Size()
		}
	}
	return best
}
