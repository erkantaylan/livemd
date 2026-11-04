package main

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"
)

var taskBoxRe = regexp.MustCompile(`<input type="checkbox" class="task-check"( checked="")?( data-task-offset="(\d+)" data-task-version="([0-9a-f]+)")?( disabled="")?>`)

type renderedBox struct {
	checked bool
	offset  int // -1 when the box is disabled
	version string
}

func renderBoxes(t *testing.T, path string) []renderedBox {
	t.Helper()
	html, err := NewRenderer().RenderMode(path, modeAuto)
	if err != nil {
		t.Fatal(err)
	}
	var boxes []renderedBox
	for _, m := range taskBoxRe.FindAllStringSubmatch(html, -1) {
		b := renderedBox{checked: m[1] != "", offset: -1, version: m[4]}
		if m[3] != "" {
			b.offset, _ = strconv.Atoi(m[3])
		}
		boxes = append(boxes, b)
	}
	return boxes
}

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func testTaskHub() *Hub {
	return &Hub{ownWrites: make(map[string]ownWrite), logger: NewLogger(10)}
}

// Every box must point at its own "[" in the file, wherever the list sits:
// after front matter, behind a blockquote, nested, with CRLF endings.
func TestTaskOffsetsPointAtBrackets(t *testing.T) {
	src := "---\r\ntitle: x\r\n---\r\n# Doc\r\n\r\n- [ ] one\r\n- [x] two\r\n  - [X] nested\r\n\r\n> - [ ] quoted\r\n\r\n```\r\n- [ ] in code\r\n```\r\n\r\n1. [ ] ordered\r\n"
	path := writeTemp(t, "doc.md", src)
	boxes := renderBoxes(t, path)
	if len(boxes) != 5 {
		t.Fatalf("got %d boxes, want 5 (code blocks hold none): %+v", len(boxes), boxes)
	}
	wantChecked := []bool{false, true, true, false, false}
	for i, b := range boxes {
		if b.offset < 0 {
			t.Fatalf("box %d is disabled", i)
		}
		if got := src[b.offset : b.offset+3]; got[0] != '[' || got[2] != ']' {
			t.Errorf("box %d offset %d lands on %q", i, b.offset, got)
		}
		if b.checked != wantChecked[i] {
			t.Errorf("box %d checked=%v, want %v", i, b.checked, wantChecked[i])
		}
		if b.version != fileVersion([]byte(src)) {
			t.Errorf("box %d carries the wrong version", i)
		}
	}
}

func TestToggleTaskFlipsOneByte(t *testing.T) {
	src := "- [ ] one\r\n- [x] two\r\n"
	path := writeTemp(t, "doc.md", src)
	h := testTaskHub()
	boxes := renderBoxes(t, path)

	if err := h.ToggleTask(path, boxes[0].offset, true, boxes[0].version); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if want := "- [x] one\r\n- [x] two\r\n"; string(got) != want {
		t.Fatalf("after tick: %q, want %q", got, want)
	}

	// The page re-renders after a tick, so the next click carries the new
	// version; unticking the second box works from that.
	boxes = renderBoxes(t, path)
	if err := h.ToggleTask(path, boxes[1].offset, false, boxes[1].version); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	if want := "- [x] one\r\n- [ ] two\r\n"; string(got) != want {
		t.Fatalf("after untick: %q, want %q", got, want)
	}
}

func TestToggleTaskRefusesStaleVersion(t *testing.T) {
	path := writeTemp(t, "doc.md", "- [ ] one\n")
	h := testTaskHub()
	boxes := renderBoxes(t, path)

	// An outside edit anywhere in the file makes the render stale.
	os.WriteFile(path, []byte("intro\n\n- [ ] one\n"), 0o644)
	err := h.ToggleTask(path, boxes[0].offset, true, boxes[0].version)
	if !errors.Is(err, errTaskChanged) {
		t.Fatalf("got %v, want errTaskChanged", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "intro\n\n- [ ] one\n" {
		t.Fatalf("file was changed: %q", got)
	}
}

func TestToggleTaskRefusesWrongOffset(t *testing.T) {
	src := "- [ ] one\n"
	path := writeTemp(t, "doc.md", src)
	h := testTaskHub()
	for _, off := range []int{-1, 0, 3, len(src)} {
		if err := h.ToggleTask(path, off, true, fileVersion([]byte(src))); !errors.Is(err, errTaskChanged) {
			t.Errorf("offset %d: got %v, want errTaskChanged", off, err)
		}
	}
}

func TestReadOnlyFileRendersDisabledBoxes(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anything")
	}
	path := writeTemp(t, "doc.md", "- [ ] one\n")
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	boxes := renderBoxes(t, path)
	if len(boxes) != 1 || boxes[0].offset != -1 {
		t.Fatalf("read-only file rendered a live box: %+v", boxes)
	}
	h := testTaskHub()
	err := h.ToggleTask(path, 2, true, fileVersion([]byte("- [ ] one\n")))
	if !errors.Is(err, errTaskReadOnly) {
		t.Fatalf("got %v, want errTaskReadOnly", err)
	}
}

// A tick keeps the time of the last real edit until anything else writes.
func TestOwnWriteKeepsShownTime(t *testing.T) {
	path := writeTemp(t, "doc.md", "- [ ] one\n")
	before, _ := os.Stat(path)
	h := testTaskHub()
	boxes := renderBoxes(t, path)
	if err := h.ToggleTask(path, boxes[0].offset, true, boxes[0].version); err != nil {
		t.Fatal(err)
	}

	shown, ok := h.ownWriteTime(path)
	if !ok || !shown.Equal(before.ModTime()) {
		t.Fatalf("after tick: shown=%v ok=%v, want %v", shown, ok, before.ModTime())
	}

	// A second tick still shows the edit before the first, not the first
	// tick's own mtime (pushed forward here so the two cannot coincide).
	later := before.ModTime().Add(time.Hour)
	os.Chtimes(path, later, later)
	boxes = renderBoxes(t, path)
	if err := h.ToggleTask(path, boxes[0].offset, false, boxes[0].version); err != nil {
		t.Fatal(err)
	}
	shown, ok = h.ownWriteTime(path)
	if !ok || !shown.Equal(before.ModTime()) {
		t.Fatalf("after second tick: shown=%v ok=%v, want %v", shown, ok, before.ModTime())
	}

	os.WriteFile(path, []byte("- [x] one!\n"), 0o644)
	if _, ok := h.ownWriteTime(path); ok {
		t.Fatal("an outside edit was taken for the tick")
	}
	if len(h.ownWrites) != 0 {
		t.Fatal("record not dropped after an outside edit")
	}
}
