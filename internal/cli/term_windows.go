//go:build windows

package cli

import (
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

var (
	kernel32                       = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode             = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode             = kernel32.NewProc("SetConsoleMode")
	enableVirtualTerminalProcessing uint32 = 0x0004
)

func isTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	var mode uint32
	r1, _, _ := procGetConsoleMode.Call(f.Fd(), uintptr(unsafe.Pointer(&mode)))
	return r1 != 0
}

func enableVirtualTerminal() {
	for _, f := range []*os.File{os.Stdout, os.Stderr} {
		var mode uint32
		r1, _, _ := procGetConsoleMode.Call(f.Fd(), uintptr(unsafe.Pointer(&mode)))
		if r1 != 0 {
			_, _, _ = procSetConsoleMode.Call(f.Fd(), uintptr(mode|enableVirtualTerminalProcessing))
		}
	}
}

func queryOSC11Background() (string, bool) {
	// Windows Terminal sets COLORFGBG or supports ANSI; avoid blocking console input handles.
	return "", false
}

func openBrowser(url string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
