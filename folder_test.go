package main

import (
	"os"
	"os/exec"
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
	if _, _, err := h.RefreshFolder(folder); err != nil {
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
	if _, _, err := h.RefreshFolder(root); err == nil {
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

// A git pull does not only add files: it deletes and renames them too. A
// folder-discovered file is registered inactive, so no watcher ever marks a
// pulled-away file as gone — a refresh has to notice it. This prune keys off
// disk existence, so it holds for git and non-git folders alike; the case that
// motivated it is exercised through git in TestRefreshAfterGitPull below.
func TestRefreshPrunesFilesRemovedSincePull(t *testing.T) {
	home := isolatedHome(t)
	root, childA, _ := exploreTree(t, home)
	h := newHubWithin(t, 5*time.Second)

	if err := h.FollowFolder(&WatchedFolder{Path: root, Recursive: true}); err != nil {
		t.Fatal(err)
	}
	if n := len(h.GetFiles()); n != 4 {
		t.Fatalf("after follow: %d files, want 4", n)
	}

	// The pull renames one file and adds another.
	old := filepath.Join(childA, "backend.md")
	renamed := filepath.Join(childA, "backend-v2.md")
	if err := os.Rename(old, renamed); err != nil {
		t.Fatal(err)
	}
	added := filepath.Join(root, "CHANGELOG.md")
	writeFile(t, added, "# x\n")

	gotAdded, gotRemoved, err := h.RefreshFolder(root)
	if err != nil {
		t.Fatal(err)
	}
	if gotAdded != 2 || gotRemoved != 1 {
		t.Fatalf("refresh reported +%d -%d, want +2 -1", gotAdded, gotRemoved)
	}
	if hasFile(h, old) {
		t.Errorf("renamed-away file still tracked: %s", old)
	}
	if !hasFile(h, renamed) {
		t.Errorf("renamed file not picked up: %s", renamed)
	}
	if !hasFile(h, added) {
		t.Errorf("added file not picked up: %s", added)
	}
	if n := len(h.GetFiles()); n != 5 {
		t.Fatalf("after refresh: %d files, want 5", n)
	}
}

// A file the walk no longer lists but that is still on disk — one tracked by
// hand, or newly gitignored — is kept. Refresh reconciles the folder with the
// disk; it does not remove what is still there.
func TestRefreshKeepsPresentFileNoLongerWalked(t *testing.T) {
	home := isolatedHome(t)
	root, _, _ := exploreTree(t, home)
	h := newHubWithin(t, 5*time.Second)

	if err := h.FollowFolder(&WatchedFolder{Path: root, Recursive: true, Extensions: []string{".md"}}); err != nil {
		t.Fatal(err)
	}
	// A file the folder's filter excludes, tracked by hand inside the folder.
	handTracked := filepath.Join(root, "notes.txt")
	writeFile(t, handTracked, "plain\n")
	if err := h.AddFile(handTracked); err != nil {
		t.Fatal(err)
	}

	if _, removed, err := h.RefreshFolder(root); err != nil {
		t.Fatal(err)
	} else if removed != 0 {
		t.Fatalf("refresh pruned %d present file(s), want 0", removed)
	}
	if !hasFile(h, handTracked) {
		t.Errorf("refresh dropped a hand-tracked file the walk does not list: %s", handTracked)
	}
}

// clear drops every tracked file and every followed folder, and the empty
// result must reach disk — a later restart must not bring the old list back.
func TestRemoveAllClearsFilesFoldersAndState(t *testing.T) {
	home := isolatedHome(t)
	root, _, _ := exploreTree(t, home)
	single := filepath.Join(home, "loose.md")
	writeFile(t, single, "# x\n")
	h := newHubWithin(t, 5*time.Second)

	if err := h.FollowFolder(&WatchedFolder{Path: root, Recursive: true}); err != nil {
		t.Fatal(err)
	}
	if err := h.AddFile(single); err != nil {
		t.Fatal(err)
	}
	if n := len(h.GetFiles()); n != 5 { // 4 under root + the loose file
		t.Fatalf("before clear: %d files, want 5", n)
	}

	files, folders := h.RemoveAll()
	if files != 5 || folders != 1 {
		t.Fatalf("RemoveAll reported %d files, %d folders; want 5, 1", files, folders)
	}
	if n := len(h.GetFiles()); n != 0 {
		t.Errorf("after clear: %d files still tracked, want 0", n)
	}

	// The state on disk must be empty too, or a restart resurrects the list.
	got, err := loadState()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Files) != 0 || len(got.Folders) != 0 {
		t.Errorf("after clear: state has %d files, %d folders; want empty", len(got.Files), len(got.Folders))
	}

	// Clearing an already-empty list is a no-op that reports nothing.
	if files, folders := h.RemoveAll(); files != 0 || folders != 0 {
		t.Errorf("second clear reported %d files, %d folders; want 0, 0", files, folders)
	}
}

// The real thing, through git: follow a git working tree, then simulate a pull
// that removes one file and adds another. The refresh walks via `git ls-files`,
// registers the newcomer, and prunes the file the pull deleted.
func TestRefreshAfterGitPull(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	home := isolatedHome(t)
	repo := filepath.Join(home, "repo")
	writeFile(t, filepath.Join(repo, "a.md"), "# a\n")
	writeFile(t, filepath.Join(repo, "b.md"), "# b\n")
	if out, err := exec.Command("git", "-C", repo, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}

	h := newHubWithin(t, 5*time.Second)
	if err := h.FollowFolder(&WatchedFolder{Path: repo, Recursive: true}); err != nil {
		t.Fatal(err)
	}
	if n := len(h.GetFiles()); n != 2 {
		t.Fatalf("after follow: %d files, want 2", n)
	}

	// A pull deletes a.md and brings in c.md.
	if err := os.Remove(filepath.Join(repo, "a.md")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repo, "c.md"), "# c\n")

	added, removed, err := h.RefreshFolder(repo)
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 || removed != 1 {
		t.Fatalf("refresh reported +%d -%d, want +1 -1", added, removed)
	}
	if hasFile(h, filepath.Join(repo, "a.md")) {
		t.Errorf("file deleted by the pull still tracked")
	}
	if !hasFile(h, filepath.Join(repo, "c.md")) {
		t.Errorf("file added by the pull not picked up")
	}
}
