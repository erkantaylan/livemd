# renderer.go Documentation

This document provides a line-by-line explanation of `renderer.go`, which handles converting various file types to HTML for display in the live markdown preview server.

## Package and Imports (Lines 1-19)

```go
package main
```
Declares this file as part of the main package, making it part of the executable application.

```go
import (
    "bytes"
    "os"
    "path/filepath"
    "strings"
    "unicode/utf8"
```
**Standard library imports:**
- `bytes`: Provides buffer operations for efficient string building during HTML generation
- `os`: File system operations (reading files)
- `path/filepath`: Cross-platform file path manipulation (extracting extensions, base names)
- `strings`: String manipulation functions (split, join, replace, ToLower)
- `unicode/utf8`: UTF-8 validation for detecting binary files

```go
    "github.com/alecthomas/chroma/v2"
    "github.com/alecthomas/chroma/v2/formatters/html"
    "github.com/alecthomas/chroma/v2/lexers"
    "github.com/alecthomas/chroma/v2/styles"
```
**Chroma syntax highlighting library:**
- `chroma`: Core library for syntax highlighting - tokenizes source code
- `formatters/html`: Converts tokenized code to HTML with styling
- `lexers`: Language-specific tokenizers (Go, Python, JavaScript, etc.)
- `styles`: Color schemes for syntax highlighting (github, monokai, etc.)

