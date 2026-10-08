//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

// hiddenCommand is exec.Command that never opens a console window. The daemon
// runs detached with no console, so a console child such as git would
// otherwise get a fresh window that flashes on screen.
func hiddenCommand(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNoWindow,
		HideWindow:    true,
	}
	return cmd
}
