package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// WatchedFolder is a directory the daemon "follows": walking it registers every
// file with a matching extension (and not gitignored). It is walked when the
// folder is first followed, when the daemon restores it from saved state, and
// whenever the folder's Refresh button asks for another look — the daemon does
// not watch the directory for new files.
type WatchedFolder struct {
	Path       string   `json:"path"`
	Extensions []string `json:"extensions,omitempty"` // empty = defaultExtensions
	Recursive  bool     `json:"recursive"`
	Depth      int      `json:"depth,omitempty"` // 0 = unlimited (non-git mode only)
}

// allowedExt returns true if path matches the folder's extension filter.
func (f *WatchedFolder) allowedExt(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	exts := f.Extensions
	if len(exts) == 0 {
		exts = defaultExtensions
	}
	for _, e := range exts {
		if ext == strings.ToLower(e) {
			return true
		}
	}
	return false
}

// isGitRepo returns true when `path` is inside a git working tree.
func isGitRepo(path string) bool {
	cmd := exec.Command("git", "-C", path, "rev-parse", "--is-inside-work-tree")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// gitListFiles returns absolute paths of files git would consider part of the
// project at `dir` (tracked + untracked-not-ignored). Submodules are skipped.
func gitListFiles(dir string) ([]string, error) {
	cmd := exec.Command("git", "-C", dir,
		"ls-files",
		"--cached", "--others", "--exclude-standard",
		"-z", // null-separated for paths with newlines/spaces
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git ls-files: %v: %s", err, stderr.String())
	}

	var files []string
	for _, raw := range bytes.Split(stdout.Bytes(), []byte{0}) {
		if len(raw) == 0 {
			continue
		}
		// git outputs paths relative to dir
		abs := filepath.Join(dir, string(raw))
		files = append(files, abs)
	}
	return files, nil
}

// gitIsIgnored returns true if `path` is ignored according to git in `repoDir`.
// Returns false if not in a git repo or git is unavailable.
func gitIsIgnored(repoDir, path string) bool {
	cmd := exec.Command("git", "-C", repoDir, "check-ignore", "-q", path)
	err := cmd.Run()
	if err == nil {
		return true // exit 0 = ignored
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 {
		return false // exit 1 = not ignored
	}
	// 128 = not in repo / other error → treat as not-ignored.
	return false
}

// walkFolder enumerates files under `folder` according to its filter and depth.
// Uses git ls-files when available so .gitignore is respected automatically;
// falls back to filepath.Walk with depth cap otherwise.
func walkFolder(folder *WatchedFolder) ([]string, error) {
	if isGitRepo(folder.Path) {
		all, err := gitListFiles(folder.Path)
		if err != nil {
			return nil, err
		}
		var out []string
		for _, p := range all {
			if folder.allowedExt(p) {
				out = append(out, p)
			}
		}
		return out, nil
	}

	maxDepth := folder.Depth
	if maxDepth == 0 {
		maxDepth = 10
	}
	rootDepth := strings.Count(folder.Path, string(filepath.Separator))

	var files []string
	err := filepath.Walk(folder.Path, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if strings.HasPrefix(info.Name(), ".") && p != folder.Path {
				return filepath.SkipDir
			}
			depth := strings.Count(p, string(filepath.Separator)) - rootDepth
			if depth > maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if folder.allowedExt(p) {
			files = append(files, p)
		}
		return nil
	})
	return files, err
}
