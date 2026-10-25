//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// testHooks enables the HCC_* test overrides. Always false in Windows builds.
const testHooks = false

type proc struct {
	PID  int
	Path string
}

var (
	kernel32                       = syscall.NewLazyDLL("kernel32.dll")
	user32                         = syscall.NewLazyDLL("user32.dll")
	procQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
	procMessageBoxW                = user32.NewProc("MessageBoxW")
)

const (
	createNoWindow          = 0x08000000
	processQueryLimitedInfo = 0x1000
	th32csSnapProcess       = 0x00000002
)

// sysTool returns the full path of a built-in Windows program, so a program
// with the same name elsewhere on PATH can never run in its place.
func sysTool(rel string) string {
	root := os.Getenv("SystemRoot")
	if root == "" || !filepath.IsAbs(root) {
		root = `C:\Windows`
	}
	return filepath.Join(root, rel)
}

func hidden(cmd *exec.Cmd) *exec.Cmd {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return cmd
}

func prepareGameCmd(cmd *exec.Cmd) {}

func imagePath(pid uint32) string {
	h, err := syscall.OpenProcess(processQueryLimitedInfo, false, pid)
	if err != nil {
		return ""
	}
	defer syscall.CloseHandle(h)
	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	r, _, _ := procQueryFullProcessImageNameW.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}

func listProcesses() []proc {
	snap, err := syscall.CreateToolhelp32Snapshot(th32csSnapProcess, 0)
	if err != nil {
		return nil
	}
	defer syscall.CloseHandle(snap)
	var e syscall.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	out := []proc{}
	for err = syscall.Process32First(snap, &e); err == nil; err = syscall.Process32Next(snap, &e) {
		pid := e.ProcessID
		if pid == 0 {
			continue
		}
		out = append(out, proc{PID: int(pid), Path: imagePath(pid)})
	}
	return out
}

func closeProcesses(pids []int, force bool) {
	if len(pids) == 0 {
		return
	}
	args := []string{}
	for _, p := range pids {
		args = append(args, "/PID", strconv.Itoa(p))
	}
	args = append(args, "/T")
	if force {
		args = append(args, "/F")
	}
	hidden(exec.Command(sysTool(`System32\taskkill.exe`), args...)).Run()
}

func powershell(script string, env ...string) (string, error) {
	cmd := hidden(exec.Command(sysTool(`System32\WindowsPowerShell\v1.0\powershell.exe`), "-NoProfile", "-NonInteractive", "-STA", "-ExecutionPolicy", "Bypass", "-Command", script))
	cmd.Env = append(os.Environ(), env...)
	b, err := cmd.Output()
	return strings.TrimSpace(string(b)), err
}

func focusProcess(pids []int) {
	for _, p := range pids {
		powershell(`(New-Object -ComObject WScript.Shell).AppActivate([int]$env:HCC_PID) | Out-Null`, "HCC_PID="+strconv.Itoa(p))
	}
}

func steamPath() string {
	for _, key := range []string{`HKCU\Software\Valve\Steam`, `HKLM\SOFTWARE\WOW6432Node\Valve\Steam`, `HKLM\SOFTWARE\Valve\Steam`} {
		for _, val := range []string{"SteamPath", "InstallPath"} {
			out, err := hidden(exec.Command(sysTool(`System32\reg.exe`), "query", key, "/v", val)).Output()
			if err != nil {
				continue
			}
			for _, line := range strings.Split(string(out), "\n") {
				if i := strings.Index(line, "REG_SZ"); i >= 0 {
					p := filepath.Clean(strings.TrimSpace(line[i+len("REG_SZ"):]))
					if dirExists(p) {
						return p
					}
				}
			}
		}
	}
	for _, p := range []string{`C:\Program Files (x86)\Steam`, `C:\Program Files\Steam`} {
		if dirExists(p) {
			return p
		}
	}
	return ""
}

