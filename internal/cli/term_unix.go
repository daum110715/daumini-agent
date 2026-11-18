//go:build !windows

package cli

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

func isTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	_, err := unix.IoctlGetTermios(int(f.Fd()), ioctlReadTermios())
	return err == nil
}

func enableVirtualTerminal() {}

func queryOSC11Background() (rgb string, ok bool) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", false
	}
	defer tty.Close()

	fd := int(tty.Fd())
	orig, err := unix.IoctlGetTermios(fd, ioctlReadTermios())
	if err != nil {
		return "", false
	}
	raw := *orig
	raw.Lflag &^= unix.ICANON | unix.ECHO
	raw.Cc[unix.VMIN] = 0
	raw.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, ioctlWriteTermios(), &raw); err != nil {
		return "", false
	}
	defer func() {
		_ = unix.IoctlSetTermios(fd, ioctlWriteTermios(), orig)
	}()

	_, _ = tty.WriteString("\x1b]11;?\x1b\\")

	deadline := time.Now().Add(55 * time.Millisecond)
	var buf []byte
	tmp := make([]byte, 64)
	for time.Now().Before(deadline) {
		pfd := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		n, _ := unix.Poll(pfd, 15)
		if n > 0 && (pfd[0].Revents&unix.POLLIN) != 0 {
			m, _ := tty.Read(tmp)
			if m > 0 {
				buf = append(buf, tmp[:m]...)
				s := string(buf)
				if strings.Contains(s, "\x07") || strings.Contains(s, "\x1b\\") {
					return s, true
				}
			}
		}
	}
	if len(buf) > 0 {
		return string(buf), true
	}
	return "", false
}

func ioctlReadTermios() uint {
	if runtime.GOOS == "darwin" || runtime.GOOS == "freebsd" {
		return 0x40487413 // TIOCGETA fallback when not using constants
	}
	return unix.TCGETS
}

func ioctlWriteTermios() uint {
	if runtime.GOOS == "darwin" || runtime.GOOS == "freebsd" {
		return 0x80487414 // TIOCSETA
	}
	return unix.TCSETS
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
