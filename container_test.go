package main

import (
	"bytes"
	"strings"
	"testing"
)

func renderMD(t *testing.T, src string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := NewRenderer().md.Convert([]byte(src), &buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestContainers(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		want      []string
		reject    []string
	}{
		{
			name:   "callout with a title and markdown body",
			src:    ":::question Açık\n**$1** Tarih doğru mu?\n:::\n\nAfter\n",
			want:   []string{`<div class="callout callout-question"><p class="callout-title">Açık</p>`, `<strong>$1</strong>`, `</div>`, `<p>After</p>`},
			reject: []string{":::"},
		},
		{
			name: "no title falls back to the kind",
			src:  ":::warning\nCareful\n:::\n",
			want: []string{`<p class="callout-title">Warning</p>`},
		},
		{
			name: "kind is case-insensitive, space after the fence allowed",
			src:  "::: NOTE Heads up\nx\n:::\n",
			want: []string{`callout-note`, `>Heads up</p>`},
		},
		{
			name: "details collapses",
			src:  ":::details More\nHidden\n:::\n",
			want: []string{`<details class="callout"><summary class="callout-title">More</summary>`, `<p>Hidden</p>`, `</details>`},
		},
		{
			name:   "unknown kind is a neutral box, its name never a class",
			src:    ":::custom-thing\nx\n:::\n",
			want:   []string{`<div class="callout"><p class="callout-title">Custom-thing</p>`},
			reject: []string{"callout-custom"},
		},
		{
			name:   "title is escaped",
			src:    ":::note <b>x</b>\nbody\n:::\n",
			want:   []string{`&lt;b&gt;x&lt;/b&gt;`},
			reject: []string{"<b>x</b>"},
		},
		{
			name: "longer outer fence nests",
			src:  "::::details Outer\n:::note Inner\nbody\n:::\nstill outer\n::::\n\nAfter\n",
			want: []string{"callout-note", "<p>body</p>\n</div>\n<p>still outer</p>\n</details>", "<p>After</p>"},
		},
		{
			name:   "a fence in a code sample does not close the box",
			src:    ":::note\n```\n:::\n```\nstill inside\n:::\n",
			want:   []string{"<p>still inside</p>\n</div>"},
			reject: []string{"<p>:::</p>"},
		},
		{
			name: "a table, list and blank lines stay inside",
			src:  ":::tip\n- a\n- b\n\n| x |\n| - |\n| 1 |\n\nend\n:::\n",
			want: []string{"<ul>", "<table>", "<p>end</p>\n</div>"},
		},
		{
			name: "opens straight after a paragraph",
			src:  "Before\n:::note\nx\n:::\n",
			want: []string{"<p>Before</p>", "callout-note"},
		},
		{
			name: "closes at end of file without a newline",
			src:  ":::note\nx\n:::",
			want: []string{"<p>x</p>\n</div>"},
		},
		{
			name: "CRLF line endings",
			src:  ":::note Title\r\nx\r\n:::\r\n\r\nAfter\r\n",
			want: []string{">Title</p>", "<p>After</p>"},
		},
		{
			name: "unclosed runs to the end, like a code fence",
			src:  ":::note\nx\n\ny\n",
			want: []string{"<p>y</p>\n</div>"},
		},
		{
			name:   "not a container: bare fence, two colons, mid-line",
			src:    ":::\n\n::note\n\nsee :::note here\n",
			want:   []string{"<p>:::</p>", "<p>::note</p>", "see :::note here"},
			reject: []string{"callout"},
		},
		{
			name:   "not a container: indented as code",
			src:    "    :::note\n",
			want:   []string{"<pre>"},
			reject: []string{"callout"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			html := renderMD(t, tc.src)
			for _, want := range tc.want {
				if !strings.Contains(html, want) {
					t.Errorf("%q missing from:\n%s", want, html)
				}
			}
			for _, reject := range tc.reject {
				if strings.Contains(html, reject) {
					t.Errorf("%q should not appear in:\n%s", reject, html)
				}
			}
		})
	}
}
