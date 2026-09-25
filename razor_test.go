package main

import (
	"os"
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

func TestRazorLexerLookup(t *testing.T) {
	for _, path := range []string{"Pages/Index.cshtml", "Shared/NavMenu.razor", "VIEW.CSHTML"} {
		if name := getLexer(path).Config().Name; name != "Razor" {
			t.Errorf("getLexer(%q) = %s, want Razor", path, name)
		}
	}
	// Markdown fences resolve by name, not path.
	for _, lang := range []string{"razor", "cshtml", "blazor"} {
		if l := lexers.Get(lang); l == nil || l.Config().Name != "Razor" {
			t.Errorf("lexers.Get(%q) is not the Razor lexer", lang)
		}
	}
}

// razorTokens lexes src and returns its non-whitespace tokens.
func razorTokens(t *testing.T, src string) []chroma.Token {
	t.Helper()
	it, err := razorLexer.Tokenise(nil, src)
	if err != nil {
		t.Fatal(err)
	}
	var out []chroma.Token
	for _, tok := range it.Tokens() {
		if tok.Type == chroma.Error {
			t.Errorf("error token %q", tok.Value)
		}
		if strings.TrimSpace(tok.Value) != "" {
			out = append(out, tok)
		}
	}
	return out
}

// hasRun reports whether want appears as consecutive tokens in got.
func hasRun(got []chroma.Token, want ...chroma.Token) bool {
	for i := 0; i+len(want) <= len(got); i++ {
		match := true
		for j, w := range want {
			if got[i+j] != w {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func tok(tt chroma.TokenType, v string) chroma.Token { return chroma.Token{Type: tt, Value: v} }

func TestRazorFixtureTokens(t *testing.T) {
	src, err := os.ReadFile("testdata/render/razor.cshtml")
	if err != nil {
		t.Fatal(err)
	}
	toks := razorTokens(t, string(src))
	at := tok(chroma.NameDecorator, "@")

	for _, c := range []struct {
		desc string
		run  []chroma.Token
	}{
		{"directive with C# argument", []chroma.Token{at, tok(chroma.Keyword, "model"), tok(chroma.Name, "OrdersModel")}},
		{"razor comment is one token", []chroma.Token{tok(chroma.CommentMultiline, "@* A Razor comment: <b>not markup</b>, @not.code *@")}},
		{"code block is C#", []chroma.Token{tok(chroma.KeywordType, "var"), tok(chroma.Name, "total")}},
		{"markup around expressions is HTML", []chroma.Token{tok(chroma.NameTag, "h1"), tok(chroma.NameAttribute, "class")}},
		{"implicit expression with indexer", []chroma.Token{at, tok(chroma.NameVariable, "ViewData"), tok(chroma.Punctuation, "["), tok(chroma.LiteralString, `"Title"`)}},
		{"expression inside an attribute", []chroma.Token{tok(chroma.LiteralString, `"`), at, tok(chroma.NameVariable, "Url")}},
		{"explicit expression", []chroma.Token{at, tok(chroma.Punctuation, "("), tok(chroma.Name, "total")}},
		{"await expression", []chroma.Token{at, tok(chroma.Keyword, "await"), tok(chroma.NameVariable, "Model")}},
		{"null-conditional member", []chroma.Token{tok(chroma.Punctuation, "?."), tok(chroma.NameVariable, "Name")}},
		{"else if continues the chain", []chroma.Token{tok(chroma.Punctuation, "}"), tok(chroma.Keyword, "else if")}},
		{"markup line inside a block", []chroma.Token{tok(chroma.NameTag, "li"), tok(chroma.NameAttribute, "class")}},
		{"@: line", []chroma.Token{at, tok(chroma.Punctuation, ":")}},
		{"<text> element", []chroma.Token{tok(chroma.NameVariable, "Year"), tok(chroma.NameTag, "</text>")}},
		{"catch after try", []chroma.Token{tok(chroma.Punctuation, "}"), tok(chroma.Keyword, "catch")}},
		{"@code block", []chroma.Token{at, tok(chroma.Keyword, "code"), tok(chroma.Punctuation, "{")}},
		{"interpolated string", []chroma.Token{tok(chroma.LiteralString, `$"#{n:D5}"`)}},
	} {
		if !hasRun(toks, c.run...) {
			t.Errorf("%s: %v not found", c.desc, c.run)
		}
	}

	// An e-mail address and the @@ escape are text, not expressions.
	for _, name := range []string{"example", "handle"} {
		if hasRun(toks, tok(chroma.NameVariable, name)) {
			t.Errorf("%q lexed as an expression", name)
		}
	}
	// The file ends back in markup: every brace was matched.
	if last := toks[len(toks)-1]; last != tok(chroma.Punctuation, "}") {
		t.Errorf("last token %v, want the @code block's closing brace", last)
	}
}

// Broken input must still render: an unclosed block runs to the end of the file.
func TestRazorUnterminated(t *testing.T) {
	for _, src := range []string{"@{ var x = 1;", "@(a + (b", "@if (x) {\n<p>@y", "@* open comment", "<p>@</p>"} {
		it, err := razorLexer.Tokenise(nil, src)
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		var got strings.Builder
		for _, tk := range it.Tokens() {
			got.WriteString(tk.Value)
		}
		if strings.TrimRight(got.String(), "\n") != src {
			t.Errorf("%q: tokens rebuild to %q", src, got.String())
		}
	}
}

func TestRazorFenceHighlighted(t *testing.T) {
	html := fixture(t, "code.md")
	// Unhighlighted, the fence would come out as "@foreach" in one plain run.
	if !strings.Contains(html, `>@</span><span style="color:#000;font-weight:bold">foreach</span>`) {
		t.Error("razor fence not highlighted")
	}
}
