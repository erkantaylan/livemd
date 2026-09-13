package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// isolatedHome points the state file at a fresh temp dir for the test.
func isolatedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# x\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

// newHubWithin builds a hub and fails the test if restore doesn't finish in time.
func newHubWithin(t *testing.T, d time.Duration) *Hub {
	t.Helper()
	done := make(chan *Hub, 1)
	go func() { done <- NewHub() }()
	select {
	case h := <-done:
		return h
	case <-time.After(d):
		t.Fatalf("NewHub did not return within %s", d)
		return nil
	}
}

// Restarting the daemon must not drop followed folders from the state file.
func TestRestoreKeepsFoldersOnDisk(t *testing.T) {
	home := isolatedHome(t)
	folder := filepath.Join(home, "notes")
	single := filepath.Join(home, "single.md")
	writeFile(t, filepath.Join(folder, "a.md"))
	writeFile(t, single)

	want := &State{
		Files:   []StateFile{{Path: single}, {Path: filepath.Join(folder, "a.md")}},
		Folders: []WatchedFolder{{Path: folder, Recursive: false, Live: true}},
	}
	if err := saveState(want); err != nil {
		t.Fatal(err)
	}

	h := newHubWithin(t, 5*time.Second)
	defer h.folderMgr.Close()

	got, err := loadState()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Folders) != 1 || got.Folders[0].Path != folder {
		t.Fatalf("folders after restore = %+v, want [%s]", got.Folders, folder)
	}
	if len(got.Files) != 2 {
		t.Fatalf("files after restore = %d, want 2", len(got.Files))
	}
}

// Restore runs before any browser can connect; a large watch list must not
// block on the broadcast channel.
func TestRestoreManyFilesDoesNotHang(t *testing.T) {
	home := isolatedHome(t)
	var files []StateFile
	for i := 0; i < 400; i++ {
		p := filepath.Join(home, "many", fmt.Sprintf("f%03d.md", i))
		writeFile(t, p)
		files = append(files, StateFile{Path: p})
	}
	if err := saveState(&State{Files: files}); err != nil {
		t.Fatal(err)
	}

	h := newHubWithin(t, 10*time.Second)
	defer h.folderMgr.Close()

	if n := len(h.GetFiles()); n != 400 {
		t.Fatalf("restored %d files, want 400", n)
	}
}

// Removing a followed folder that holds no files must still save the unfollow.
func TestRemoveEmptyFollowedFolderPersists(t *testing.T) {
	home := isolatedHome(t)
	folder := filepath.Join(home, "empty")
	if err := os.MkdirAll(folder, 0755); err != nil {
		t.Fatal(err)
	}

	h := newHubWithin(t, 5*time.Second)
	defer h.folderMgr.Close()
	if err := h.FollowFolder(&WatchedFolder{Path: folder, Live: true}); err != nil {
		t.Fatal(err)
	}
	h.RemoveFolder(folder)

	got, err := loadState()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Folders) != 0 {
		t.Fatalf("folders after remove = %+v, want none", got.Folders)
	}
}

// A state file that fails to parse must be set aside, not overwritten by the
// next change with an empty list.
func TestCorruptStateIsPreserved(t *testing.T) {
	home := isolatedHome(t)
	statePath := getStateFilePath()
	if err := os.WriteFile(statePath, []byte(`{"files": [ {"path": "/x.md"`), 0644); err != nil {
		t.Fatal(err)
	}

	h := newHubWithin(t, 5*time.Second)
	defer h.folderMgr.Close()

	p := filepath.Join(home, "new.md")
	writeFile(t, p)
	if err := h.AddFile(p); err != nil {
		t.Fatal(err)
	}

	backups, _ := filepath.Glob(statePath + ".corrupt-*")
	if len(backups) != 1 {
		t.Fatalf("want 1 corrupt-state backup, found %v", backups)
	}
	data, _ := os.ReadFile(backups[0])
	if string(data) != `{"files": [ {"path": "/x.md"` {
		t.Fatalf("backup content changed: %q", data)
	}
}
