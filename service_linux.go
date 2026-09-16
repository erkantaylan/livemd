//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const systemdUnitName = "livemd.service"

func systemdUnitPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "systemd", "user", systemdUnitName)
}

func systemctlUser(args ...string) (string, error) {
	out, err := exec.Command("systemctl", append([]string{"--user"}, args...)...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// systemdUserAvailable fails on systems without systemd, and where systemd
// runs but this session cannot reach a user manager (WSL without systemd,
// some containers and ssh sessions).
func systemdUserAvailable() error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf("systemctl not found; autostart on Linux needs systemd")
	}
	if out, err := systemctlUser("show-environment"); err != nil {
		return fmt.Errorf("cannot reach the systemd user manager: %s", out)
	}
	return nil
}

func platformServiceInstall(exe string) error {
	if err := systemdPathSupported(exe); err != nil {
		return err
	}
	if err := systemdUserAvailable(); err != nil {
		return err
	}
	path := systemdUnitPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(systemdUnit(exe)), 0644); err != nil {
		return err
	}
	if out, err := systemctlUser("daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %s", out)
	}

	// A daemon started by hand holds the lock, and the unit's `livemd start`
	// would exit "already running" and leave the unit failed.
	if !serviceActive() {
		stopRunningDaemon()
	}
	if out, err := systemctlUser("enable", systemdUnitName); err != nil {
		return fmt.Errorf("systemctl enable: %s", out)
	}
	// restart, not start: a reinstall may have changed ExecStart.
	if out, err := systemctlUser("restart", systemdUnitName); err != nil {
		return fmt.Errorf("systemctl restart: %s", out)
	}
	return nil
}

func platformServiceUninstall() (bool, error) {
	path := systemdUnitPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false, nil
	}
	if err := systemdUserAvailable(); err != nil {
		return false, err
	}
	if out, err := systemctlUser("disable", "--now", systemdUnitName); err != nil {
		return false, fmt.Errorf("systemctl disable: %s", out)
	}
	if err := os.Remove(path); err != nil {
		return false, err
	}
	systemctlUser("daemon-reload")
	systemctlUser("reset-failed", systemdUnitName) // clears a failed state; errors when there is none
	return true, nil
}

func platformServiceStatus() (serviceStatus, error) {
	path := systemdUnitPath()
	st := serviceStatus{Mechanism: "systemd user unit", Location: path}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	st.Installed = true
	if exe, err := serviceExecutable(); err == nil {
		st.Current = string(data) == systemdUnit(exe)
	}
	if err := systemdUserAvailable(); err != nil {
		return st, err
	}
	out, _ := systemctlUser("is-enabled", systemdUnitName)
	st.Enabled = out == "enabled"
	return st, nil
}

// serviceManaged reports whether systemd owns the daemon's lifecycle: the unit
// exists and systemctl can reach it. Not when this process runs inside the unit,
// so a hand-written unit whose command uses --detach cannot ask systemd to
// start itself. (INVOCATION_ID can't tell: desktop terminals are systemd
// scopes, and every shell in them inherits it.)
func serviceManaged() bool {
	if cg, err := os.ReadFile("/proc/self/cgroup"); err == nil &&
		strings.Contains(string(cg), "/"+systemdUnitName+"\n") {
		return false
	}
	if _, err := os.Stat(systemdUnitPath()); err != nil {
		return false
	}
	return systemdUserAvailable() == nil
}

func serviceActive() bool {
	out, _ := systemctlUser("is-active", systemdUnitName)
	return out == "active"
}

func serviceStart() error {
	if out, err := systemctlUser("start", systemdUnitName); err != nil {
		return fmt.Errorf("systemctl start: %s", out)
	}
	return nil
}

func serviceStop() error {
	if out, err := systemctlUser("stop", systemdUnitName); err != nil {
		return fmt.Errorf("systemctl stop: %s", out)
	}
	return nil
}
