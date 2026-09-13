package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Autostart: `livemd service install` makes the daemon start at login.
//
//   - Linux: a systemd user unit running the foreground `livemd start`, with
//     Restart=on-failure. systemd owns the process, so `start --detach` and
//     `livemd install` hand start/stop to systemctl while the unit exists.
//   - Windows: a value under HKCU\...\CurrentVersion\Run that runs
//     `livemd start --detach` at logon. No admin rights needed, unlike a
//     logon-triggered scheduled task. Nothing supervises the process, so the
//     usual detach/relaunch paths still apply; the console window of that
//     short-lived `--detach` parent can flash at logon.
//
// The platform files provide the functions below; other systems get
// errServiceUnsupported.

var errServiceUnsupported = errors.New("autostart is supported on Linux (systemd) and Windows only")

// serviceStatus is what `livemd service status` reports.
type serviceStatus struct {
	Mechanism string // human name of the autostart mechanism
	Location  string // unit file path or registry value
	Installed bool
	Enabled   bool // false when installed but switched off (systemctl disable, Task Manager)
	Current   bool // installed entry matches what this binary would write
}

func cmdService() {
	usage := func() {
		fmt.Fprintln(os.Stderr, `Usage:
  livemd service install     Start livemd automatically when you log in
  livemd service uninstall   Remove autostart and stop the daemon
  livemd service status      Show whether autostart is installed and livemd is running`)
	}
	if len(os.Args) < 3 {
		usage()
		os.Exit(1)
	}

	var err error
	switch os.Args[2] {
	case "install":
		err = serviceInstallCmd()
	case "uninstall":
		err = serviceUninstallCmd()
	case "status":
		err = serviceStatusCmd()
	default:
		fmt.Fprintf(os.Stderr, "Unknown service command: %s\n", os.Args[2])
		usage()
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func serviceInstallCmd() error {
	exe, err := serviceExecutable()
	if err != nil {
		return err
	}
	if err := platformServiceInstall(exe); err != nil {
		return err
	}
	st, err := platformServiceStatus()
	if err != nil {
		return err
	}
	fmt.Printf("Installed autostart (%s)\n  %s\n", st.Mechanism, st.Location)
	fmt.Println("  livemd now starts when you log in.")

	// systemd started the unit already. A logon entry only fires at the next
	// logon, so start the daemon now unless one is running.
	if !serviceManaged() {
		if port, err := readLockFile(); err != nil || !daemonReachable(port) {
			return relaunchDaemon() // prints the addresses itself
		}
	}
	port, ok := waitForDaemon(5 * time.Second)
	if !ok {
		return fmt.Errorf("autostart is installed, but the daemon did not come up; see 'livemd service status'")
	}
	fmt.Println()
	printServerAddresses(port)
	return nil
}

func serviceUninstallCmd() error {
	removed, err := platformServiceUninstall()
	if err != nil {
		return err
	}
	if !removed {
		fmt.Println("Autostart is not installed.")
		return nil
	}
	fmt.Println("Removed autostart and stopped livemd.")
	fmt.Println("  Run 'livemd start --detach' to run it without autostart.")
	return nil
}

func serviceStatusCmd() error {
	st, err := platformServiceStatus()
	if err != nil {
		return err
	}
	fmt.Printf("Autostart: %s\n", st.Mechanism)
	if !st.Installed {
		fmt.Println("  Installed: no ('livemd service install' to add it)")
	} else {
		fmt.Printf("  Installed: yes, %s\n", st.Location)
		fmt.Printf("  Enabled:   %s\n", yesNo(st.Enabled))
		if !st.Current {
			fmt.Println("  Note:      the entry differs from what this binary would write;")
			fmt.Println("             run 'livemd service install' to update it.")
		}
	}
	if port, err := readLockFile(); err == nil && daemonReachable(port) {
		fmt.Printf("  Running:   yes, port %d\n", port)
	} else {
		fmt.Println("  Running:   no")
	}
	return nil
}

// serviceExecutable is the path autostart should run: this binary, with
// symlinks resolved so the entry survives the link being repointed.
func serviceExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot determine executable path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

// waitForDaemon polls until a lock file names a port that accepts connections.
func waitForDaemon(timeout time.Duration) (int, bool) {
	deadline := time.Now().Add(timeout)
	for {
		if port, err := readLockFile(); err == nil && daemonReachable(port) {
			return port, true
		}
		if time.Now().After(deadline) {
			return 0, false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// systemdUnit renders the user unit for exe. The foreground `start` is what
// systemd supervises; `--detach` would fork away from it.
func systemdUnit(exe string) string {
	return fmt.Sprintf(`[Unit]
Description=LiveMD live markdown viewer

[Service]
Type=simple
ExecStart=%s start
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
`, systemdQuote(exe))
}

// systemdQuote quotes the ExecStart executable path. Quotes keep a path with
// spaces in one piece, and %-specifiers are doubled. $VARIABLES are expanded
// only in arguments, not in the executable path, where "$$" would stay a
// literal double dollar. Paths systemd refuses outright are rejected by
// systemdPathSupported before this runs.
func systemdQuote(s string) string {
	return `"` + strings.ReplaceAll(s, "%", "%%") + `"`
}

// systemdPathSupported rejects executable paths systemd will not start,
// however they are escaped ("Executable name contains special characters").
func systemdPathSupported(exe string) error {
	for _, r := range exe {
		if r == '"' || r == '\\' || r < 0x20 || r == 0x7f {
			return fmt.Errorf("systemd cannot run a binary whose path contains %q: %s — move livemd to a plain directory and run this again", r, exe)
		}
	}
	return nil
}

// windowsRunCommand is the Run-key command line for exe. Windows paths cannot
// contain double quotes, so quoting the path is enough.
func windowsRunCommand(exe string) string {
	return `"` + exe + `" start --detach`
}
