package main

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"html/template"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// maxFileSize caps files the daemon reads into memory (text, code, markdown,
// tabular). Media is exempt — the browser streams it from /raw, so a 500MB
// video costs the daemon nothing.
const maxFileSize = 50 << 20 // 50 MB

// maxHighlightSize caps syntax highlighting. Chroma wraps every token in a
// span, inflating output roughly 8x — a 47MB file became 374MB of HTML and
// took the daemon past 5GB. Above this we serve escaped plain text (~1x)
// instead: still every line, just no colors, which nobody reads at this size.
const maxHighlightSize = 2 << 20 // 2 MB

// renderMode selects which view of a file to produce.
type renderMode int

const (
	// modeAuto picks the viewer by file type: markdown → goldmark, tabular →
	// table, media → embed, everything else → highlighted source.
	modeAuto renderMode = iota
	// modeRaw forces the highlighted-source view, backing the Preview/Raw
	// toggle for markdown and HTML.
	modeRaw
)

// Renderer converts files to HTML
type Renderer struct {
	md goldmark.Markdown
}

func NewRenderer() *Renderer {
	md := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			highlighting.NewHighlighting(
				highlighting.WithStyle("github"),
				highlighting.WithFormatOptions(),
			),
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
			parser.WithASTTransformers(
				util.Prioritized(&mermaidTransformer{}, 100),
			),
			parser.WithBlockParsers(
				// Before the paragraph parser (1000) so $$ blocks are claimed
				// whole — setext/thematic rules never see their inner lines.
				util.Prioritized(&mathBlockParser{}, 750),
			),
		),
		goldmark.WithRendererOptions(
			goldmarkhtml.WithHardWraps(),
			goldmarkhtml.WithUnsafe(),
			renderer.WithNodeRenderers(
				util.Prioritized(&mermaidRenderer{}, 100),
				util.Prioritized(&mathBlockRenderer{}, 100),
			),
		),
	)

	return &Renderer{md: md}
}

// Mermaid support works in two stages: an AST transformer swaps ```mermaid
// fences for a dedicated mermaidBlock node at parse time, and a renderer emits
// those nodes as <div class="mermaid"> for client-side mermaid.js. Registering
// a renderer for the shared FencedCodeBlock kind instead would displace
// goldmark-highlighting entirely — goldmark keeps one render func per node
// kind — silently dropping every non-mermaid code block.
type mermaidBlock struct {
	ast.BaseBlock
}

var kindMermaidBlock = ast.NewNodeKind("MermaidBlock")

func (n *mermaidBlock) Kind() ast.NodeKind { return kindMermaidBlock }

func (n *mermaidBlock) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

type mermaidTransformer struct{}

func (t *mermaidTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	source := reader.Source()
	var fences []*ast.FencedCodeBlock
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			if cb, ok := n.(*ast.FencedCodeBlock); ok && string(cb.Language(source)) == "mermaid" {
				fences = append(fences, cb)
			}
		}
		return ast.WalkContinue, nil
	})
	for _, cb := range fences {
		mb := &mermaidBlock{}
		mb.SetLines(cb.Lines())
		cb.Parent().ReplaceChild(cb.Parent(), cb, mb)
	}
}

// Display math ($$ ... $$) is parsed as its own block, like a code fence.
// Left to the default pipeline, the formula's inner lines are exposed to
// markdown block rules — a lone "=" line turns the preceding lines into a
// setext heading — and WithHardWraps splits the text across <br> tags, which
// client-side KaTeX auto-render cannot match a $$ pair across. Claiming the
// whole block and emitting its raw source keeps the formula intact for KaTeX.
type mathBlock struct {
	ast.BaseBlock
	closed bool
}

var kindMathBlock = ast.NewNodeKind("MathBlock")

func (n *mathBlock) Kind() ast.NodeKind { return kindMathBlock }

func (n *mathBlock) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

type mathBlockParser struct{}

func (b *mathBlockParser) Trigger() []byte {
	return []byte{'$'}
}

func (b *mathBlockParser) Open(parent ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, segment := reader.PeekLine()
	pos := pc.BlockOffset()
	if pos < 0 || pos+1 >= len(line) || line[pos] != '$' || line[pos+1] != '$' {
		return nil, parser.NoChildren
	}
	node := &mathBlock{}
	node.Lines().Append(segment)
	// $$...$$ on a single line closes immediately.
	if trimmed := bytes.TrimSpace(line[pos+2:]); len(trimmed) >= 2 && bytes.HasSuffix(trimmed, []byte("$$")) {
		node.closed = true
	}
	reader.Advance(segment.Len() - 1)
	return node, parser.NoChildren
}

