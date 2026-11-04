package ccmeter

import (
	"os"
	"syscall"
)

const enableVirtualTerminalProcessing = 0x0004

// enableTerminalColor turns on ANSI handling in the console, and reports false
// on consoles too old to support it, so they get plain text.
func enableTerminalColor() bool {
	h := syscall.Handle(os.Stdout.Fd())
	var mode uint32
	if err := syscall.GetConsoleMode(h, &mode); err != nil {
		return false
	}
	if mode&enableVirtualTerminalProcessing != 0 {
		return true
	}
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("SetConsoleMode")
	ok, _, _ := proc.Call(uintptr(h), uintptr(mode|enableVirtualTerminalProcessing))
	return ok != 0
}
