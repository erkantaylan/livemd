package main

import (
	"strings"
	"testing"
)

// systemd would split the path at the space and expand %d as a specifier if
// they reached ExecStart unescaped. $ is left alone: the executable path is
// not variable-expanded. Checked against systemd 255 with a linked unit.
func TestSystemdUnitEscapesExecStart(t *testing.T) {
	unit := systemdUnit(`/home/a b/100%d/$HOME/livemd`)
	want := `ExecStart="/home/a b/100%%d/$HOME/livemd" start` + "\n"
	if !strings.Contains(unit, want) {
		t.Fatalf("unit does not contain %q:\n%s", want, unit)
	}
	for _, line := range []string{"Restart=on-failure\n", "WantedBy=default.target\n"} {
		if !strings.Contains(unit, line) {
			t.Errorf("unit is missing %q", line)
		}
	}
}

// systemd refuses these executable names however they are quoted.
func TestSystemdPathSupported(t *testing.T) {
	for _, bad := range []string{`/opt/q"uote/livemd`, `/opt/back\slash/livemd`, "/opt/tab\tdir/livemd"} {
		if systemdPathSupported(bad) == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if err := systemdPathSupported(`/home/a b/100%d/$HOME/livemd`); err != nil {
		t.Errorf("rejected a path systemd runs: %v", err)
	}
}

func TestWindowsRunCommandQuotesPath(t *testing.T) {
	got := windowsRunCommand(`C:\Users\A B\AppData\Local\Programs\livemd\livemd.exe`)
	want := `"C:\Users\A B\AppData\Local\Programs\livemd\livemd.exe" start --detach`
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}
