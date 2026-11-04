//go:build !windows

package ccmeter

import (
	"os"
	"os/exec"
	"syscall"
)

// detach starts the child in its own session so it survives the terminal closing.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// terminate asks the process to exit cleanly.
func terminate(p *os.Process) error { return p.Signal(syscall.SIGTERM) }
