package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// WatchedFolder is a directory the daemon "follows": it auto-registers any new
// file with a matching extension (and not gitignored) when it appears on disk.
type WatchedFolder struct {
	Path       string   `json:"path"`
	Extensions []string `json:"extensions,omitempty"` // empty = defaultExtensions
	Recursive  bool     `json:"recursive"`
	Depth      int      `json:"depth,omitempty"` // 0 = unlimited (non-git mode only)
	Live       bool     `json:"live"`
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

// FolderManager owns a single fsnotify watcher across every followed folder
// directory and dispatches Create events to the Hub for auto-registration.
type FolderManager struct {
	hub     *Hub
	watcher *fsnotify.Watcher
	mu      sync.Mutex
	folders map[string]*WatchedFolder // root path -> folder
	// dirRoots maps each subscribed directory to every followed root that
	// covers it. Followed folders can nest (explore/ and explore/tgoyemek/),
	// and one root per directory meant the later follow took the directory
	// over: unfollowing either one then dropped the other's watch on it.
	dirRoots map[string]map[string]bool
	done     chan struct{}
}

func NewFolderManager(hub *Hub) (*FolderManager, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	fm := &FolderManager{
		hub:      hub,
		watcher:  w,
		folders:  make(map[string]*WatchedFolder),
		dirRoots: make(map[string]map[string]bool),
		done:     make(chan struct{}),
	}
	go fm.run()
	return fm, nil
}

// Follow registers a folder, runs initial discovery, and starts watching it
// (recursively if folder.Recursive). Returns the list of newly discovered files.
func (fm *FolderManager) Follow(folder *WatchedFolder) ([]string, error) {
	fm.mu.Lock()
	fm.folders[folder.Path] = folder
	fm.mu.Unlock()

	// Initial discovery
	files, err := walkFolder(folder)
	if err != nil {
		return nil, err
	}

	// Subscribe directories so future Create events fire.
	if err := fm.subscribeTree(folder.Path, folder); err != nil {
		fm.hub.logger.Warn(fmt.Sprintf("Folder watcher subscribe partial failure for %s: %v", folder.Path, err))
	}
	return files, nil
}

// Unfollow stops watching a folder. A directory is released from fsnotify only
// once no other followed folder still covers it.
func (fm *FolderManager) Unfollow(rootPath string) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	for dir, roots := range fm.dirRoots {
		if !roots[rootPath] {
			continue
		}
		delete(roots, rootPath)
		if len(roots) == 0 {
			fm.watcher.Remove(dir)
			delete(fm.dirRoots, dir)
		}
	}
	delete(fm.folders, rootPath)
}

// SetLive flips the auto-add flag without un-subscribing.
func (fm *FolderManager) SetLive(rootPath string, live bool) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	if f, ok := fm.folders[rootPath]; ok {
		f.Live = live
	}
}

func (fm *FolderManager) Close() {
	close(fm.done)
	fm.watcher.Close()
}

// subscribeTree adds the folder root and all subdirs (if recursive) to fsnotify.
// On Linux/macOS fsnotify isn't recursive, so we walk and add explicitly.
func (fm *FolderManager) subscribeTree(root string, folder *WatchedFolder) error {
	rootDepth := strings.Count(root, string(filepath.Separator))
	maxDepth := folder.Depth
	if maxDepth == 0 {
		maxDepth = 10
	}
	return filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			return nil
		}
		if strings.HasPrefix(info.Name(), ".") && p != root {
			return filepath.SkipDir
		}
		if !folder.Recursive && p != root {
			return filepath.SkipDir
		}
		depth := strings.Count(p, string(filepath.Separator)) - rootDepth
		if depth > maxDepth {
			return filepath.SkipDir
		}
		fm.mu.Lock()
		if fm.dirRoots[p] == nil {
			fm.dirRoots[p] = make(map[string]bool)
		}
		fm.dirRoots[p][folder.Path] = true
		fm.mu.Unlock()
		_ = fm.watcher.Add(p) // no-op if another root already added it
		return nil
	})
}

func (fm *FolderManager) run() {
	for {
		select {
		case <-fm.done:
			return
		case ev, ok := <-fm.watcher.Events:
			if !ok {
				return
			}
			if ev.Op&fsnotify.Create == fsnotify.Create {
				fm.handleCreate(ev.Name)
			}
		case err, ok := <-fm.watcher.Errors:
			if !ok {
				return
			}
			fm.hub.logger.Warn(fmt.Sprintf("Folder watcher error: %v", err))
		}
	}
}

// handleCreate offers a new path to every followed folder covering its parent
// directory; each applies its own live switch, filter and recursion. AddFile
// ignores a file a second folder already registered.
func (fm *FolderManager) handleCreate(path string) {
	fm.mu.Lock()
	var covering []*WatchedFolder
	for root := range fm.dirRoots[filepath.Dir(path)] {
		if folder, ok := fm.folders[root]; ok {
			covering = append(covering, folder)
		}
	}
	fm.mu.Unlock()
	if len(covering) == 0 {
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		return
	}

	for _, folder := range covering {
		fm.mu.Lock()
		live := folder.Live
		fm.mu.Unlock()
		if !live {
			fm.hub.logger.Info(fmt.Sprintf("Live=off, skipping create: %s", filepath.Base(path)))
			continue
		}

		if info.IsDir() {
			if folder.Recursive {
				_ = fm.subscribeTree(path, folder)
				// New subdir might already contain files (e.g. created via mv).
				files, _ := walkFolder(&WatchedFolder{Path: path, Extensions: folder.Extensions, Recursive: true, Depth: folder.Depth})
				for _, f := range files {
					fm.maybeAddFile(folder, f)
				}
			}
			continue
		}

		fm.maybeAddFile(folder, path)
	}
}

func (fm *FolderManager) maybeAddFile(folder *WatchedFolder, path string) {
	if !folder.allowedExt(path) {
		return
	}
	if isGitRepo(folder.Path) && gitIsIgnored(folder.Path, path) {
		return
	}
	if err := fm.hub.AddFile(path); err != nil {
		// Already-registered errors are expected when a file came in via two paths.
		if !strings.Contains(err.Error(), "already registered") {
			fm.hub.logger.Warn(fmt.Sprintf("Auto-add %s: %v", path, err))
		}
		return
	}
	fm.hub.logger.Info(fmt.Sprintf("Auto-added: %s", filepath.Base(path)))
	fm.hub.persistState()
}

