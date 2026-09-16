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
	writeFile(t, filepath.Join(root, "README.md"), "# x\n")
	writeFile(t, filepath.Join(childA, "backend.md"), "# x\n")
	writeFile(t, filepath.Join(childA, "raw", "capture.md"), "# x\n")
	writeFile(t, filepath.Join(childB, "frontend.md"), "# x\n")
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

// refreshed walks the folder again and reports whether path is now tracked.
// Folders are pulled, not watched, so this is the whole mechanism by which a
// file that appeared after the follow ever shows up.
func refreshed(t *testing.T, h *Hub, folder, path string) bool {
	t.Helper()
	if _, err := h.RefreshFolder(folder); err != nil {
		t.Fatalf("refresh %s: %v", folder, err)
	}
	return hasFile(h, path)
}

// Adding a folder that is already followed must bring back files that were
// removed from the list since, not return without looking.
func TestRefollowRediscoversRemovedFiles(t *testing.T) {
	home := isolatedHome(t)
	root, childA, childB := exploreTree(t, home)
	h := newHubWithin(t, 5*time.Second)

	if err := h.FollowFolder(&WatchedFolder{Path: root, Recursive: true}); err != nil {
		t.Fatal(err)
	}
	h.RemoveFolder(childA)
	h.RemoveFolder(childB)
	h.RemoveFile(filepath.Join(root, "README.md"))
	if n := len(h.GetFiles()); n != 0 {
		t.Fatalf("setup: %d files left, want 0", n)
	}

	// With a trailing slash, as typed into the page's add box.
	if err := h.FollowFolder(&WatchedFolder{Path: root + "/", Recursive: true}); err != nil {
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

	if err := h.FollowFolder(&WatchedFolder{Path: root, Recursive: false}); err != nil {
		t.Fatal(err)
	}
	if err := h.FollowFolder(&WatchedFolder{Path: root, Recursive: true}); err != nil {
		t.Fatal(err)
	}
	got, _ := loadState()
	if len(got.Folders) != 1 || !got.Folders[0].Recursive {
		t.Fatalf("folders after re-adding recursively = %+v", got.Folders)
	}

	// Recursive now, so a refresh must reach into the subfolder.
	p := filepath.Join(childA, "new.md")
	writeFile(t, p, "# x\n")
	if !refreshed(t, h, root, p) {
		t.Fatalf("refresh missed a new file in a subfolder")
	}
}

// Parent followed first, child second, child removed: the parent still covers
// the child's directory, so refreshing the parent must pick up files there.
func TestRemovingNestedChildKeepsParentCovering(t *testing.T) {
	home := isolatedHome(t)
	root, childA, _ := exploreTree(t, home)
	h := newHubWithin(t, 5*time.Second)

	if err := h.FollowFolder(&WatchedFolder{Path: root, Recursive: true}); err != nil {
		t.Fatal(err)
	}
	if err := h.FollowFolder(&WatchedFolder{Path: childA, Recursive: true}); err != nil {
		t.Fatal(err)
	}
	h.RemoveFolder(childA)

	p := filepath.Join(childA, "later.md")
	writeFile(t, p, "# x\n")
	if !refreshed(t, h, root, p) {
		t.Fatalf("refreshing the parent missed the removed child's directory")
	}
}

// Child followed first, parent second: the parent covers everything outside
// the child, and unfollowing the parent must leave the child refreshable on
// its own.
func TestUnfollowingParentKeepsNestedChildRefreshable(t *testing.T) {
	home := isolatedHome(t)
	root, childA, childB := exploreTree(t, home)
	h := newHubWithin(t, 5*time.Second)

	if err := h.FollowFolder(&WatchedFolder{Path: childA, Recursive: true}); err != nil {
		t.Fatal(err)
	}
	if err := h.FollowFolder(&WatchedFolder{Path: root, Recursive: true}); err != nil {
		t.Fatal(err)
	}

	pB := filepath.Join(childB, "b-new.md")
	writeFile(t, pB, "# x\n")
	if !refreshed(t, h, root, pB) {
		t.Fatalf("refreshing the parent missed a file outside the child")
	}

	h.UnfollowFolder(root)
	pA := filepath.Join(childA, "a-new.md")
	writeFile(t, pA, "# x\n")
	if !refreshed(t, h, childA, pA) {
		t.Fatalf("unfollowing the parent left the child unrefreshable")
	}
	if _, err := h.RefreshFolder(root); err == nil {
		t.Errorf("the unfollowed parent is still refreshable")
	}
}

// Removing a folder from the page removes everything under it, including
// folders followed inside it — otherwise they linger with no files, invisible
// in the sidebar and impossible to remove there.
func TestRemoveFolderUnfollowsNestedFolders(t *testing.T) {
	home := isolatedHome(t)
	root, childA, _ := exploreTree(t, home)
	h := newHubWithin(t, 5*time.Second)

	if err := h.FollowFolder(&WatchedFolder{Path: childA, Recursive: true}); err != nil {
		t.Fatal(err)
	}
	if err := h.FollowFolder(&WatchedFolder{Path: root, Recursive: true}); err != nil {
		t.Fatal(err)
	}
	h.RemoveFolder(root)

	got, _ := loadState()
	if len(got.Folders) != 0 || len(got.Files) != 0 {
		t.Fatalf("after removing the parent: folders %+v, %d files", got.Folders, len(got.Files))
	}
}

// A research session creates a topic folder first and writes into it after:
// files in a subfolder that appeared since the follow must be picked up.
func TestFilesInNewSubfolderArePickedUpOnRefresh(t *testing.T) {
	home := isolatedHome(t)
	root, _, _ := exploreTree(t, home)
	h := newHubWithin(t, 5*time.Second)

	if err := h.FollowFolder(&WatchedFolder{Path: root, Recursive: true}); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "n11")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	p := filepath.Join(sub, "README.md")
	writeFile(t, p, "# x\n")
	if hasFile(h, p) {
		t.Fatalf("a file created after the follow appeared without a refresh")
	}
	if !refreshed(t, h, root, p) {
		t.Fatalf("refresh missed a file in a subfolder created since the follow")
	}
}
