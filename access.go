package main

import (
	"os"
	"path/filepath"
	"strings"
)

// Following a markdown link means reading a file nobody added by hand. Serving
// only exact members of the watch list would break every cross-reference;
// serving anything on disk would hand every file the daemon can read to
// whoever reaches this port, which is the whole LAN (main.go advertises the
// interface addresses on purpose). The middle ground is a tracked root: a
// followed folder, or the directory holding a tracked file. Inside one, a
// neighbour renders on demand and the watch list stays exactly as curated;
// outside, the answer is no, and the browser offers to track the path
// explicitly instead.

// resolveReadable maps a requested path to the file the daemon will serve, or
// ok=false to refuse. Tracked files match outright; everything else must land
// inside a tracked root once symlinks are resolved.
func (h *Hub) resolveReadable(requested string) (string, bool) {
	if requested == "" {
		return "", false
	}

	h.mu.RLock()
	for k := range h.files {
		if PathsEqual(k, requested) {
			h.mu.RUnlock()
			return k, true
		}
	}
	roots := h.trackedRootsLocked()
	h.mu.RUnlock()

	abs, err := filepath.Abs(requested)
	if err != nil {
		return "", false
	}
	// Resolve symlinks before the containment test, or a link planted inside a
	// followed folder becomes a keyhole onto the rest of the disk.
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", false
	}
	info, err := os.Stat(real)
	if err != nil || info.IsDir() {
		return "", false
	}
	for _, root := range roots {
		if pathWithin(root, real) {
			return real, true
		}
	}
	return "", false
}

// trackedRootsLocked lists the directories an untracked file may be read from.
// Callers must hold h.mu.
//
// A followed folder is a root outright — the user pointed livemd at the tree.
// A lone tracked file contributes its own directory, which is what makes
// ./sibling.md and img/logo.png resolve, but never $HOME or a filesystem root:
// `livemd add ~/README.md` must not quietly open the entire home directory.
func (h *Hub) trackedRootsLocked() []string {
	roots := make([]string, 0, len(h.folders)+len(h.files))
	for p := range h.folders {
		roots = append(roots, p)
	}
	seen := make(map[string]bool, len(h.files))
	for p := range h.files {
		dir := filepath.Dir(p)
		key := NormalizePathForComparison(dir)
		if seen[key] || isTooBroadRoot(dir) {
			continue
		}
		seen[key] = true
		roots = append(roots, dir)
	}
	return roots
}

// isTooBroadRoot rejects directories whose whole subtree is too much to open on
// the strength of one tracked file inside it.
func isTooBroadRoot(dir string) bool {
	clean := filepath.Clean(dir)
	if clean == "." {
		return true
	}
	// Filesystem root, or a Windows drive/UNC share root.
	if clean == string(filepath.Separator) || clean == filepath.VolumeName(clean)+string(filepath.Separator) {
		return true
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" && PathsEqual(home, clean) {
		return true
	}
	return false
}

// pathWithin reports whether p sits inside root (or is root itself). Both sides
// go through NormalizePathForComparison so the test is case-insensitive on
// Windows, matching how the rest of the daemon compares paths.
func pathWithin(root, p string) bool {
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		rootReal = root
	}
	rel, err := filepath.Rel(NormalizePathForComparison(rootReal), NormalizePathForComparison(p))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
