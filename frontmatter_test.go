package main

import (
	"strings"
	"testing"
)

func TestSplitFrontMatter(t *testing.T) {
	doc := "---\nname: livemd\ndescription: A viewer\n---\n# Title\n\nBody.\n"
	fm, body := splitFrontMatter([]byte(doc))
	if string(fm) != "name: livemd\ndescription: A viewer\n" {
		t.Errorf("front matter = %q", fm)
	}
	if string(body) != "# Title\n\nBody.\n" {
		t.Errorf("body = %q", body)
	}

	// No fence: everything is body, nothing is lost.
	plain := "# Title\n\n---\n\nA horizontal rule is not front matter.\n"
	if fm, body := splitFrontMatter([]byte(plain)); fm != nil || string(body) != plain {
		t.Errorf("plain document was split: fm=%q", fm)
	}

	// An unterminated fence must not swallow the file.
	unterminated := "---\nname: broken\nstill going\n"
	if fm, body := splitFrontMatter([]byte(unterminated)); fm != nil || string(body) != unterminated {
		t.Errorf("unterminated fence was split: fm=%q body=%q", fm, body)
	}
}

func TestRenderFrontMatter(t *testing.T) {
	// Block scalars and wrapped lines fold into one value, in document order.
	src := "name: livemd\ndescription: >\n  Render a file in the browser\n  with live reload.\nallowed-tools:\n  - Bash\n  - Read\n"
	got := renderFrontMatter([]byte(src))

	for _, want := range []string{
		"<dt>name</dt><dd>livemd</dd>",
		"Render a file in the browser with live reload.",
		"<dd>Bash Read</dd>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Index(got, "<dt>name</dt>") > strings.Index(got, "<dt>description</dt>") {
		t.Error("fields were reordered; document order carries meaning")
	}

	// Values are escaped, not trusted.
	if got := renderFrontMatter([]byte(`title: <script>alert(1)</script>`)); strings.Contains(got, "<script>") {
		t.Errorf("front matter value was not escaped: %s", got)
	}

	// Unparseable front matter is shown, never silently dropped.
	if got := renderFrontMatter([]byte("[ this is not yaml ]")); !strings.Contains(got, "not yaml") {
		t.Errorf("unparseable front matter was dropped: %q", got)
	}
}

// The whole point: front matter must stop arriving as a heading.
func TestMarkdownFrontMatterIsNotProse(t *testing.T) {
	dir := t.TempDir()
	doc := dir + "/skill.md"
	writeFile(t, doc, "---\nname: livemd\ndescription: Render a file\n---\n\n# LiveMD\n\nBody.\n")

	html, err := NewRenderer().RenderMode(doc, modeAuto)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, `class="front-matter"`) {
		t.Errorf("no front-matter block:\n%s", html)
	}
	if strings.Contains(html, "<h1>name: livemd") || strings.Contains(html, "<h2>name: livemd") {
		t.Errorf("front matter still rendered as a heading:\n%s", html)
	}
	if !strings.Contains(html, `<h1 id="livemd">LiveMD</h1>`) {
		t.Errorf("document heading lost:\n%s", html)
	}
}
