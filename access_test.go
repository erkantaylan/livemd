package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestHub builds a Hub without NewHub's side effects — no saved state read
// from the user's home directory, no fsnotify watchers.
func newTestHub(t *testing.T) *Hub {
	t.Helper()
	return &Hub{
		files:    make(map[string]*WatchedFile),
		folders:  make(map[string]*WatchedFolder),
		watchers: make(map[string]*Watcher),
		renderer: NewRenderer(),
		logger:   NewLogger(10),
		// Buffered and never drained: AddFile broadcasts, and a nil channel
		// would deadlock the test rather than fail it.
		broadcast: make(chan []byte, 256),
	}
}

func (h *Hub) track(path string) {
	h.files[path] = &WatchedFile{Path: path, Name: filepath.Base(path)}
}

func TestResolveReadable(t *testing.T) {
	root := t.TempDir()
	// A home directory of its own, so the $HOME guard is exercised against a
	// directory this test controls rather than the developer's real one.
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	docs := filepath.Join(home, "docs")
	writeFile(t, filepath.Join(docs, "index.md"), "# index")
	writeFile(t, filepath.Join(docs, "setup.md"), "# setup")
	writeFile(t, filepath.Join(docs, "img", "logo.png"), "png")
	writeFile(t, filepath.Join(home, "secret.md"), "# secret")
	writeFile(t, filepath.Join(root, "elsewhere", "other.md"), "# other")

	t.Run("tracked file is readable", func(t *testing.T) {
		h := newTestHub(t)
		h.track(filepath.Join(docs, "index.md"))
		if _, ok := h.resolveReadable(filepath.Join(docs, "index.md")); !ok {
			t.Fatal("tracked file refused")
		}
	})

	t.Run("neighbour of a tracked file is readable", func(t *testing.T) {
		h := newTestHub(t)
		h.track(filepath.Join(docs, "index.md"))
		for _, p := range []string{
			filepath.Join(docs, "setup.md"),
			filepath.Join(docs, "img", "logo.png"),
		} {
			if _, ok := h.resolveReadable(p); !ok {
				t.Errorf("%s refused; should be inside the tracked root", p)
			}
		}
	})

	t.Run("outside every root is refused", func(t *testing.T) {
		h := newTestHub(t)
		h.track(filepath.Join(docs, "index.md"))
		for _, p := range []string{
			filepath.Join(root, "elsewhere", "other.md"),
			filepath.Join(docs, "..", "secret.md"),
		} {
			if actual, ok := h.resolveReadable(p); ok {
				t.Errorf("%s allowed as %s; should be outside the tracked root", p, actual)
			}
		}
	})

	t.Run("directory is refused", func(t *testing.T) {
		h := newTestHub(t)
		h.track(filepath.Join(docs, "index.md"))
		if _, ok := h.resolveReadable(docs); ok {
			t.Error("a directory was accepted as a readable file")
		}
	})

	t.Run("followed folder opens its whole tree", func(t *testing.T) {
		h := newTestHub(t)
		h.folders[docs] = &WatchedFolder{Path: docs, Recursive: true}
		if _, ok := h.resolveReadable(filepath.Join(docs, "img", "logo.png")); !ok {
			t.Error("file inside a followed folder refused")
		}
		if _, ok := h.resolveReadable(filepath.Join(home, "secret.md")); ok {
			t.Error("file outside the followed folder allowed")
		}
	})

	// Tracking one file in $HOME must not open the whole home directory: the
	// implicit root is dropped, so only exact matches remain readable.
	t.Run("home directory is never an implicit root", func(t *testing.T) {
		h := newTestHub(t)
		h.track(filepath.Join(home, "secret.md"))
		if _, ok := h.resolveReadable(filepath.Join(home, "secret.md")); !ok {
			t.Fatal("the tracked file itself was refused")
		}
		if _, ok := h.resolveReadable(filepath.Join(docs, "setup.md")); ok {
			t.Error("tracking ~/secret.md opened the rest of the home directory")
		}
	})

	t.Run("symlink out of the root is refused", func(t *testing.T) {
		link := filepath.Join(docs, "escape.md")
		if err := os.Symlink(filepath.Join(root, "elsewhere", "other.md"), link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		defer os.Remove(link)

		h := newTestHub(t)
		h.track(filepath.Join(docs, "index.md"))
		if _, ok := h.resolveReadable(link); ok {
			t.Error("a symlink pointing outside the root was followed")
		}
	})
}

// The two handlers that read files off disk must agree with resolveReadable,
// since that is the only thing standing between a LAN peer and the filesystem.
func TestHandlersHonourTrackedRoots(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))

	docs := filepath.Join(root, "docs")
	writeFile(t, filepath.Join(docs, "index.md"), "# index\n\n[setup](./setup.md)\n")
	writeFile(t, filepath.Join(docs, "setup.md"), "# setup")
	writeFile(t, filepath.Join(root, "elsewhere", "other.md"), "# other")

	h := newTestHub(t)
	h.track(filepath.Join(docs, "index.md"))
	s := &Server{hub: h}

	mux := http.NewServeMux()
	mux.HandleFunc("/raw", s.handleRaw)
	mux.HandleFunc("/api/render", s.handleRender)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	get := func(endpoint, path string) int {
		resp, err := http.Get(srv.URL + endpoint + "?path=" + url.QueryEscape(path))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	for _, endpoint := range []string{"/raw", "/api/render"} {
		if code := get(endpoint, filepath.Join(docs, "setup.md")); code != http.StatusOK {
			t.Errorf("%s untracked neighbour: got %d, want 200", endpoint, code)
		}
		if code := get(endpoint, filepath.Join(root, "elsewhere", "other.md")); code != http.StatusNotFound {
			t.Errorf("%s file outside the root: got %d, want 404", endpoint, code)
		}
	}
}

