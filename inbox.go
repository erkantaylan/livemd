package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// The inbox is one fixed folder that agents (or anything else) drop files into
// and the browser lists in a tab of its own. It differs from a followed folder
// in three ways, all on purpose:
//
//   - It is pushed, not pulled. A followed folder is walked on demand (see
//     RefreshFolder) because a tree under git churns in ways a watcher reports
//     badly. The inbox is flat and written by programs, so one fsnotify watch on
//     one directory answers "what is in there" exactly, the moment it changes.
//   - Its files never join the watch list. They live in the Inbox tab only, so
//     throwaway output stays out of the Files tree and out of the state file.
//   - Removing a file there deletes it — into the OS trash, never for good.
//     Everywhere else removing only stops tracking; in a dump folder the only
//     useful "remove" is getting rid of the file.

// InboxFile is one entry in the Inbox tab.
type InboxFile struct {
	Path    string    `json:"path"`
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}

// getInboxDir is the inbox's fixed location: ~/.livemd/inbox, or
// %APPDATA%\livemd\inbox on Windows, next to the rest of livemd's files there.
func getInboxDir() string {
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = os.Getenv("USERPROFILE")
		}
		return filepath.Join(appData, "livemd", "inbox")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".livemd", "inbox")
}

// inboxHidden reports names the inbox does not list: dotfiles and editor
// backups. Programs that write atomically often go through a hidden temp file
// and rename it into place; listing that file for a few milliseconds would only
// make a row flicker in and out.
func inboxHidden(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasSuffix(name, "~")
}

// listInbox reads the inbox directory. Subdirectories are skipped: the inbox is
// flat by design, and nothing in it is walked.
func listInbox(dir string) ([]InboxFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	files := make([]InboxFile, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || inboxHidden(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // gone between ReadDir and Info
		}
		if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			continue // sockets, fifos and the like
		}
		files = append(files, InboxFile{
			Path:    filepath.Join(dir, e.Name()),
			Name:    e.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files, nil
}

// inboxEntry resolves path to a file directly inside the inbox, or ok=false.
// It is the guard on the one endpoint that deletes things, so it is strict:
// the parent must be the inbox itself (no subfolders, no ../ escapes) and the
// name must be one the inbox lists.
func inboxEntry(dir, path string) (string, bool) {
	if dir == "" || path == "" {
		return "", false
	}
	clean := filepath.Clean(path)
	if !PathsEqual(filepath.Dir(clean), dir) {
		return "", false
	}
	name := filepath.Base(clean)
	if name == "." || name == ".." || inboxHidden(name) {
		return "", false
	}
	full := filepath.Join(dir, name)
	info, err := os.Lstat(full)
	if err != nil || info.IsDir() {
		return "", false
	}
	return full, true
}

// inbox watches the inbox directory and broadcasts its listing on every change.
type inbox struct {
	dir     string
	watcher *fsnotify.Watcher

	mu    sync.Mutex
	timer *time.Timer
}

// startInbox creates the inbox if needed and starts watching it. A failure is
// logged, not fatal: livemd without an inbox is still livemd.
func (h *Hub) startInbox() {
	dir := getInboxDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		h.logger.Warn(fmt.Sprintf("Inbox: %v", err))
		return
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		h.logger.Warn(fmt.Sprintf("Inbox watcher: %v", err))
		return
	}
	if err := w.Add(dir); err != nil {
		w.Close()
		h.logger.Warn(fmt.Sprintf("Inbox watcher: %v", err))
		return
	}

	ib := &inbox{dir: dir, watcher: w}
	h.mu.Lock()
	h.inbox = ib
	h.mu.Unlock()

	go func() {
		for {
			select {
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if inboxHidden(filepath.Base(ev.Name)) {
					continue
				}
				ib.debounce(h.broadcastInbox)
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				h.logger.Warn(fmt.Sprintf("Inbox watcher: %v", err))
			}
		}
	}()
	h.logger.Info("Inbox: " + dir)
}

// debounce coalesces a burst of events — an agent writing a file in chunks
// raises one per write — into a single listing.
func (ib *inbox) debounce(fn func()) {
	ib.mu.Lock()
	defer ib.mu.Unlock()
	if ib.timer != nil {
		ib.timer.Stop()
	}
	ib.timer = time.AfterFunc(100*time.Millisecond, fn)
}

func (ib *inbox) close() {
	ib.mu.Lock()
	if ib.timer != nil {
		ib.timer.Stop()
	}
	ib.mu.Unlock()
	ib.watcher.Close()
}

// inboxDir is the watched inbox directory, or "" when it failed to start.
func (h *Hub) inboxDir() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.inbox == nil {
		return ""
	}
	return h.inbox.dir
}

// inboxMessage is the "inbox" broadcast: the directory and what is in it. The
// listing carries mtimes, which is how the page notices that the file it is
// showing was rewritten and needs fetching again.
func (h *Hub) inboxMessage() []byte {
	dir := h.inboxDir()
	files := []InboxFile{}
	if dir != "" {
		if list, err := listInbox(dir); err == nil {
			files = list
		}
	}
	data, _ := json.Marshal(struct {
		Type  string      `json:"type"`
		Dir   string      `json:"dir"`
		Files []InboxFile `json:"files"`
	}{"inbox", dir, files})
	return data
}

func (h *Hub) broadcastInbox() {
	h.broadcast <- h.inboxMessage()
}

// TrashInboxFile moves one inbox file to the OS trash. The watcher sees it go
// and broadcasts the new listing, so this does not.
func (h *Hub) TrashInboxFile(path string) error {
	full, ok := inboxEntry(h.inboxDir(), path)
	if !ok {
		return fmt.Errorf("not an inbox file: %s", path)
	}
	if err := moveToTrash(full); err != nil {
		return err
	}
	h.logger.Info("Moved to trash: " + filepath.Base(full))
	return nil
}

// cmdInbox prints the inbox path, creating the folder so a caller can write to
// it straight away whether or not the daemon is running. Agents ask for the
// path rather than hardcoding it, so it can move without breaking them.
func cmdInbox() {
	dir := getInboxDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(dir)
}