func (b *mathBlockParser) Continue(node ast.Node, reader text.Reader, pc parser.Context) parser.State {
	n := node.(*mathBlock)
	if n.closed {
		return parser.Close
	}
	line, segment := reader.PeekLine()
	node.Lines().Append(segment)
	if trimmed := bytes.TrimSpace(line); bytes.HasSuffix(trimmed, []byte("$$")) {
		n.closed = true
	}
	reader.Advance(segment.Len() - 1)
	return parser.Continue | parser.NoChildren
}

func (b *mathBlockParser) Close(node ast.Node, reader text.Reader, pc parser.Context) {}

func (b *mathBlockParser) CanInterruptParagraph() bool { return false }

func (b *mathBlockParser) CanAcceptIndentedLine() bool { return false }

type mathBlockRenderer struct{}

func (r *mathBlockRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindMathBlock, r.render)
}

func (r *mathBlockRenderer) render(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*mathBlock)
	w.WriteString(`<div class="math-display">`)
	for i := 0; i < n.Lines().Len(); i++ {
		line := n.Lines().At(i)
		w.Write(util.EscapeHTML(line.Value(source)))
	}
	w.WriteString(`</div>`)
	return ast.WalkSkipChildren, nil
}

type mermaidRenderer struct{}

func (r *mermaidRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindMermaidBlock, r.render)
}

func (r *mermaidRenderer) render(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*mermaidBlock)
	w.WriteString(`<div class="mermaid">`)
	for i := 0; i < n.Lines().Len(); i++ {
		line := n.Lines().At(i)
		w.Write(util.EscapeHTML(line.Value(source)))
	}
	w.WriteString(`</div>`)
	return ast.WalkSkipChildren, nil
}

func (r *Renderer) Render(path string) (string, error) {
	return r.RenderMode(path, modeAuto)
}