func candidateRoots() []string {
	var roots []string
	for c := 'C'; c <= 'Z'; c++ {
		d := string(c) + `:\`
		if !dirExists(d) {
			continue
		}
		roots = append(roots, d, d+"Games", d+"Program Files", d+"Program Files (x86)")
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, s := range []string{"Desktop", "Downloads", "Documents", "Games"} {
			roots = append(roots, filepath.Join(home, s))
		}
	}
	return roots
}

func openFolder(p string) { exec.Command(sysTool("explorer.exe"), p).Start() }

func openURL(u string) {
	hidden(exec.Command(sysTool(`System32\rundll32.exe`), "url.dll,FileProtocolHandler", u)).Start()
}

const pickScript = `
[Console]::OutputEncoding = [Text.Encoding]::UTF8
Add-Type -AssemblyName System.Windows.Forms
$owner = New-Object System.Windows.Forms.Form
$owner.TopMost = $true
$owner.ShowInTaskbar = $false
$owner.Opacity = 0
$owner.StartPosition = 'CenterScreen'
$owner.Show()
$owner.Activate()
$d = New-Object System.Windows.Forms.FolderBrowserDialog
$d.Description = $env:HCC_DESC
$d.ShowNewFolderButton = $false
if ($env:HCC_START -and (Test-Path -LiteralPath $env:HCC_START)) { $d.SelectedPath = $env:HCC_START }
if ($d.ShowDialog($owner) -eq [System.Windows.Forms.DialogResult]::OK) { [Console]::Out.Write($d.SelectedPath) }
$owner.Close()
`

const pickImageScript = `
[Console]::OutputEncoding = [Text.Encoding]::UTF8
Add-Type -AssemblyName System.Windows.Forms
$owner = New-Object System.Windows.Forms.Form
$owner.TopMost = $true
$owner.ShowInTaskbar = $false
$owner.Opacity = 0
$owner.StartPosition = 'CenterScreen'
$owner.Show()
$owner.Activate()
$d = New-Object System.Windows.Forms.OpenFileDialog
$d.Title = $env:HCC_DESC
$d.Filter = 'Images (*.jpg;*.jpeg;*.png;*.webp)|*.jpg;*.jpeg;*.png;*.webp'
if ($d.ShowDialog($owner) -eq [System.Windows.Forms.DialogResult]::OK) { [Console]::Out.Write($d.FileName) }
$owner.Close()
`

func pickImage(title string) (string, error) {
	return powershell(pickImageScript, "HCC_DESC="+title)
}

func pickFolder(title, start string) (string, error) {
	return powershell(pickScript, "HCC_DESC="+title, "HCC_START="+start)
}

func showError(title, msg string) {
	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(msg)
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), 0x10)
}

func findBrowser() string {
	var cands []string
	for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles", "LOCALAPPDATA"} {
		base := os.Getenv(env)
		if base == "" {
			continue
		}
		cands = append(cands,
			filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(base, "Google", "Chrome", "Application", "chrome.exe"),
		)
	}
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

// openWindow shows the menu in its own app window (Edge or Chrome, which
// every Windows 10/11 PC has). It returns a func that blocks until that
// window's browser process ends.
func openWindow(url string) (func(), bool) {
	b := findBrowser()
	if b == "" {
		openURL(url)
		return nil, false
	}
	profile := filepath.Join(os.Getenv("LOCALAPPDATA"), "HaloCommunityCollection", "window-v3") // new profile: Edge cached the old icon in "window"
	os.MkdirAll(profile, 0o755)
	cmd := exec.Command(b,
		"--app="+url,
		"--user-data-dir="+profile,
		"--window-size=1360,820",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-sync",
	)
	if err := cmd.Start(); err != nil {
		openURL(url)
		return nil, false
	}
	windowMu.Lock()
	windowCmd = cmd
	windowMu.Unlock()
	return func() { cmd.Wait() }, true
}

var (
	windowMu  sync.Mutex
	windowCmd *exec.Cmd
)

// closeWindow ends our app window's browser process (used before restarting
// into an updated launcher, which opens a fresh window).
func closeWindow() {
	windowMu.Lock()
	cmd := windowCmd
	windowMu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	closeProcesses([]int{cmd.Process.Pid}, true)
	time.Sleep(500 * time.Millisecond)
}

// ---------- desktop shortcut ----------

var (
	shell32                  = syscall.NewLazyDLL("shell32.dll")
	ole32                    = syscall.NewLazyDLL("ole32.dll")
	procSHGetKnownFolderPath = shell32.NewProc("SHGetKnownFolderPath")
	procCoTaskMemFree        = ole32.NewProc("CoTaskMemFree")
)

// FOLDERID_Desktop {B4BFCC3A-DB2C-424C-B029-7FE99A87C641}: the real desktop,
// even when OneDrive or the player has moved it.
var folderIDDesktop = syscall.GUID{Data1: 0xB4BFCC3A, Data2: 0xDB2C, Data3: 0x424C,
	Data4: [8]byte{0xB0, 0x29, 0x7F, 0xE9, 0x9A, 0x87, 0xC6, 0x41}}

func desktopDir() string {
	var p *uint16
	r, _, _ := procSHGetKnownFolderPath.Call(uintptr(unsafe.Pointer(&folderIDDesktop)), 0, 0, uintptr(unsafe.Pointer(&p)))
	if r != 0 || p == nil {
		return ""
	}
	defer procCoTaskMemFree.Call(uintptr(unsafe.Pointer(p)))
	s := syscall.UTF16ToString((*[1 << 15]uint16)(unsafe.Pointer(p))[:])
	if !filepath.IsAbs(s) || !dirExists(s) {
		return ""
	}
	return s
}

// Paths go to PowerShell through environment variables, never inside the
// script text, so a folder name can't change what the script does.
const makeShortcutScript = `
$s = (New-Object -ComObject WScript.Shell).CreateShortcut($env:HCC_LNK)
$s.TargetPath = $env:HCC_TARGET
$s.WorkingDirectory = $env:HCC_DIR
$s.IconLocation = $env:HCC_TARGET + ',0'
$s.Description = 'Halo Modding Community Collection'
$s.Save()
`

const readShortcutScript = `
[Console]::OutputEncoding = [Text.Encoding]::UTF8
[Console]::Out.Write((New-Object -ComObject WScript.Shell).CreateShortcut($env:HCC_LNK).TargetPath)
`

func createShortcut(lnk, target string) error {
	_, err := powershell(makeShortcutScript, "HCC_LNK="+lnk, "HCC_TARGET="+target, "HCC_DIR="+filepath.Dir(target))
	if err != nil {
		return err
	}
	if _, err := os.Stat(lnk); err != nil {
		return err
	}
	return nil
}

func readShortcutTarget(lnk string) string {
	out, err := powershell(readShortcutScript, "HCC_LNK="+lnk)
	if err != nil {
		return ""
	}
	return out
}
