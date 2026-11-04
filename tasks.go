package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Task list checkboxes are the one place the page writes to a file. A click
// sends the byte offset of the box's "[ ]" plus a hash of the file as it was
// rendered; the server flips that one character only if the file still hashes
// the same, so a tick can never land on a line that moved under an outside
// edit. Anything else - a stale page, a changed file - is refused and the page
// renders the file again. The write is a single byte in place: no truncation,
// no temp file and rename, so line endings, a BOM, permissions and the file's
// identity on disk (hard links, editors watching the inode) are all untouched.

var (
	taskOffsetKey   = parser.NewContextKey() // byte offset of the body within the file
	taskVersionKey  = parser.NewContextKey() // hash of the whole file as read
	taskWritableKey = parser.NewContextKey() // whether the boxes may be clicked
)

// fileVersion is the token a render carries and a tick must echo back.
func fileVersion(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// taskTransformer stamps every task checkbox with where its "[ ]" sits in the
// file. goldmark gives the box itself no position, but it is always the first
// thing on its paragraph's first line, so that line's segment points at it.
type taskTransformer struct{}

func (t *taskTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	writable, _ := pc.Get(taskWritableKey).(bool)
	if !writable {
		return
	}
	base, _ := pc.Get(taskOffsetKey).(int)
	version, _ := pc.Get(taskVersionKey).(string)
	source := reader.Source()

	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		box, ok := n.(*east.TaskCheckBox)
		if !ok {
			return ast.WalkContinue, nil
		}
		if pos, ok := taskBoxPosition(box, source); ok {
			box.SetAttributeString("data-task-offset", []byte(strconv.Itoa(base+pos)))
			box.SetAttributeString("data-task-version", []byte(version))
		}
		return ast.WalkSkipChildren, nil
	})
}

// taskBoxPosition finds the "[" of a checkbox in source. Anything that does
// not look exactly like "[ ]", "[x]" or "[X]" there is left unstamped, which
// renders the box disabled rather than risk flipping the wrong byte.
func taskBoxPosition(box *east.TaskCheckBox, source []byte) (int, bool) {
	parent := box.Parent()
	if parent == nil || parent.Lines().Len() == 0 {
		return 0, false
	}
	pos := parent.Lines().At(0).Start
	for pos < len(source) && (source[pos] == ' ' || source[pos] == '\t') {
		pos++
	}
	if pos+2 >= len(source) || source[pos] != '[' || source[pos+2] != ']' {
		return 0, false
	}
	switch source[pos+1] {
	case ' ', 'x', 'X':
		return pos, true
	}
	return 0, false
}

// taskRenderer replaces GFM's checkbox output, which is always disabled. A box
// the transformer stamped is live; an unstamped one (read-only file, or a box
// it could not place) stays disabled.
type taskRenderer struct{}

func (r *taskRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(east.KindTaskCheckBox, r.render)
}

func (r *taskRenderer) render(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	box := node.(*east.TaskCheckBox)
	w.WriteString(`<input type="checkbox" class="task-check"`)
	if box.IsChecked {
		w.WriteString(` checked=""`)
	}
	offset, hasOffset := box.AttributeString("data-task-offset")
	version, _ := box.AttributeString("data-task-version")
	if hasOffset {
		fmt.Fprintf(w, ` data-task-offset="%s" data-task-version="%s"`, offset, version)
	} else {
		w.WriteString(` disabled=""`)
	}
	w.WriteString("> ")
	return ast.WalkContinue, nil
}

// ownWrite remembers a tick, so the change it causes is not reported as an
// edit. The watcher and the inbox listing both see the new mtime; while the
// file still holds exactly what the tick wrote, they show the time from
// before the tick instead.
type ownWrite struct {
	hash  [32]byte
	size  int64
	shown time.Time
}

