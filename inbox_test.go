package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestListInboxIsFlatAndSkipsHidden(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "b.md"), "b")
	writeFile(t, filepath.Join(dir, "a.zip"), "\x00\x01")
	writeFile(t, filepath.Join(dir, ".partial.md.tmp"), "half")
	writeFile(t, filepath.Join(dir, "notes.md~"), "backup")
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "sub", "deep.md"), "deep")

	files, err := listInbox(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range files {
		names = append(names, f.Name)
	}
	if got := strings.Join(names, ","); got != "a.zip,b.md" {
		t.Fatalf("listed %q, want a.zip,b.md", got)
	}
	if files[0].Size != 2 {
		t.Errorf("a.zip size = %d, want 2", files[0].Size)
	}
}

func TestInboxEntryOnlyAcceptsDirectChildren(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "inbox")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "ok.md"), "x")
	writeFile(t, filepath.Join(dir, "sub", "nested.md"), "x")
	writeFile(t, filepath.Join(dir, ".hidden"), "x")
	writeFile(t, filepath.Join(root, "outside.md"), "x")

	cases := map[string]bool{
		filepath.Join(dir, "ok.md"):              true,
		filepath.Join(dir, "sub", "..", "ok.md"): true, // cleans to a direct child
		filepath.Join(dir, "sub", "nested.md"):   false,
		filepath.Join(dir, "sub"):                false,
		filepath.Join(dir, ".hidden"):            false,
		filepath.Join(dir, "missing.md"):         false,
		filepath.Join(dir, "..", "outside.md"):   false,
		filepath.Join(root, "outside.md"):        false,
		dir:                                      false,
		"":                                       false,
	}
	for path, want := range cases {
		if _, ok := inboxEntry(dir, path); ok != want {
			t.Errorf("inboxEntry(%q) = %v, want %v", path, ok, want)
		}
	}
}

func TestMoveToTrashFreedesktop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("freedesktop trash is the unix implementation")
	}
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))

	src := filepath.Join(root, "my notes.md")
	for i := 0; i < 2; i++ {
		writeFile(t, src, "v"+string(rune('1'+i)))
		if err := moveToTrash(src); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(src); !os.IsNotExist(err) {
			t.Fatalf("source still exists after trash #%d", i+1)
		}
	}

	trash := filepath.Join(root, "data", "Trash")
	for _, name := range []string{"my notes.md", "my notes 2.md"} {
		if _, err := os.Stat(filepath.Join(trash, "files", name)); err != nil {
			t.Errorf("trash/files/%s: %v", name, err)
		}
		info, err := os.ReadFile(filepath.Join(trash, "info", name+".trashinfo"))
		if err != nil {
			t.Fatalf("trash/info/%s.trashinfo: %v", name, err)
		}
		body := string(info)
		if !strings.HasPrefix(body, "[Trash Info]\n") ||
			!strings.Contains(body, "Path="+strings.ReplaceAll(src, " ", "%20")+"\n") ||
			!strings.Contains(body, "DeletionDate="+time.Now().Format("2006-01-02T")) {
			t.Errorf("unexpected trashinfo:\n%s", body)
		}
	}
}

func TestTrashEndpointRefusesFilesOutsideInbox(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("would send a real file to the Recycle Bin")
	}
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	inboxDir := filepath.Join(root, "inbox")
	if err := os.MkdirAll(inboxDir, 0o755); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(inboxDir, "dump.md")
	outside := filepath.Join(root, "keep.md")
	writeFile(t, inside, "x")
	writeFile(t, outside, "x")

	h := newTestHub(t)
	h.inbox = &inbox{dir: inboxDir}
	s := &Server{hub: h}
	mux := s.routes()

	post := func(path string) int {
		body, _ := json.Marshal(map[string]string{"path": path})
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/inbox/trash", bytes.NewReader(body)))
		return rec.Code
	}

	if code := post(outside); code != http.StatusBadRequest {
		t.Errorf("trash outside inbox: status %d, want 400", code)
	}
	if code := post(filepath.Join(inboxDir, "..", "keep.md")); code != http.StatusBadRequest {
		t.Errorf("trash via ..: status %d, want 400", code)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("file outside the inbox was touched: %v", err)
	}
	if code := post(inside); code != http.StatusOK {
		t.Fatalf("trash inside inbox: status %d, want 200", code)
	}
	if _, err := os.Stat(inside); !os.IsNotExist(err) {
		t.Errorf("inbox file still on disk after trash")
	}
}

func TestInboxIsReadableRoot(t *testing.T) {
	root := t.TempDir()
	inboxDir := filepath.Join(root, "inbox")
	if err := os.MkdirAll(inboxDir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(inboxDir, "report.md")
	writeFile(t, file, "# hi")

	h := newTestHub(t)
	if _, ok := h.resolveReadable(file); ok {
		t.Fatal("inbox file readable before the inbox exists")
	}
	h.inbox = &inbox{dir: inboxDir}
	if _, ok := h.resolveReadable(file); !ok {
		t.Fatal("inbox file not readable once the inbox is set")
	}
}

func TestHumanSize(t *testing.T) {
	for n, want := range map[int64]string{0: "0 B", 1023: "1023 B", 1536: "1.5 KB", 5 << 20: "5.0 MB"} {
		if got := humanSize(n); got != want {
			t.Errorf("humanSize(%d) = %q, want %q", n, got, want)
		}
	}
}