// With the app served at the file's own path, every real endpoint has to keep
// winning against the catch-all that hands back index.html.
func TestPathStyleURLsDoNotSwallowEndpoints(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	writeFile(t, filepath.Join(root, "docs", "index.md"), "# index")

	h := newTestHub(t)
	h.track(filepath.Join(root, "docs", "index.md"))
	srv := httptest.NewServer((&Server{hub: h}).routes())
	defer srv.Close()

	body := func(path string) (int, string) {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}

	// The app itself, at / and at any file path.
	for _, p := range []string{"/", "/home/me/doc.md", "/C:/Users/me/doc.md", "/home/me/my%20notes.md"} {
		code, b := body(p)
		if code != http.StatusOK || !strings.Contains(b, "<title>LiveMD</title>") {
			t.Errorf("GET %s: got %d, want the app shell", p, code)
		}
	}

	// Endpoints and assets must not be mistaken for file paths.
	if code, b := body("/api/files"); code != http.StatusOK || !strings.HasPrefix(strings.TrimSpace(b), "[") {
		t.Errorf("GET /api/files: got %d %q, want the JSON file list", code, b)
	}
	if code, b := body("/static/client.js"); code != http.StatusOK || !strings.Contains(b, "WebSocket client") {
		t.Errorf("GET /static/client.js: got %d, want the client script", code)
	}
	if code, _ := body("/favicon.ico"); code != http.StatusNotFound {
		t.Errorf("GET /favicon.ico: got %d, want 404 rather than the app shell", code)
	}
}

// Followed folders are walked on request, not watched, so Refresh is the only
// thing that picks up a file created after the folder was followed.
func TestRefreshFolderPicksUpNewFiles(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))

	docs := filepath.Join(root, "docs")
	writeFile(t, filepath.Join(docs, "one.md"), "# one")

	h := newTestHub(t)
	folder := &WatchedFolder{Path: docs, Recursive: true}
	if err := h.FollowFolder(folder); err != nil {
		t.Fatal(err)
	}
	if got := len(h.files); got != 1 {
		t.Fatalf("after following: %d files, want 1", got)
	}

	// A file that appears afterwards is invisible until someone asks again.
	writeFile(t, filepath.Join(docs, "two.md"), "# two")
	if got := len(h.files); got != 1 {
		t.Fatalf("before refresh: %d files, want the new file still unseen", got)
	}

	added, err := h.RefreshFolder(docs)
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 {
		t.Errorf("RefreshFolder added %d, want 1", added)
	}
	if got := len(h.files); got != 2 {
		t.Errorf("after refresh: %d files, want 2", got)
	}

	// Refreshing again is a no-op: already-registered files aren't counted.
	if added, err := h.RefreshFolder(docs); err != nil || added != 0 {
		t.Errorf("second refresh added %d (err %v), want 0", added, err)
	}

	if _, err := h.RefreshFolder(filepath.Join(root, "not-followed")); err == nil {
		t.Error("refreshing an unfollowed folder should fail")
	}
}
