//go:build windows

package main

import (
	"fmt"

	"golang.org/x/sys/windows/registry"
)

const (
	runKeyPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName = "livemd"
	// Task Manager's Startup tab records its on/off switch here, separately
	// from the Run value itself.
	startupApprovedPath = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`
)

func platformServiceInstall(exe string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open HKCU\\%s: %w", runKeyPath, err)
	}
	defer key.Close()
	if err := key.SetStringValue(runValueName, windowsRunCommand(exe)); err != nil {
		return fmt.Errorf("write Run value: %w", err)
	}
	// Installing means "start at login": clear a Task Manager "Disabled".
	deleteStartupApproval()
	return nil
}

func platformServiceUninstall() (bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return false, nil
		}
		return false, fmt.Errorf("open HKCU\\%s: %w", runKeyPath, err)
	}
	defer key.Close()
	if err := key.DeleteValue(runValueName); err != nil {
		if err == registry.ErrNotExist {
			return false, nil
		}
		return false, fmt.Errorf("delete Run value: %w", err)
	}
	deleteStartupApproval()
	stopRunningDaemon()
	return true, nil
}

func platformServiceStatus() (serviceStatus, error) {
	st := serviceStatus{
		Mechanism: "logon entry",
		Location:  `HKCU\` + runKeyPath + `\` + runValueName,
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return st, nil
		}
		return st, err
	}
	defer key.Close()
	value, _, err := key.GetStringValue(runValueName)
	if err == registry.ErrNotExist {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	st.Installed = true
	st.Enabled = !disabledInTaskManager()
	if exe, err := serviceExecutable(); err == nil {
		st.Current = value == windowsRunCommand(exe)
	}
	return st, nil
}

// disabledInTaskManager reads the Startup tab's switch: a binary value whose
// first byte is even while enabled (02, 06) and odd once switched off (03).
func disabledInTaskManager() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, startupApprovedPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	data, _, err := key.GetBinaryValue(runValueName)
	return err == nil && len(data) > 0 && data[0]&1 == 1
}

func deleteStartupApproval() {
	key, err := registry.OpenKey(registry.CURRENT_USER, startupApprovedPath, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer key.Close()
	key.DeleteValue(runValueName)
}

// Nothing supervises a Run-key daemon; the detach and relaunch paths apply.
func serviceManaged() bool { return false }
func serviceActive() bool  { return false }
func serviceStart() error  { return errServiceUnsupported }
func serviceStop() error   { return errServiceUnsupported }
