//go:build !windows

package main

// Stand-ins so the launcher can be run and tested off Windows.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// testHooks enables the HCC_* test overrides. Always false in Windows builds.
const testHooks = true

type proc struct {
	PID  int
	Path string
}

func prepareGameCmd(cmd *exec.Cmd) {}

func listProcesses() []proc {
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	out := []proc{}
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		p, _ := os.Readlink(filepath.Join("/proc", e.Name(), "exe"))
		p = strings.TrimSuffix(p, " (deleted)")
		out = append(out, proc{PID: pid, Path: p})
	}
	return out
}

func closeProcesses(pids []int, force bool) {
	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	for _, p := range pids {
		syscall.Kill(p, sig)
	}
}

func focusProcess(pids []int) {}

func steamPath() string {
	if p := os.Getenv("HCC_STEAM"); p != "" {
		return p
	}
	return ""
}

func candidateRoots() []string {
	if r := os.Getenv("HCC_ROOT"); r != "" {
		return []string{r}
	}
	return nil
}

func openFolder(p string) {}
func openURL(u string)    {}
func pickFolder(title, start string) (string, error) {
	if p := os.Getenv("HCC_PICK_DIR"); p != "" {
		return p, nil
	}
	return "", errors.New("folder picker is Windows-only")
}
func pickImage(title string) (string, error) {
	if p := os.Getenv("HCC_TEST_IMAGE"); p != "" {
		return p, nil
	}
	return "", errors.New("image picker is Windows-only")
}
func showError(title, msg string)          { println(title + ": " + msg) }
func openWindow(url string) (func(), bool) { println(url); return nil, false }

// Shortcut stand-ins for tests: a plain text file holding the target path.
func desktopDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	d := filepath.Join(home, "Desktop")
	if !dirExists(d) {
		return ""
	}
	return d
}

func createShortcut(lnk, target string) error { return os.WriteFile(lnk, []byte(target), 0o644) }

func readShortcutTarget(lnk string) string {
	b, _ := os.ReadFile(lnk)
	return strings.TrimSpace(string(b))
}