// RenderMode renders a file in the requested view. Files above maxFileSize get
// a placeholder instead of content — the guard lives here as well as at add
// time because a watched file can grow past the limit while being tailed.
func (r *Renderer) RenderMode(path string, mode renderMode) (string, error) {
	ext := strings.ToLower(filepath.Ext(path))

	// Media: rendered as <img>/<embed>/<audio>/<video> referencing /raw.
	// Don't read content into memory — the browser fetches via /raw.
	if html, ok := renderMedia(path, ext); ok {
		return html, nil
	}

	if info, err := os.Stat(path); err == nil && info.Size() > maxFileSize {
		return renderTooLargeMessage(path, info.Size()), nil
	}

	// Tabular: read and render as HTML table (raw view shows the source).
	if mode == modeAuto && (ext == ".csv" || ext == ".tsv") {
		content, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return renderTable(content, ext == ".tsv"), nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	if isBinary(content) {
		return renderBinaryMessage(path), nil
	}

	if mode == modeAuto && isMarkdown(path) {
		return r.renderMarkdown(content)
	}

	// Line numbers help in code but clutter prose, and they land in the
	// selection when copying out of the raw markdown view.
	return r.renderCode(path, content, !isMarkdown(path))
}

func (r *Renderer) renderMarkdown(content []byte) (string, error) {
	var buf bytes.Buffer
	if err := r.md.Convert(content, &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (r *Renderer) renderCode(path string, content []byte, lineNumbers bool) (string, error) {
	code := string(content)
	total := strings.Count(code, "\n") + 1

	if len(content) > maxHighlightSize {
		return renderPlainText(code, total), nil
	}

	// Get lexer
	lexer := getLexer(path)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	// Get style and formatter
	style := styles.Get("github")
	if style == nil {
		style = styles.Fallback
	}
	formatter := html.New(
		html.WithClasses(false),
		html.WithLineNumbers(lineNumbers),
		html.TabWidth(4),
	)

	// Tokenize and format
	iterator, err := lexer.Tokenise(nil, code)
	if err != nil {
		// Fall back to plain text
		return renderPlainText(code, total), nil
	}

	var buf bytes.Buffer
	err = formatter.Format(&buf, style, iterator)
	if err != nil {
		return renderPlainText(code, total), nil
	}

	return buf.String() + lineCountMarker(total), nil
}

// lineCountMarker embeds the file's line count in the rendered HTML as an
// invisible element, which the client surfaces in the content subheader.
func lineCountMarker(total int) string {
	if total == 0 {
		return ""
	}
	return fmt.Sprintf(`<div class="line-info" data-total="%d" hidden></div>`, total)
}

func renderPlainText(code string, total int) string {
	escaped := strings.ReplaceAll(code, "&", "&amp;")
	escaped = strings.ReplaceAll(escaped, "<", "&lt;")
	escaped = strings.ReplaceAll(escaped, ">", "&gt;")

	return `<pre style="background: #f6f8fa; padding: 16px; overflow-x: auto; border-radius: 6px; font-family: monospace; font-size: 14px; line-height: 1.45;"><code>` + escaped + `</code></pre>` +
		lineCountMarker(total)
}

// renderMedia returns embed HTML for image/PDF/audio/video files. The browser
// fetches the bytes from /raw — the daemon never reads them into memory.
// Returns ok=false for non-media extensions.
// isStreamedMedia reports whether a file is served straight from disk to the
// browser via /raw rather than read into daemon memory. Such files are exempt
// from maxFileSize — a large video or PDF costs the daemon nothing.
func isStreamedMedia(path string) bool {
	_, ok := renderMedia(path, strings.ToLower(filepath.Ext(path)))
	return ok
}

func renderMedia(path, ext string) (string, bool) {
	rawURL := "/raw?path=" + url.QueryEscape(path)
	name := template.HTMLEscapeString(filepath.Base(path))

	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".ico", ".avif", ".svg":
		return fmt.Sprintf(
			`<div style="text-align:center;padding:16px;"><img src="%s" alt="%s" style="max-width:100%%;height:auto;border-radius:4px;"></div>`,
			rawURL, name,
		), true
	case ".pdf":
		return fmt.Sprintf(
			`<embed src="%s" type="application/pdf" style="width:100%%;height:calc(100vh - 120px);border:none;">`,
			rawURL,
		), true
	case ".mp3", ".wav", ".ogg", ".oga", ".m4a", ".flac", ".aac", ".opus":
		return fmt.Sprintf(
			`<div style="padding:24px;"><div style="margin-bottom:12px;color:#444;font-weight:500;">%s</div><audio controls preload="metadata" style="width:100%%;"><source src="%s"></audio></div>`,
			name, rawURL,
		), true
	case ".mp4", ".webm", ".mov", ".mkv", ".m4v":
		return fmt.Sprintf(
			`<div style="padding:16px;text-align:center;"><video controls preload="metadata" style="max-width:100%%;max-height:calc(100vh - 160px);border-radius:4px;"><source src="%s"></video></div>`,
			rawURL,
		), true
	}
	return "", false
}

// renderTable parses CSV (or TSV) content and emits a styled HTML table.
// Truncates at maxTableRows so a 1M-row file doesn't lock up the browser.
func renderTable(content []byte, tsv bool) string {
	const maxTableRows = 5000

	reader := csv.NewReader(bytes.NewReader(content))
	if tsv {
		reader.Comma = '\t'
	}
	reader.FieldsPerRecord = -1 // tolerate ragged rows
	reader.LazyQuotes = true

	var rows [][]string
	truncated := false
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Stop on the first parse error rather than silently producing
			// half a table — fall back to the chroma view.
			return renderPlainText(string(content), strings.Count(string(content), "\n")+1)
		}
		rows = append(rows, row)
		if len(rows) >= maxTableRows {
			truncated = true
			break
		}
	}
	if len(rows) == 0 {
		return `<p style="padding:24px;color:#666;">Empty file.</p>`
	}

	var b strings.Builder
	b.WriteString(`<div style="overflow:auto;max-height:calc(100vh - 120px);"><table style="border-collapse:collapse;font-family:monospace;font-size:13px;">`)
	for i, row := range rows {
		tag := "td"
		bg := ""
		if i == 0 {
			tag = "th"
			bg = "background:#f6f8fa;position:sticky;top:0;"
		}
		b.WriteString("<tr>")
		for _, cell := range row {
			fmt.Fprintf(&b,
				`<%s style="border:1px solid #d0d7de;padding:6px 10px;text-align:left;%s">%s</%s>`,
				tag, bg, template.HTMLEscapeString(cell), tag,
			)
		}
		b.WriteString("</tr>")
	}
	b.WriteString(`</table></div>`)

	if truncated {
		fmt.Fprintf(&b,
			`<div style="padding:12px;background:#fff3cd;color:#856404;border-radius:4px;margin-top:16px;">Showing first %d rows.</div>`,
			maxTableRows,
		)
	}
	return b.String()
}

// renderTooLargeMessage is shown instead of content for files over
// maxFileSize. Media never lands here — it streams from /raw.
func renderTooLargeMessage(path string, size int64) string {
	return fmt.Sprintf(`<div style="text-align: center; padding: 40px; color: #666;">
		<p style="font-size: 48px; margin-bottom: 16px;">&#128207;</p>
		<p>File too large: %s</p>
		<p style="color: #999; font-size: 14px; margin-top: 8px;">%.1f MB exceeds the %d MB display limit.</p>
	</div>`,
		template.HTMLEscapeString(filepath.Base(path)),
		float64(size)/(1<<20),
		maxFileSize>>20,
	)
}

