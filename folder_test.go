package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// exploreTree lays out root/README.md plus two child folders with files,
// the shape of a research folder where each topic gets its own directory.
func exploreTree(t *testing.T, home string) (root, childA, childB string) {
	t.Helper()
	root = filepath.Join(home, "explore")
	childA = filepath.Join(root, "tgoyemek")
	childB = filepath.Join(root, "trendyol")
	writeFile(t, filepath.Join(root, "README.md"))
	writeFile(t, filepath.Join(childA, "backend.md"))
	writeFile(t, filepath.Join(childA, "raw", "capture.md"))
	writeFile(t, filepath.Join(childB, "frontend.md"))
	return root, childA, childB
}

func hasFile(h *Hub, path string) bool {
	for _, f := range h.GetFiles() {
		if f.Path == path {
			return true
		}
	}
	return false
}

// waitForFile polls for an fsnotify-driven auto-add.
func waitForFile(h *Hub, path string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if hasFile(h, path) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// Adding a folder that is already followed must bring back files that were
// removed from the list since, not return without looking.
func TestRefollowRediscoversRemovedFiles(t *testing.T) {
	home := isolatedHome(t)
	root, childA, childB := exploreTree(t, home)
	h := newHubWithin(t, 5*time.Second)
	defer h.folderMgr.Close()

	if err := h.FollowFolder(&WatchedFolder{Path: root, Recursive: true, Live: true}); err != nil {
		t.Fatal(err)
	}
	h.RemoveFolder(childA)
	h.RemoveFolder(childB)
	h.RemoveFile(filepath.Join(root, "README.md"))
	if n := len(h.GetFiles()); n != 0 {
		t.Fatalf("setup: %d files left, want 0", n)
	}

	// With a trailing slash, as typed into the page's add box.
	if err := h.FollowFolder(&WatchedFolder{Path: root + "/", Recursive: true, Live: true}); err != nil {
		t.Fatal(err)
	}
	if got, _ := loadState(); len(got.Folders) != 1 {
		t.Fatalf("re-adding with a trailing slash made a second folder: %+v", got.Folders)
	}
	if n := len(h.GetFiles()); n != 4 {
		t.Fatalf("after re-adding the folder: %d files, want 4", n)
	}
}

// Re-adding with different options (the page used to follow non-recursively)
// must apply the new options.
func TestRefollowUpdatesOptions(t *testing.T) {
	home := isolatedHome(t)
	root, childA, _ := exploreTree(t, home)
	h := newHubWithin(t, 5*time.Second)
	defer h.folderMgr.Close()

	if err := h.FollowFolder(&WatchedFolder{Path: root, Recursive: false, Live: true}); err != nil {
		t.Fatal(err)
	}
	if err := h.FollowFolder(&WatchedFolder{Path: root, Recursive: true, Live: true}); err != nil {
		t.Fatal(err)
	}
	got, _ := loadState()
	if len(got.Folders) != 1 || !got.Folders[0].Recursive {
		t.Fatalf("folders after re-adding recursively = %+v", got.Folders)
	}

	p := filepath.Join(childA, "new.md")
	writeFile(t, p)
	if !waitForFile(h, p, 3*time.Second) {
		t.Fatalf("new file in a subfolder was not auto-added")
	}
}

// Parent followed first, child second, child removed: the parent still
// follows the child's directory and must keep auto-adding there.
func TestRemovingNestedChildKeepsParentWatching(t *testing.T) {
	home := isolatedHome(t)
	root, childA, _ := exploreTree(t, home)
	h := newHubWithin(t, 5*time.Second)
	defer h.folderMgr.Close()

	if err := h.FollowFolder(&WatchedFolder{Path: root, Recursive: true, Live: true}); err != nil {
		t.Fatal(err)
	}
	if err := h.FollowFolder(&WatchedFolder{Path: childA, Recursive: true, Live: true}); err != nil {
		t.Fatal(err)
	}
	h.RemoveFolder(childA)

	p := filepath.Join(childA, "later.md")
	writeFile(t, p)
	if !waitForFile(h, p, 3*time.Second) {
		t.Fatalf("parent stopped auto-adding in the removed child's directory")
	}
}

// Child followed first, parent second: the parent auto-adds outside the
// child, and unfollowing the parent must leave the child watching its own
// directories.
func TestUnfollowingParentKeepsNestedChildWatching(t *testing.T) {
	home := isolatedHome(t)
	root, childA, childB := exploreTree(t, home)
	h := newHubWithin(t, 5*time.Second)
	defer h.folderMgr.Close()

	if err := h.FollowFolder(&WatchedFolder{Path: childA, Recursive: true, Live: true}); err != nil {
		t.Fatal(err)
	}
	if err := h.FollowFolder(&WatchedFolder{Path: root, Recursive: true, Live: true}); err != nil {
		t.Fatal(err)
	}

	pB := filepath.Join(childB, "b-new.md")
	writeFile(t, pB)
	if !waitForFile(h, pB, 3*time.Second) {
		t.Fatalf("parent did not auto-add outside the child")
	}

	h.UnfollowFolder(root)
	pA := filepath.Join(childA, "a-new.md")
	writeFile(t, pA)
	if !waitForFile(h, pA, 3*time.Second) {
		t.Fatalf("unfollowing the parent stopped the child from auto-adding")
	}
}

// Removing a folder from the page removes everything under it, including
// folders followed inside it — otherwise they linger with no files, invisible
// in the sidebar and impossible to remove there.
func TestRemoveFolderUnfollowsNestedFolders(t *testing.T) {
	home := isolatedHome(t)
	root, childA, _ := exploreTree(t, home)
	h := newHubWithin(t, 5*time.Second)
	defer h.folderMgr.Close()

	if err := h.FollowFolder(&WatchedFolder{Path: childA, Recursive: true, Live: true}); err != nil {
		t.Fatal(err)
	}
	if err := h.FollowFolder(&WatchedFolder{Path: root, Recursive: true, Live: true}); err != nil {
		t.Fatal(err)
	}
	h.RemoveFolder(root)

	got, _ := loadState()
	if len(got.Folders) != 0 || len(got.Files) != 0 {
		t.Fatalf("after removing the parent: folders %+v, %d files", got.Folders, len(got.Files))
	}
}

// A research session creates a topic folder first and writes into it after:
// files written into a subfolder that appeared after the follow must be added.
func TestFilesInNewSubfolderAreAutoAdded(t *testing.T) {
	home := isolatedHome(t)
	root, _, _ := exploreTree(t, home)
	h := newHubWithin(t, 5*time.Second)
	defer h.folderMgr.Close()

	if err := h.FollowFolder(&WatchedFolder{Path: root, Recursive: true, Live: true}); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "n11")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond) // let the Create for the directory subscribe it

	p := filepath.Join(sub, "README.md")
	writeFile(t, p)
	if !waitForFile(h, p, 3*time.Second) {
		t.Fatalf("file written into a new subfolder was not auto-added")
	}
}
