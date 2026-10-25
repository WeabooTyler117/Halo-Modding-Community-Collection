package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRelease struct {
	tag      string
	exe      []byte
	sums     string // "" = no SHA256SUMS.txt asset
	exeURL   string // override the exe download URL
	restarts []string
}

func (f *fakeRelease) serve(t *testing.T) *httptest.Server {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dl := srv.URL + "/" + selfRepo + "/releases/download/" + f.tag + "/"
		switch {
		case r.URL.Path == "/repos/"+selfRepo+"/releases/latest":
			exeURL := dl + selfAsset
			if f.exeURL != "" {
				exeURL = f.exeURL
			}
			assets := []ghAsset{{Name: selfAsset, URL: exeURL, Size: int64(len(f.exe))}}
			if f.sums != "" {
				assets = append(assets, ghAsset{Name: selfSums, URL: dl + selfSums})
			}
			json.NewEncoder(w).Encode(map[string]any{"tag_name": f.tag, "html_url": srv.URL + "/rel", "assets": assets})
		case strings.HasSuffix(r.URL.Path, "/"+selfAsset):
			w.Write(f.exe)
		case strings.HasSuffix(r.URL.Path, "/"+selfSums):
			w.Write([]byte(f.sums))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// setupUpdate points the updater at a fake GitHub and a fake installed exe.
func setupUpdate(t *testing.T, f *fakeRelease) (exe string) {
	srv := f.serve(t)
	t.Setenv("HCC_GH_API", srv.URL)
	dir := t.TempDir()
	exe = filepath.Join(dir, selfAsset)
	os.WriteFile(exe, []byte("MZ old launcher"), 0o755)
	oldExe, oldRestart, oldVer := selfExePath, restartSelf, appVersion
	selfExePath = func() (string, error) { return exe, nil }
	restartSelf = func(p string) error { f.restarts = append(f.restarts, p); return nil }
	appVersion = "0.3.4"
	upd = SelfUpdate{}
	t.Cleanup(func() { selfExePath, restartSelf, appVersion = oldExe, oldRestart, oldVer; upd = SelfUpdate{} })
	return exe
}

func sumsFor(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]) + "  " + selfAsset + "\n"
}

func TestSelfUpdateInstallsAndRestarts(t *testing.T) {
	newExe := []byte("MZ new launcher v0.3.5")
	f := &fakeRelease{tag: "v0.3.5", exe: newExe, sums: sumsFor(newExe)}
	exe := setupUpdate(t, f)
	if err := runSelfUpdate(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != string(newExe) {
		t.Fatalf("exe not replaced: %q", b)
	}
	if b, _ := os.ReadFile(exe + ".old"); string(b) != "MZ old launcher" {
		t.Fatalf("old exe not kept as .old: %q", b)
	}
	if len(f.restarts) != 1 || f.restarts[0] != exe {
		t.Fatalf("restart not called with the new exe: %v", f.restarts)
	}
	cleanupAfterUpdate()
	if _, err := os.Stat(exe + ".old"); !os.IsNotExist(err) {
		t.Fatal(".old not cleaned up")
	}
}

func expectRefused(t *testing.T, f *fakeRelease, wantErr string) {
	t.Helper()
	exe := setupUpdate(t, f)
	err := runSelfUpdate()
	if err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("want error containing %q, got %v", wantErr, err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "MZ old launcher" {
		t.Fatalf("exe changed after a refused update: %q", b)
	}
	if len(f.restarts) != 0 {
		t.Fatal("restarted after a refused update")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(exe), updateTmp)); !os.IsNotExist(err) {
		t.Fatal("temporary download left behind")
	}
}

func TestSelfUpdateRefusesBadChecksum(t *testing.T) {
	newExe := []byte("MZ new launcher v0.3.5")
	expectRefused(t, &fakeRelease{tag: "v0.3.5", exe: newExe, sums: sumsFor([]byte("something else"))}, "checksum")
}

func TestSelfUpdateRefusesMissingChecksum(t *testing.T) {
	expectRefused(t, &fakeRelease{tag: "v0.3.5", exe: []byte("MZ new")}, "missing its files")
}

func TestSelfUpdateRefusesChecksumWithoutOurFile(t *testing.T) {
	expectRefused(t, &fakeRelease{tag: "v0.3.5", exe: []byte("MZ new"), sums: strings.Repeat("a", 64) + "  other.exe\n"}, "no checksum")
}

func TestSelfUpdateRefusesForeignURL(t *testing.T) {
	newExe := []byte("MZ new launcher")
	expectRefused(t, &fakeRelease{tag: "v0.3.5", exe: newExe, sums: sumsFor(newExe), exeURL: "https://evil.example/x.exe"}, "GitHub page")
}

func TestSelfUpdateRefusesNonProgram(t *testing.T) {
	notExe := []byte("<html>not a program</html>")
	expectRefused(t, &fakeRelease{tag: "v0.3.5", exe: notExe, sums: sumsFor(notExe)}, "isn't a Windows program")
}

func TestSelfUpdateRefusesSameVersion(t *testing.T) {
	newExe := []byte("MZ same")
	expectRefused(t, &fakeRelease{tag: "v0.3.4", exe: newExe, sums: sumsFor(newExe)}, "newest version")
}

func TestSelfUpdateRollsBackWhenRestartFails(t *testing.T) {
	newExe := []byte("MZ new launcher v0.3.5")
	f := &fakeRelease{tag: "v0.3.5", exe: newExe, sums: sumsFor(newExe)}
	exe := setupUpdate(t, f)
	restartSelf = func(string) error { return os.ErrPermission }
	if err := runSelfUpdate(); err == nil {
		t.Fatal("expected an error")
	}
	if b, _ := os.ReadFile(exe); string(b) != "MZ old launcher" {
		t.Fatalf("old exe not restored: %q", b)
	}
}

func TestSelfOfficialURL(t *testing.T) {
	os.Unsetenv("HCC_GH_API")
	ok := "https://github.com/WeabooTyler117/Halo-Modding-Community-Collection/releases/download/v1/x.exe"
	if !selfOfficialURL(ok) || !selfOfficialURL(strings.ToLower(ok)) {
		t.Fatal("official URL rejected")
	}
	for _, u := range []string{
		"http://github.com/WeabooTyler117/Halo-Modding-Community-Collection/releases/download/v1/x.exe",
		"https://github.com/WeabooTyler117/Halo-Modding-Community-Collection-evil/releases/download/v1/x.exe",
		"https://github.com/someone/Halo-Modding-Community-Collection/releases/download/v1/x.exe",
		"https://github.com.evil.com/WeabooTyler117/Halo-Modding-Community-Collection/releases/download/v1/x.exe",
	} {
		if selfOfficialURL(u) {
			t.Fatal("accepted", u)
		}
	}
}