func renderBinaryMessage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	name := filepath.Base(path)

	// Check if it's an image
	imageExts := map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".svg": true, ".webp": true, ".ico": true}
	if imageExts[ext] {
		return `<div style="text-align: center; padding: 40px;">
			<p style="color: #666; margin-bottom: 16px;">Image file: ` + name + `</p>
			<p style="color: #999; font-size: 14px;">Image preview not supported</p>
		</div>`
	}

	return `<div style="text-align: center; padding: 40px; color: #666;">
		<p style="font-size: 48px; margin-bottom: 16px;">📦</p>
		<p>Binary file: ` + name + `</p>
		<p style="color: #999; font-size: 14px; margin-top: 8px;">Cannot display binary content</p>
	</div>`
}

func isMarkdown(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".md" || ext == ".markdown" || ext == ".mdown" || ext == ".mkd"
}

func isBinary(content []byte) bool {
	// Check first 8000 bytes for null bytes or invalid UTF-8
	checkLen := len(content)
	if checkLen > 8000 {
		checkLen = 8000
	}

	sample := content[:checkLen]

	// Check for null bytes (common in binary files)
	if bytes.Contains(sample, []byte{0}) {
		return true
	}

	// Check if valid UTF-8
	if !utf8.Valid(sample) {
		return true
	}

	return false
}

func getLexer(path string) chroma.Lexer {
	name := strings.ToLower(filepath.Base(path))
	ext := strings.ToLower(filepath.Ext(path))

	// Special filenames
	specialFiles := map[string]string{
		"makefile":      "makefile",
		"gnumakefile":   "makefile",
		"dockerfile":    "docker",
		".gitignore":    "gitignore",
		".gitattributes": "gitignore",
		".gitmodules":   "gitignore",
		".dockerignore": "docker",
		".editorconfig": "ini",
		".env":          "bash",
		".bashrc":       "bash",
		".zshrc":        "bash",
		".bash_profile": "bash",
		"cmakelists.txt": "cmake",
		"go.mod":        "gomod",
		"go.sum":        "gomod",
		"cargo.toml":    "toml",
		"cargo.lock":    "toml",
		"package.json":  "json",
		"tsconfig.json": "json",
		"composer.json": "json",
		"requirements.txt": "text",
		"gemfile":       "ruby",
		"rakefile":      "ruby",
		"vagrantfile":   "ruby",
		"jenkinsfile":   "groovy",
	}

	if lexerName, ok := specialFiles[name]; ok {
		if l := lexers.Get(lexerName); l != nil {
			return l
		}
	}

	// Try by extension
	if ext != "" {
		// Strip the dot
		extNoDot := ext[1:]

		// Common extension mappings
		extMap := map[string]string{
			"yml":  "yaml",
			"js":   "javascript",
			"ts":   "typescript",
			"tsx":  "typescript",
			"jsx":  "javascript",
			"py":   "python",
			"rb":   "ruby",
			"rs":   "rust",
			"sh":   "bash",
			"zsh":  "bash",
			"fish": "fish",
			"ps1":  "powershell",
			"psm1": "powershell",
			"bat":  "batch",
			"cmd":  "batch",
			"h":    "c",
			"hpp":  "cpp",
			"cc":   "cpp",
			"cxx":  "cpp",
			"cs":   "csharp",
			"fs":   "fsharp",
			"kt":   "kotlin",
			"kts":  "kotlin",
			"scala": "scala",
			"clj":  "clojure",
			"ex":   "elixir",
			"exs":  "elixir",
			"erl":  "erlang",
			"hrl":  "erlang",
			"hs":   "haskell",
			"ml":   "ocaml",
			"mli":  "ocaml",
			"pl":   "perl",
			"pm":   "perl",
			"r":    "r",
			"lua":  "lua",
			"vim":  "vim",
			"el":   "emacs-lisp",
			"lisp": "common-lisp",
			"scm":  "scheme",
			"rkt":  "racket",
			"asm":  "nasm",
			"s":    "gas",
			"tf":   "terraform",
			"hcl":  "hcl",
			"nix":  "nix",
			"vue":  "vue",
			"svelte": "svelte",
		}

		if mappedName, ok := extMap[extNoDot]; ok {
			if l := lexers.Get(mappedName); l != nil {
				return l
			}
		}

		// Try direct extension match
		if l := lexers.Get(extNoDot); l != nil {
			return l
		}
	}

	// Try to match by filename
	if l := lexers.Match(path); l != nil {
		return l
	}

	// Fallback
	return lexers.Fallback
}