```go
    "github.com/yuin/goldmark"
    highlighting "github.com/yuin/goldmark-highlighting/v2"
    "github.com/yuin/goldmark/extension"
    "github.com/yuin/goldmark/parser"
    goldmarkhtml "github.com/yuin/goldmark/renderer/html"
)
```
**Goldmark markdown library:**
- `goldmark`: Core markdown parsing and rendering library
- `goldmark-highlighting`: Integrates Chroma syntax highlighting into markdown code blocks
- `extension`: Goldmark extensions (GFM = GitHub Flavored Markdown)
- `parser`: Parser configuration options
- `renderer/html`: HTML rendering options (aliased as `goldmarkhtml` to avoid conflict with Chroma's html package)

## Constants (Line 21)

```go
const maxLines = 1000
```
Limits the number of lines displayed for code files. Files longer than 1000 lines are truncated to prevent browser performance issues and excessive memory usage.

## Renderer Struct (Lines 23-26)

```go
// Renderer converts files to HTML
type Renderer struct {
    md goldmark.Markdown
}
```
The `Renderer` struct wraps a configured Goldmark markdown processor. The struct encapsulates the markdown engine to allow consistent configuration across all markdown conversions.

## NewRenderer Constructor (Lines 28-47)

```go
func NewRenderer() *Renderer {
    md := goldmark.New(
```
Factory function that creates and configures a new `Renderer` instance.

```go
        goldmark.WithExtensions(
            extension.GFM,
```
Enables GitHub Flavored Markdown (GFM) extension, which adds support for:
- Tables
- Strikethrough text
- Autolinks
- Task lists

```go
            highlighting.NewHighlighting(
                highlighting.WithStyle("github"),
                highlighting.WithFormatOptions(),
            ),
        ),
```
Configures syntax highlighting for fenced code blocks in markdown:
- Uses the "github" color scheme for a familiar look
- `WithFormatOptions()` uses default formatting (inline styles)

```go
        goldmark.WithParserOptions(
            parser.WithAutoHeadingID(),
        ),
```
Automatically generates HTML `id` attributes for headings (e.g., `<h2 id="my-heading">My Heading</h2>`), enabling anchor links to sections.

```go
        goldmark.WithRendererOptions(
            goldmarkhtml.WithHardWraps(),
            goldmarkhtml.WithUnsafe(),
        ),
    )
```
HTML rendering options:
- `WithHardWraps()`: Converts single newlines in markdown to `<br>` tags (matches GitHub behavior)
- `WithUnsafe()`: Allows raw HTML in markdown to pass through unchanged (required for embedded HTML content)

```go
    return &Renderer{md: md}
}
```
Returns a pointer to the new Renderer with the configured markdown engine.

## Main Render Method (Lines 49-67)

```go
func (r *Renderer) Render(filepath string) (string, error) {
    content, err := os.ReadFile(filepath)
    if err != nil {
        return "", err
    }
```
Entry point for rendering any file. Reads the entire file into memory. Returns an error if the file cannot be read (doesn't exist, permission denied, etc.).

```go
    // Check if binary
    if isBinary(content) {
        return renderBinaryMessage(filepath), nil
    }
```
First check: Is this a binary file? If so, display a placeholder message instead of garbled binary data.

```go
    // Check if markdown
    if isMarkdown(filepath) {
        return r.renderMarkdown(content)
    }
```
Second check: Is this a markdown file? If so, render it as formatted HTML using Goldmark.

```go
    // Render as code with syntax highlighting
    return r.renderCode(filepath, content)
}
```
Default case: Treat the file as source code and apply syntax highlighting.

## Markdown Rendering (Lines 69-75)

```go
func (r *Renderer) renderMarkdown(content []byte) (string, error) {
    var buf bytes.Buffer
    if err := r.md.Convert(content, &buf); err != nil {
        return "", err
    }
    return buf.String(), nil
}
```
Converts markdown content to HTML:
1. Creates a `bytes.Buffer` to collect output
2. Calls `r.md.Convert()` to parse markdown and write HTML to the buffer
3. Returns the HTML string or an error if parsing fails

## Code Rendering with Syntax Highlighting (Lines 77-126)

```go
func (r *Renderer) renderCode(path string, content []byte) (string, error) {
    // Limit lines
    lines := strings.Split(string(content), "\n")
    truncated := false
    if len(lines) > maxLines {
        lines = lines[:maxLines]
        truncated = true
    }
    code := strings.Join(lines, "\n")
```
Handles long files:
1. Splits content into lines
2. If more than 1000 lines, truncates and sets a flag
3. Rejoins the (potentially truncated) lines

```go
    // Get lexer
    lexer := getLexer(path)
    if lexer == nil {
        lexer = lexers.Fallback
    }
    lexer = chroma.Coalesce(lexer)
```
Determines which syntax highlighter to use:
1. `getLexer()` attempts to find a language-specific lexer based on filename/extension
2. Razor (`.cshtml`, `.razor`, ```` ```razor ```` fences) uses livemd's own lexer in `razor.go` — Chroma has none
3. Falls back to a generic lexer if none found
4. `Coalesce()` optimizes the lexer by combining adjacent tokens of the same type

```go
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
```
Configures output formatting:
- Uses "github" color scheme (falls back to default if unavailable)
- `WithClasses(false)`: Uses inline styles instead of CSS classes (self-contained HTML)
- `WithLineNumbers(true)`: Adds line numbers to output
- `TabWidth(4)`: Renders tabs as 4 spaces

```go
    // Tokenize and format
    iterator, err := lexer.Tokenise(nil, code)
    if err != nil {
        // Fall back to plain text
        return renderPlainText(code, truncated), nil
    }
```
Tokenizes the source code:
1. `Tokenise()` breaks code into tokens (keywords, strings, comments, etc.)
2. If tokenization fails, falls back to plain text rendering

```go
    var buf bytes.Buffer
    err = formatter.Format(&buf, style, iterator)
    if err != nil {
        return renderPlainText(code, truncated), nil
    }
```
Generates HTML:
1. Creates output buffer
2. `Format()` converts tokens to styled HTML
3. Falls back to plain text if formatting fails

```go
    result := buf.String()
    if truncated {
        result += `<div style="padding: 12px; background: #fff3cd; color: #856404; border-radius: 4px; margin-top: 16px;">
            Showing first 1000 lines. File has more content.
        </div>`
    }

    return result, nil
}
```
Final output:
1. Converts buffer to string
2. If file was truncated, appends a warning banner
3. Returns the complete HTML

## Plain Text Fallback (Lines 128-142)

```go
func renderPlainText(code string, truncated bool) string {
    escaped := strings.ReplaceAll(code, "&", "&amp;")
    escaped = strings.ReplaceAll(escaped, "<", "&lt;")
    escaped = strings.ReplaceAll(escaped, ">", "&gt;")
```
HTML-escapes special characters to prevent XSS and rendering issues:
- `&` becomes `&amp;`
- `<` becomes `&lt;`
- `>` becomes `&gt;`

```go
    result := `<pre style="background: #f6f8fa; padding: 16px; overflow-x: auto; border-radius: 6px; font-family: monospace; font-size: 14px; line-height: 1.45;"><code>` + escaped + `</code></pre>`
```
Wraps content in styled `<pre><code>` tags with:
- Light gray background (`#f6f8fa`)
- Padding and rounded corners
- Horizontal scrolling for long lines
- Monospace font at 14px

```go
    if truncated {
        result += `<div style="padding: 12px; background: #fff3cd; color: #856404; border-radius: 4px; margin-top: 16px;">
            Showing first 1000 lines. File has more content.
        </div>`
    }

    return result
}
```
Adds truncation warning if applicable and returns the HTML.

## Binary File Message (Lines 144-162)

```go
func renderBinaryMessage(path string) string {
    ext := strings.ToLower(filepath.Ext(path))
    name := filepath.Base(path)
```
Extracts the file extension (lowercased) and filename for display.

```go
    // Check if it's an image
    imageExts := map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".svg": true, ".webp": true, ".ico": true}
    if imageExts[ext] {
        return `<div style="text-align: center; padding: 40px;">
            <p style="color: #666; margin-bottom: 16px;">Image file: ` + name + `</p>
            <p style="color: #999; font-size: 14px;">Image preview not supported</p>
        </div>`
    }
```
Special handling for image files: displays a message indicating the file is an image but preview is not supported.

```go
    return `<div style="text-align: center; padding: 40px; color: #666;">
        <p style="font-size: 48px; margin-bottom: 16px;">📦</p>
        <p>Binary file: ` + name + `</p>
        <p style="color: #999; font-size: 14px; margin-top: 8px;">Cannot display binary content</p>
    </div>`
}
```
Generic binary file message with a package emoji icon.

## Markdown File Detection (Lines 164-167)

```go
func isMarkdown(path string) bool {
    ext := strings.ToLower(filepath.Ext(path))
    return ext == ".md" || ext == ".markdown" || ext == ".mdown" || ext == ".mkd"
}
```
Checks if a file is markdown by its extension. Supports common markdown extensions:
- `.md` (most common)
- `.markdown` (verbose)
- `.mdown` (abbreviation)
- `.mkd` (abbreviation)

## Binary File Detection (Lines 169-189)

```go
func isBinary(content []byte) bool {
    // Check first 8000 bytes for null bytes or invalid UTF-8
    checkLen := len(content)
    if checkLen > 8000 {
        checkLen = 8000
    }

    sample := content[:checkLen]
```
Samples up to the first 8000 bytes for binary detection. This is enough to reliably detect binary files without scanning the entire content.

```go
    // Check for null bytes (common in binary files)
    if bytes.Contains(sample, []byte{0}) {
        return true
    }
```
Null bytes (`0x00`) are almost never present in text files but common in binaries (executables, images, etc.).

```go
    // Check if valid UTF-8
    if !utf8.Valid(sample) {
        return true
    }

    return false
}
```
Text files should be valid UTF-8. Invalid UTF-8 sequences indicate binary content.

## Lexer Selection (Lines 191-306)

```go
func getLexer(path string) chroma.Lexer {
    name := strings.ToLower(filepath.Base(path))
    ext := strings.ToLower(filepath.Ext(path))
```
Extracts the lowercase filename and extension for matching.

```go
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
```
**Special filename handling:** Many configuration and build files have no extension or non-standard extensions. This map handles common cases:
- Build files: Makefile, Dockerfile, CMakeLists.txt, Jenkinsfile
- Git config: .gitignore, .gitattributes, .gitmodules
- Shell config: .bashrc, .zshrc, .bash_profile, .env
- Package manifests: go.mod, Cargo.toml, package.json, Gemfile
- Ruby DSL files: Rakefile, Vagrantfile

```go
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
```
**Extension mapping:** Maps file extensions to Chroma lexer names. This handles:
- Alternative extensions (`.yml` -> yaml, `.tsx` -> typescript)
- Short extensions (`.py` -> python, `.rb` -> ruby)
- Platform-specific (`.ps1` -> powershell, `.bat` -> batch)
- Header files (`.h` -> c, `.hpp` -> cpp)

```go
        // Try direct extension match
        if l := lexers.Get(extNoDot); l != nil {
            return l
        }
    }
```
Falls back to using the extension directly as a lexer name (works for many languages like `.go`, `.java`, `.rust`).

```go
    // Try to match by filename
    if l := lexers.Match(path); l != nil {
        return l
    }

    // Fallback
    return lexers.Fallback
}
```
Final fallbacks:
1. `lexers.Match()` uses Chroma's built-in filename matching (handles shebangs, content analysis)
2. Returns the fallback lexer (plain text) if nothing else matches
