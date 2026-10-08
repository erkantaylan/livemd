//go:build !windows

package main

import "os/exec"

// hiddenCommand is exec.Command; only Windows needs to suppress a console window.
func hiddenCommand(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}
