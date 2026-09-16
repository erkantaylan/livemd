package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRewriteRef(t *testing.T) {
	base := filepath.Join(string(filepath.Separator), "docs")

	tests := []struct {
		name  string
		dest  string
		image bool
		want  string
		skip  bool // left exactly as written
	}{
		{name: "relative sibling", dest: "./setup.md", want: "/docs/setup.md"},
		{name: "bare sibling", dest: "setup.md", want: "/docs/setup.md"},
		{name: "nested", dest: "sub/deep.md", want: "/docs/sub/deep.md"},
		{name: "parent", dest: "../other/a.md", want: "/other/a.md"},
		{name: "fragment kept", dest: "./setup.md#install", want: "/docs/setup.md#install"},
		{name: "percent-encoded space", dest: "my%20notes.md", want: "/docs/my%20notes.md"},
		{name: "query dropped", dest: "a.md?v=2", want: "/docs/a.md"},
		{name: "image to raw", dest: "img/logo.png", image: true, want: "/raw?path=%2Fdocs%2Fimg%2Flogo.png"},
		{name: "http", dest: "https://example.com", skip: true},
		{name: "mailto", dest: "mailto:me@example.com", skip: true},
		{name: "protocol relative", dest: "//cdn.example.com/x.png", skip: true},
		{name: "in-page anchor", dest: "#heading", skip: true},
		{name: "empty", dest: "", skip: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := rewriteRef(base, tc.dest, tc.image)
			if tc.skip {
				if ok {
					t.Fatalf("rewriteRef(%q) rewrote to %q; want left alone", tc.dest, got)
				}
				return
			}
			if !ok {
				t.Fatalf("rewriteRef(%q) left it alone; want %q", tc.dest, tc.want)
			}
			if got != tc.want {
				t.Errorf("rewriteRef(%q) = %q, want %q", tc.dest, got, tc.want)
			}
		})
	}
}

// A single-letter prefix is a Windows drive, not a URL scheme — the one case
// where the "looks like scheme:" test has to say no.
func TestIsExternalRef(t *testing.T) {
	external := []string{"http://x", "https://x", "mailto:a@b", "data:text/plain,x", "ftp://x", "//cdn/x"}
	local := []string{"C:\\docs\\a.md", "./a.md", "a.md", "../a.md", "/docs/a.md", "a:b.md"}

	for _, d := range external {
		if !isExternalRef(d) {
			t.Errorf("isExternalRef(%q) = false, want true", d)
		}
	}
	for _, d := range local {
		if isExternalRef(d) {
			t.Errorf("isExternalRef(%q) = true, want false", d)
		}
	}
}

func TestFilePathToURLPath(t *testing.T) {
	if got := filePathToURLPath("/home/me/doc.md"); got != "/home/me/doc.md" {
		t.Errorf("plain path = %q", got)
	}
	// Space, ? and # have to be escaped or they change the URL's meaning.
	if got := filePathToURLPath("/home/me/my notes.md"); got != "/home/me/my%20notes.md" {
		t.Errorf("spaced path = %q", got)
	}
	if got := filePathToURLPath("/home/me/a#b.md"); got != "/home/me/a%23b.md" {
		t.Errorf("hashed path = %q", got)
	}
}

// End to end through goldmark: the rendered HTML must point at URLs this
// server actually serves, and send off-site links to a new tab.
func TestRenderMarkdownRewritesLinks(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "index.md")
	writeFile(t, doc, "[setup](./setup.md)\n\n![logo](img/logo.png)\n\n[out](https://example.com)\n")

	html, err := NewRenderer().RenderMode(doc, modeAuto)
	if err != nil {
		t.Fatal(err)
	}

	wantHref := `href="` + filePathToURLPath(filepath.Join(dir, "setup.md")) + `"`
	if !strings.Contains(html, wantHref) {
		t.Errorf("link not rewritten; want %s in:\n%s", wantHref, html)
	}
	if !strings.Contains(html, `src="/raw?path=`) {
		t.Errorf("image not pointed at /raw:\n%s", html)
	}
	if !strings.Contains(html, `target="_blank"`) {
		t.Errorf("external link not sent to a new tab:\n%s", html)
	}
	// The raw view is source, not a document — nothing there gets rewritten.
	raw, err := NewRenderer().RenderMode(doc, modeRaw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, `href="/`) {
		t.Errorf("raw view rewrote a link:\n%s", raw)
	}
}

// viewKind drives the layout, so it has to agree with RenderMode's dispatch:
// anything it calls a document gets the narrow reading column.
func TestViewKind(t *testing.T) {
	cases := []struct {
		path string
		mode renderMode
		want string
	}{
		{"notes.md", modeAuto, "document"},
		{"notes.markdown", modeAuto, "document"},
		{"notes.md", modeRaw, "source"}, // Raw is the source, not prose
		{"main.go", modeAuto, "source"},
		{"page.html", modeAuto, "source"},
		{"data.csv", modeAuto, "table"},
		{"data.tsv", modeAuto, "table"},
		{"data.csv", modeRaw, "source"},
		{"diagram.png", modeAuto, "media"},
		{"paper.pdf", modeAuto, "media"},
		{"clip.mp4", modeAuto, "media"},
		{"song.mp3", modeRaw, "media"}, // media has no source view to fall back on
	}
	for _, tc := range cases {
		if got := viewKind(tc.path, tc.mode); got != tc.want {
			t.Errorf("viewKind(%q, %v) = %q, want %q", tc.path, tc.mode, got, tc.want)
		}
	}
}
