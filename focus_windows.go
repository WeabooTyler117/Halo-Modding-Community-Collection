//go:build windows

package main

// Bringing a game to the front. Windows only lets the program you're using
// put a new window in front of you. The game is started by the launcher's
// background process, not by the menu window you clicked, so Windows leaves
// the game behind the menu. Once the game's window exists, the launcher
// minimizes its own window and hands the game the focus.

import (
	"syscall"
	"time"
	"unsafe"
)

var (
	procEnumWindows              = user32.NewProc("EnumWindows")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procIsIconic                 = user32.NewProc("IsIconic")
	procGetWindow                = user32.NewProc("GetWindow")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetClassNameW            = user32.NewProc("GetClassNameW")
	procGetWindowRect            = user32.NewProc("GetWindowRect")
	procShowWindow               = user32.NewProc("ShowWindow")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procBringWindowToTop         = user32.NewProc("BringWindowToTop")
	procKeybdEvent               = user32.NewProc("keybd_event")
)

const (
	gwOwner         = 4
	swMinimize      = 6
	swRestore       = 9
	vkMenu          = 0x12
	keyeventfKeyUp  = 0x0002
	menuWindowTitle = "Halo: MCC" // the menu page's <title>
	chromeWinClass  = "Chrome_WidgetWin_1"
)

type winRect struct{ Left, Top, Right, Bottom int32 }

func windowText(h uintptr, proc *syscall.LazyProc) string {
	buf := make([]uint16, 256)
	n, _, _ := proc.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:n])
}

// topWindows lists visible, unowned top-level windows with their PIDs.
func topWindows() map[uintptr]uint32 {
	out := map[uintptr]uint32{}
	cb := syscall.NewCallback(func(h, _ uintptr) uintptr {
		if v, _, _ := procIsWindowVisible.Call(h); v == 0 {
			return 1
		}
		if o, _, _ := procGetWindow.Call(h, gwOwner); o != 0 {
			return 1
		}
		var pid uint32
		procGetWindowThreadProcessId.Call(h, uintptr(unsafe.Pointer(&pid)))
		out[h] = pid
		return 1
	})
	procEnumWindows.Call(cb, 0)
	return out
}

// gameWindow finds the main window of any of the given processes.
func gameWindow(pids []int) uintptr {
	want := map[uint32]bool{}
	for _, p := range pids {
		want[uint32(p)] = true
	}
	for h, pid := range topWindows() {
		if !want[pid] {
			continue
		}
		var r winRect
		procGetWindowRect.Call(h, uintptr(unsafe.Pointer(&r)))
		if r.Right-r.Left < 200 || r.Bottom-r.Top < 150 { // skip tiny helper windows
			continue
		}
		return h
	}
	return 0
}

// menuWindows finds the launcher's own menu window(s).
func menuWindows() []uintptr {
	var out []uintptr
	for h := range topWindows() {
		if windowText(h, procGetClassNameW) == chromeWinClass && windowText(h, procGetWindowTextW) == menuWindowTitle {
			out = append(out, h)
		}
	}
	return out
}

func bringToFront(h uintptr) {
	if ic, _, _ := procIsIconic.Call(h); ic != 0 {
		procShowWindow.Call(h, swRestore)
	}
	// A tap of Alt lets this process move the focus (Windows' own rule for
	// programs the player is interacting with).
	procKeybdEvent.Call(vkMenu, 0, 0, 0)
	procKeybdEvent.Call(vkMenu, 0, keyeventfKeyUp, 0)
	procBringWindowToTop.Call(h)
	procSetForegroundWindow.Call(h)
}

// focusGameWhenReady waits (up to 90 s) for the game's window, then
// minimizes the menu and brings the game to the front.
func focusGameWhenReady(id string) {
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(700 * time.Millisecond)
		pids := runningPIDs()[id]
		if len(pids) == 0 {
			continue // still starting, or it closed (the quick-exit hint covers that)
		}
		if h := gameWindow(pids); h != 0 {
			time.Sleep(400 * time.Millisecond) // let the window finish appearing
			for _, m := range menuWindows() {
				procShowWindow.Call(m, swMinimize)
			}
			bringToFront(h)
			return
		}
	}
}

// focusProcess brings an already-running game back to the front
// ("Return to game").
func focusProcess(pids []int) {
	if h := gameWindow(pids); h != 0 {
		for _, m := range menuWindows() {
			procShowWindow.Call(m, swMinimize)
		}
		bringToFront(h)
	}
}