// ownWriteTime reports the time to show for path if its latest change was a
// tick, or ok=false once anything else has touched it (the record is dropped
// then, so a later edit is never mistaken for ours).
func (h *Hub) ownWriteTime(path string) (time.Time, bool) {
	key := NormalizePathForComparison(path)
	h.taskMu.Lock()
	defer h.taskMu.Unlock()
	rec, ok := h.ownWrites[key]
	if !ok {
		return time.Time{}, false
	}
	if info, err := os.Stat(path); err == nil && info.Size() == rec.size {
		if content, err := os.ReadFile(path); err == nil && sha256.Sum256(content) == rec.hash {
			return rec.shown, true
		}
	}
	delete(h.ownWrites, key)
	return time.Time{}, false
}

// Errors a tick can end in. The handler maps them to status codes the page
// tells apart: a conflict re-renders, a read-only file shows its lock.
var (
	errTaskChanged  = errors.New("the file changed since it was rendered")
	errTaskReadOnly = errors.New("the file is read-only")
)

// ToggleTask sets the checkbox whose "[" sits at offset to checked, provided
// the file still hashes to version. The log line is written after the lock is
// released: logging broadcasts, and the hub loop takes taskMu for the inbox
// listing it sends a newly connected page.
func (h *Hub) ToggleTask(path string, offset int, checked bool, version string) error {
	msg, err := h.toggleTask(path, offset, checked, version)
	if msg != "" {
		h.logger.Info(msg)
	}
	return err
}

func (h *Hub) toggleTask(path string, offset int, checked bool, version string) (string, error) {
	h.taskMu.Lock()
	defer h.taskMu.Unlock()

	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if fileVersion(content) != version {
		return "", errTaskChanged
	}
	if offset < 0 || offset+2 >= len(content) || content[offset] != '[' || content[offset+2] != ']' {
		return "", errTaskChanged
	}
	mark := content[offset+1]
	if mark != ' ' && mark != 'x' && mark != 'X' {
		return "", errTaskChanged
	}
	if (mark != ' ') == checked {
		return "", nil // already in that state
	}
	if !fileWritable(path) {
		return "", errTaskReadOnly
	}

	next := byte(' ')
	if checked {
		next = 'x'
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return "", errTaskReadOnly
		}
		return "", err
	}
	_, werr := f.WriteAt([]byte{next}, int64(offset+1))
	cerr := f.Close()
	if werr != nil {
		return "", werr
	}
	if cerr != nil {
		return "", cerr
	}

	// The time to show is the last edit that was not a tick. If the file
	// still holds what the previous tick wrote, that tick's record already
	// knows it; the current mtime is only the previous tick's own.
	key := NormalizePathForComparison(path)
	shown := info.ModTime()
	if prev, ok := h.ownWrites[key]; ok && prev.hash == sha256.Sum256(content) {
		shown = prev.shown
	}
	content[offset+1] = next
	h.ownWrites[key] = ownWrite{
		hash:  sha256.Sum256(content),
		size:  int64(len(content)),
		shown: shown,
	}

	verb := "Unticked"
	if checked {
		verb = "Ticked"
	}
	line := bytes.Count(content[:offset], []byte("\n")) + 1
	return fmt.Sprintf("%s a checkbox in %s (line %d)", verb, filepath.Base(path), line), nil
}

// handleTask backs a checkbox click. It answers with a fresh render in the
// same shape as /api/render, so the page shows the result (and the new
// version to send with the next click) without a second round trip.
func (s *Server) handleTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Path    string `json:"path"`
		Offset  int    `json:"offset"`
		Checked bool   `json:"checked"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	actual, ok := s.hub.resolveReadable(req.Path)
	if !ok || !isMarkdown(actual) {
		http.NotFound(w, r)
		return
	}

	switch err := s.hub.ToggleTask(actual, req.Offset, req.Checked, req.Version); {
	case errors.Is(err, errTaskChanged):
		http.Error(w, err.Error(), http.StatusConflict)
		return
	case errors.Is(err, errTaskReadOnly):
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	case err != nil:
		s.hub.logger.Warn(fmt.Sprintf("Checkbox in %s: %v", filepath.Base(actual), err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.writeRender(w, actual, modeAuto)
}
