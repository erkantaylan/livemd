package main

import "testing"

// Relative and Windows-side paths must come back untouched; only absolute
// Linux paths are mapped into a WSL share.
func TestConvertToWindowsPathLeavesNonLinuxPathsAlone(t *testing.T) {
	for _, p := range []string{".", "..", "docs", `docs\a.md`, `C:\Projects\livemd`, `\\wsl.localhost\Ubuntu\etc`} {
		if got := convertToWindowsPath(p); got != p {
			t.Errorf("convertToWindowsPath(%q) = %q, want unchanged", p, got)
		}
	}
	if got := convertToWindowsPath("/mnt/c/Projects"); got != `C:\Projects` {
		t.Errorf("convertToWindowsPath(/mnt/c/Projects) = %q", got)
	}
}
