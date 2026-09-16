package main

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"
)

// YAML front matter is metadata about a document, not part of it. Left in the
// markdown it renders as content — a "name:" line followed by "description:"
// becomes a setext heading, so a skill file opens with its own description set
// in 28px bold above the actual title. Splitting it out and presenting it as a
// small metadata block says the same thing without shouting.
//
// This is a deliberately shallow reader: top-level "key: value" pairs, block
// scalars, and simple lists. Anything it cannot make sense of is shown as-is
// rather than dropped — the point is to stop it being rendered as prose, not
// to interpret it.

// splitFrontMatter separates a leading --- fenced block from the document body.
// Returns nil front matter when the content does not open with one.
func splitFrontMatter(content []byte) (frontMatter, body []byte) {
	// A UTF-8 BOM before the fence would hide it from the prefix test.
	trimmed := bytes.TrimPrefix(content, []byte{0xEF, 0xBB, 0xBF})
	if !bytes.HasPrefix(trimmed, []byte("---\n")) && !bytes.HasPrefix(trimmed, []byte("---\r\n")) {
		return nil, content
	}

	// Skip the opening fence, then look for the line that closes it.
	rest := trimmed[bytes.IndexByte(trimmed, '\n')+1:]
	offset := 0
	for offset < len(rest) {
		lineEnd := bytes.IndexByte(rest[offset:], '\n')
		var line []byte
		if lineEnd < 0 {
			line = rest[offset:]
		} else {
			line = rest[offset : offset+lineEnd]
		}
		switch strings.TrimRight(string(line), "\r") {
		case "---", "...":
			if lineEnd < 0 {
				return rest[:offset], nil
			}
			return rest[:offset], rest[offset+lineEnd+1:]
		}
		if lineEnd < 0 {
			break
		}
		offset += lineEnd + 1
	}
	// Unterminated fence: treat the whole thing as body rather than swallow it.
	return nil, content
}

// frontMatterField is one key and its value, kept in document order — the
// author's ordering carries meaning that alphabetising would destroy.
type frontMatterField struct {
	Key   string
	Value string
}

// parseFrontMatter reads top-level key/value pairs. Nested mappings are kept as
// their raw text: rendering them properly would mean a YAML parser, and the
// block already reads as reference material rather than prose.
func parseFrontMatter(src []byte) []frontMatterField {
	var fields []frontMatterField
	var current *frontMatterField

	for _, raw := range strings.Split(string(src), "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}

		// An indented line continues the value above it: a block scalar's body,
		// a list item, or a wrapped string.
		if line[0] == ' ' || line[0] == '\t' || strings.HasPrefix(strings.TrimSpace(line), "- ") {
			if current == nil {
				continue
			}
			piece := strings.TrimSpace(line)
			piece = strings.TrimPrefix(piece, "- ")
			if current.Value == "" {
				current.Value = piece
			} else {
				current.Value += " " + piece
			}
			continue
		}

		key, value, found := strings.Cut(line, ":")
		if !found || strings.ContainsAny(key, " \t") {
			// Not a mapping line at all. Attach it to the previous value so
			// nothing disappears.
			if current != nil {
				current.Value = strings.TrimSpace(current.Value + " " + strings.TrimSpace(line))
			}
			continue
		}

		fields = append(fields, frontMatterField{Key: strings.TrimSpace(key), Value: cleanScalar(value)})
		current = &fields[len(fields)-1]
	}
	return fields
}

// cleanScalar strips the syntax around a value that carries no meaning once the
// value is being displayed: quotes, and the |/> block-scalar indicators.
func cleanScalar(v string) string {
	v = strings.TrimSpace(v)
	if v == "|" || v == ">" || v == "|-" || v == ">-" || v == "|+" || v == ">+" {
		return ""
	}
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

// renderFrontMatter emits the metadata block that precedes the document.
// Front matter that parses to nothing is shown verbatim instead of dropped.
func renderFrontMatter(src []byte) string {
	fields := parseFrontMatter(src)
	if len(fields) == 0 {
		if strings.TrimSpace(string(src)) == "" {
			return ""
		}
		return `<div class="front-matter"><pre>` + template.HTMLEscapeString(string(src)) + `</pre></div>`
	}

	var b strings.Builder
	b.WriteString(`<div class="front-matter"><dl>`)
	for _, f := range fields {
		fmt.Fprintf(&b, `<dt>%s</dt><dd>%s</dd>`,
			template.HTMLEscapeString(f.Key),
			template.HTMLEscapeString(f.Value),
		)
	}
	b.WriteString(`</dl></div>`)
	return b.String()
}
