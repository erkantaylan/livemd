package main

import (
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// Markdown links and images are written for the filesystem — [setup](./setup.md),
// ![](img/logo.png) — but goldmark emits them verbatim, so the browser resolves
// them against the page URL and gets nothing. linkTransformer rewrites them
// against the document's own directory: links become the app's URL for that
// file, images point at /raw. Whether the target may actually be read is the
// server's call (see resolveReadable) — this only makes the reference point at
// the right place.
type linkTransformer struct{}

// baseDirKey carries the rendered document's directory into the transformer.
// It travels through parser.Context rather than a field on the transformer
// because one goldmark instance is shared across concurrent renders.
var baseDirKey = parser.NewContextKey()

func (t *linkTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	baseDir, _ := pc.Get(baseDirKey).(string)
	if baseDir == "" {
		return
	}
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch node := n.(type) {
		case *ast.Image:
			if dest, ok := rewriteRef(baseDir, string(node.Destination), true); ok {
				node.Destination = []byte(dest)
			}
		case *ast.AutoLink:
			// Bare URLs in the text are always off-site.
			markExternal(node)
		case *ast.Link:
			dest := string(node.Destination)
			if isExternalRef(dest) {
				markExternal(node)
				return ast.WalkContinue, nil
			}
			if rewritten, ok := rewriteRef(baseDir, dest, false); ok {
				node.Destination = []byte(rewritten)
			}
		}
		return ast.WalkContinue, nil
	})
}

// markExternal sends off-site links to a new tab. Without it, clicking one
// replaces the app in this tab — losing the WebSocket and the file list for
// the sake of a link the reader probably wanted alongside the document.
func markExternal(n ast.Node) {
	n.SetAttributeString("target", []byte("_blank"))
	n.SetAttributeString("rel", []byte("noopener noreferrer"))
}

// A scheme needs two or more characters before the colon, which is what keeps
// a Windows drive letter (C:\docs\a.md) on the filesystem side of the fence.
var urlSchemeRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.\-]+:`)

// isExternalRef reports whether a destination addresses something off-disk: a
// scheme (http:, mailto:, data:) or a protocol-relative //host URL.
func isExternalRef(dest string) bool {
	return strings.HasPrefix(dest, "//") || urlSchemeRe.MatchString(dest)
}

// rewriteRef converts one markdown destination into a URL this server serves.
// Returns ok=false for references that must be left exactly as written: empty
// ones, bare #fragments, and anything external.
func rewriteRef(baseDir, dest string, isImage bool) (string, bool) {
	if dest == "" || strings.HasPrefix(dest, "#") || isExternalRef(dest) {
		return "", false
	}

	// Split off the fragment — it addresses a heading in the target document,
	// not part of its filename — and drop any query string, which means
	// nothing to a file on disk.
	path, fragment := dest, ""
	if i := strings.IndexByte(path, '#'); i >= 0 {
		path, fragment = path[:i], path[i:]
	}
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	if path == "" {
		return "", false
	}
	// Destinations are URLs, so "my%20notes.md" names a file with a space.
	if decoded, err := url.PathUnescape(path); err == nil {
		path = decoded
	}

	local := filepath.FromSlash(path)
	if !filepath.IsAbs(local) {
		local = filepath.Join(baseDir, local)
	} else {
		local = filepath.Clean(local)
	}

	if isImage {
		return "/raw?path=" + url.QueryEscape(local), true
	}
	return filePathToURLPath(local) + fragment, true
}

// filePathToURLPath renders an absolute filesystem path as the URL this server
// serves it at — the path itself. /home/me/doc.md is reachable at
// http://host:port/home/me/doc.md, which is what makes a path copied from a
// terminal a working link, and a link copied from the page a working path.
// Windows paths keep their drive letter behind the URL's leading slash:
// C:\Users\me\doc.md becomes /C:/Users/me/doc.md.
func filePathToURLPath(abs string) string {
	slashed := filepath.ToSlash(abs)
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	// url.URL escapes what a path segment may not contain (spaces, ?, #, %)
	// and leaves the rest — including the drive-letter colon — readable.
	u := url.URL{Path: slashed}
	return u.EscapedPath()
}
