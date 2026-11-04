//go:build windows

package ccmeter

import (
	"os"
	"os/exec"
	"syscall"
)

const (
	detachedProcess       = 0x00000008
	createNewProcessGroup = 0x00000200
	createNoWindow        = 0x08000000
)

// detach starts the child with no console window, independent of this one.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: detachedProcess | createNewProcessGroup | createNoWindow,
		HideWindow:    true,
	}
}

// terminate stops the process; Windows has no SIGTERM for console-less processes.
func terminate(p *os.Process) error { return p.Kill() }
