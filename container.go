package main

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Fenced containers (":::note Title" ... ":::") are the callout syntax of
// Markdig, Docusaurus and VitePress. Unlike a code fence the body is ordinary
// markdown, so the node holds child blocks instead of raw lines.
//
// A closing fence is a line of colons at least as long as the opening one, so a
// container nests inside a longer fence (::::details around :::note).

// calloutKinds maps the known kinds to the title shown when the fence has none.
// A kind outside this list still renders, as a neutral box titled by its name.
var calloutKinds = map[string]string{
	"note":     "Note",
	"info":     "Info",
	"tip":      "Tip",
	"success":  "Success",
	"warning":  "Warning",
	"danger":   "Danger",
	"example":  "Example",
	"question": "Question",
}

const detailsKind = "details"

// A name is required: a bare ":::" is only ever a closing fence.
var containerOpen = regexp.MustCompile(`^(:{3,})[ \t]*([A-Za-z][A-Za-z0-9_-]*)(?:[ \t]+(.*?))?[ \t]*$`)

type containerBlock struct {
	ast.BaseBlock
	fence int    // length of the opening run of colons
	kind  string // lowercased
	title string // as written, empty when the fence has none
}

var kindContainerBlock = ast.NewNodeKind("ContainerBlock")

func (n *containerBlock) Kind() ast.NodeKind { return kindContainerBlock }

func (n *containerBlock) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"kind": n.kind, "title": n.title}, nil)
}

// heading is the text for the box's title line.
func (n *containerBlock) heading() string {
	if n.title != "" {
		return n.title
	}
	if n.kind == detailsKind {
		return "Details"
	}
	if title, ok := calloutKinds[n.kind]; ok {
		return title
	}
	return strings.ToUpper(n.kind[:1]) + n.kind[1:]
}

type containerParser struct{}

func (b *containerParser) Trigger() []byte {
	return []byte{':'}
}

func (b *containerParser) Open(parent ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, segment := reader.PeekLine()
	pos := pc.BlockOffset()
	if pos < 0 || pos >= len(line) {
		return nil, parser.NoChildren
	}
	m := containerOpen.FindSubmatch(bytes.TrimRight(line[pos:], "\r\n"))
	if m == nil {
		return nil, parser.NoChildren
	}
	reader.Advance(restOfLine(line, segment))
	return &containerBlock{
		fence: len(m[1]),
		kind:  strings.ToLower(string(m[2])),
		title: string(m[3]),
	}, parser.HasChildren
}

func (b *containerParser) Continue(node ast.Node, reader text.Reader, pc parser.Context) parser.State {
	n := node.(*containerBlock)
	line, segment := reader.PeekLine()
	if isContainerClose(line, n.fence, reader.LineOffset()) && !insideVerbatim(n, pc) {
		reader.Advance(restOfLine(line, segment))
		return parser.Close
	}
	return parser.Continue | parser.HasChildren
}

func (b *containerParser) Close(node ast.Node, reader text.Reader, pc parser.Context) {}

// Same as a code fence: a container needs no blank line above it.
func (b *containerParser) CanInterruptParagraph() bool { return true }

func (b *containerParser) CanAcceptIndentedLine() bool { return false }

// restOfLine is how far to advance to consume a line but leave its newline, the
// way goldmark's own code fence does.
func restOfLine(line []byte, segment text.Segment) int {
	n := segment.Stop - segment.Start + segment.Padding
	if len(line) > 0 && line[len(line)-1] == '\n' {
		n--
	}
	return n
}

func isContainerClose(line []byte, fence, offset int) bool {
	if w, _ := util.IndentWidth(line, offset); w >= 4 {
		return false
	}
	trimmed := bytes.TrimSpace(line)
	return len(trimmed) >= fence && len(bytes.TrimLeft(trimmed, ":")) == 0
}

// insideVerbatim reports whether a block open inside n takes its lines as
// source. goldmark asks the outermost block first, so without this a ":::" in a
// code sample would end the container around it.
func insideVerbatim(n *containerBlock, pc parser.Context) bool {
	opened := pc.OpenedBlocks()
	for i := len(opened) - 1; i >= 0 && opened[i].Node != ast.Node(n); i-- {
		switch opened[i].Node.(type) {
		case *ast.FencedCodeBlock, *mathBlock:
			return true
		}
	}
	return false
}

type containerRenderer struct{}

func (r *containerRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindContainerBlock, r.render)
}

func (r *containerRenderer) render(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*containerBlock)
	details := n.kind == detailsKind
	if !entering {
		if details {
			w.WriteString("</details>\n")
		} else {
			w.WriteString("</div>\n")
		}
		return ast.WalkContinue, nil
	}

	// The title is plain text, as in Markdig, so it is escaped and not parsed.
	title := util.EscapeHTML([]byte(n.heading()))
	if details {
		w.WriteString(`<details class="callout"><summary class="callout-title">`)
		w.Write(title)
		w.WriteString("</summary>\n")
		return ast.WalkContinue, nil
	}
	if _, known := calloutKinds[n.kind]; known {
		// A known kind, so safe in a class name as it stands.
		w.WriteString(`<div class="callout callout-` + n.kind + `">`)
	} else {
		w.WriteString(`<div class="callout">`)
	}
	w.WriteString(`<p class="callout-title">`)
	w.Write(title)
	w.WriteString("</p>\n")
	return ast.WalkContinue, nil
}
