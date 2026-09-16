package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The fixtures in testdata/render exercise every markdown element the app
// renders. They exist to be looked at in a browser after a styling change, and
// these tests keep the rendering side honest in between: structure that used to
// come out right must keep coming out right.

func fixture(t *testing.T, name string) string {
	t.Helper()
	// Absolute, as the daemon always renders: relative-link rewriting resolves
	// against the document's own directory.
	path := mustAbs(t, filepath.Join("testdata", "render", name))
	html, err := NewRenderer().RenderMode(path, modeAuto)
	if err != nil {
		t.Fatalf("render %s: %v", name, err)
	}
	return html
}

func TestFixturesRender(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("testdata", "render"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("no fixtures found")
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			if html := fixture(t, e.Name()); strings.TrimSpace(html) == "" {
				t.Error("rendered to nothing")
			}
		})
	}
}

func TestKitchenSinkStructure(t *testing.T) {
	html := fixture(t, "kitchen-sink.md")

	for _, want := range []struct{ desc, frag string }{
		{"front matter lifted out", `class="front-matter"`},
		{"heading ids for anchors", `<h2 id="lists">`},
		{"nested list", "<ul>"},
		{"ordered list", "<ol>"},
		{"task list checkbox", `type="checkbox"`},
		{"table head", "<thead>"},
		{"blockquote", "<blockquote>"},
		{"horizontal rule", "<hr"},
		{"inline code", "<code>"},
		{"relative link rewritten", `href="` + filePathToURLPath(mustAbs(t, "testdata/render/code.md")) + `"`},
	} {
		if !strings.Contains(html, want.frag) {
			t.Errorf("%s: %q missing from render", want.desc, want.frag)
		}
	}

	// Front matter must not leak back in as prose.
	if strings.Contains(html, "<h1>name: kitchen-sink") || strings.Contains(html, "<h2>name: kitchen-sink") {
		t.Error("front matter rendered as a heading")
	}

	// A code block inside a list item is where alignment went wrong before, so
	// make sure the nesting survives the pipeline at all.
	if !regexp.MustCompile(`(?s)<li>.*<pre`).MatchString(html) {
		t.Error("no code block nested inside a list item")
	}
}

// Code fences must become code, not prose — including the untagged one.
func TestCodeFixtureBlocks(t *testing.T) {
	html := fixture(t, "code.md")
	if n := strings.Count(html, "<pre"); n < 5 {
		t.Errorf("got %d code blocks, want the fixture's 5+", n)
	}
	if !strings.Contains(html, "no language tag at all") {
		t.Error("untagged fence lost its content")
	}
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// --- Release notes ---------------------------------------------------------
//
// The changelog tab renders markdown, but through a separate goldmark than the
// documents: release bodies come off the network, so raw HTML must not survive.

func TestRenderReleaseBody(t *testing.T) {
	html := renderReleaseBody("## Fixes\n\n- the `--port` flag\n- see [#12](https://example.test/12)\n")

	for _, want := range []struct{ desc, frag string }{
		{"heading", "<h2>Fixes</h2>"},
		{"list", "<li>"},
		{"inline code", "<code>--port</code>"},
		{"link", `href="https://example.test/12"`},
	} {
		if !strings.Contains(html, want.frag) {
			t.Errorf("%s: %q missing from render", want.desc, want.frag)
		}
	}
}

func TestRenderReleaseBodyDropsRawHTML(t *testing.T) {
	html := renderReleaseBody("<script>alert(1)</script>\n\n<img src=x onerror=alert(1)>\n")

	for _, bad := range []string{"<script", "onerror", "<img"} {
		if strings.Contains(html, bad) {
			t.Errorf("raw HTML %q survived into the changelog: %q", bad, html)
		}
	}
}

// An empty body must render to nothing, so the client drops the block instead
// of drawing an empty one.
func TestRenderReleaseBodyEmpty(t *testing.T) {
	for _, in := range []string{"", "   ", "\n\t\n"} {
		if got := renderReleaseBody(in); got != "" {
			t.Errorf("renderReleaseBody(%q) = %q, want empty", in, got)
		}
	}
}
