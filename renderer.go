package main

import (
	"bytes"
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
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"
)

const maxLines = 1000

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
		),
		goldmark.WithRendererOptions(
			goldmarkhtml.WithHardWraps(),
			goldmarkhtml.WithUnsafe(),
		),
	)

	return &Renderer{md: md}
}

func (r *Renderer) Render(filepath string) (string, error) {
	content, err := os.ReadFile(filepath)
	if err != nil {
		return "", err
	}

	// Check if binary
	if isBinary(content) {
		return renderBinaryMessage(filepath), nil
	}

	// Check if markdown
	if isMarkdown(filepath) {
		return r.renderMarkdown(content)
	}

	// Render as code with syntax highlighting
	return r.renderCode(filepath, content)
}

func (r *Renderer) renderMarkdown(content []byte) (string, error) {
	var buf bytes.Buffer
	if err := r.md.Convert(content, &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (r *Renderer) renderCode(path string, content []byte) (string, error) {
	// Limit lines
	lines := strings.Split(string(content), "\n")
	truncated := false
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		truncated = true
	}
	code := strings.Join(lines, "\n")

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
		html.WithLineNumbers(true),
		html.TabWidth(4),
	)

	// Tokenize and format
	iterator, err := lexer.Tokenise(nil, code)
	if err != nil {
		// Fall back to plain text
		return renderPlainText(code, truncated), nil
	}

	var buf bytes.Buffer
	err = formatter.Format(&buf, style, iterator)
	if err != nil {
		return renderPlainText(code, truncated), nil
	}

	result := buf.String()
	if truncated {
		result += `<div style="padding: 12px; background: #fff3cd; color: #856404; border-radius: 4px; margin-top: 16px;">
			Showing first 1000 lines. File has more content.
		</div>`
	}

	return result, nil
}

func renderPlainText(code string, truncated bool) string {
	escaped := strings.ReplaceAll(code, "&", "&amp;")
	escaped = strings.ReplaceAll(escaped, "<", "&lt;")
	escaped = strings.ReplaceAll(escaped, ">", "&gt;")

	result := `<pre style="background: #f6f8fa; padding: 16px; overflow-x: auto; border-radius: 6px; font-family: monospace; font-size: 14px; line-height: 1.45;"><code>` + escaped + `</code></pre>`

	if truncated {
		result += `<div style="padding: 12px; background: #fff3cd; color: #856404; border-radius: 4px; margin-top: 16px;">
			Showing first 1000 lines. File has more content.
		</div>`
	}

	return result
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
