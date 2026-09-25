package main

import (
	. "github.com/alecthomas/chroma/v2" // rule tables read like Chroma's own lexers
	"github.com/alecthomas/chroma/v2/lexers"
)

// Chroma ships no Razor lexer (checked through v2.27.0), so .cshtml/.razor
// files and ```razor fences used to fall through to plain text.
//
// The lexer is two passes, the way Chroma's own template lexers work: the
// razor pass below recognises the C# islands and emits everything else as
// Other, then DelegatingLexer runs the HTML lexer over all the Other text
// stitched back together. HTML therefore sees whole tags even when an
// expression sits inside an attribute — <a href="@Url.Action("x")"> reaches
// it as <a href="">.
//
// This is regex highlighting, not the Razor parser. The known gap is markup
// inside a C# block: a line starting with a tag (or @:) is treated as markup
// up to its end, so an element spanning several lines only has its tag lines
// recognised; the text lines between them are read as C#.
var razorLexer = lexers.Register(DelegatingLexer(lexers.HTML, MustNewLexer(
	&Config{
		Name:      "Razor",
		Aliases:   []string{"razor", "cshtml", "blazor"},
		Filenames: []string{"*.cshtml", "*.razor"},
		MimeTypes: []string{"text/x-razor"},
		DotAll:    true,
	},
	razorRules,
)))

// razorDirectives start a line-long directive whose argument is C#.
const razorDirectives = `page|model|using|inject|inherits|layout|namespace|implements|attribute|typeparam|preservewhitespace|rendermode|addTagHelper|removeTagHelper|tagHelperPrefix`

func razorRules() Rules {
	// Shared by every state that holds C#.
	csharpAtoms := []Rule{
		rule(`//[^\n]*`, CommentSingle, nil),
		rule(`/\*.*?\*/`, CommentMultiline, nil),
		rule(`\$@?"(?:[^"\\]|\\.)*"|@\$?"(?:[^"]|"")*"|"(?:[^"\\\n]|\\.)*"`, LiteralString, nil),
		rule(`'(?:[^'\\\n]|\\.)*'`, LiteralStringChar, nil),
	}
	// Razor constructs that can appear wherever markup can.
	transitions := []Rule{
		rule(`@\*.*?\*@`, CommentMultiline, nil),
		rule(`@@`, Other, nil),
		rule(`(@)(code|functions)(\s*)(\{)`, ByGroups(NameDecorator, Keyword, Text, Punctuation), Push("block")),
		rule(`(@)(section)(\s+)([A-Za-z_]\w*)`, ByGroups(NameDecorator, Keyword, Text, NameVariable), nil),
		rule(`(@)(if|foreach|for|while|switch|lock|try|do)\b`, ByGroups(NameDecorator, Keyword), Push("control")),
		rule(`(@)(using)(?=\s*\()`, ByGroups(NameDecorator, Keyword), Push("control")),
		rule(`(@)(`+razorDirectives+`)\b([^\n]*)`, ByGroups(NameDecorator, Keyword, Using("C#")), nil),
		rule(`(@)(\{)`, ByGroups(NameDecorator, Punctuation), Push("block")),
		rule(`(@)(\()`, ByGroups(NameDecorator, Punctuation), Push("parens")),
		rule(`(@)(await)(\s+)([A-Za-z_]\w*)`, ByGroups(NameDecorator, Keyword, Text, NameVariable), Push("implicit")),
		rule(`(@)([A-Za-z_]\w*)`, ByGroups(NameDecorator, NameVariable), Push("implicit")),
	}

	return Rules{
		"root": concatRules(
			// An @ right after a word character is an e-mail address, not code.
			// Runs are capped so no single match nears Chroma's 250ms regex
			// timeout on a large file; Coalesce joins the pieces back up.
			[]Rule{rule(`(?:[^@]|(?<=[\w.+-])@){1,4096}`, Other, nil)},
			transitions,
			[]Rule{rule(`@`, Other, nil)},
		),
		// A C# block. Nested braces push another block.
		"block": concatRules(
			[]Rule{
				rule(`\}`, Punctuation, Pop(1)),
				rule(`\{`, Punctuation, Push("block")),
				rule(`(?<=^|\n)([ \t]*)(<text>)`, ByGroups(Text, NameTag), Push("text")),
				rule(`(?<=^|\n)([ \t]*)(?=<[A-Za-z/!])`, Text, Push("markupline")),
				rule(`(@)(:)`, ByGroups(NameDecorator, Punctuation), Push("markupline")),
				rule(`(@)(\{)`, ByGroups(NameDecorator, Punctuation), Push("block")),
				rule(`(@)(if|foreach|for|while|switch|lock|try|do|using)\b`, ByGroups(NameDecorator, Keyword), Push("control")),
				rule(`(@)(\()`, ByGroups(NameDecorator, Punctuation), Push("parens")),
			},
			csharpAtoms,
			[]Rule{
				rule(`\b(if|foreach|for|while|switch|lock|using)(\s*)(\()`, ByGroups(Keyword, Text, Punctuation), Push("parens")),
				rule(`[^{}"'/@$<\n]+|[/@$<]`, Using("C#"), nil),
				rule(`\n`, Text, nil),
			},
		),
		// Between @if/@foreach/... and its opening brace. Its block is a
		// "controlblock" so the closing brace can look for else/catch/finally.
		"control": concatRules(
			[]Rule{
				rule(`\s+`, Text, nil),
				rule(`\(`, Punctuation, Push("parens")),
				rule(`\{`, Punctuation, Mutators(Pop(1), Push("controlblock"))),
				rule(`;`, Punctuation, Pop(1)),
			},
			csharpAtoms,
			[]Rule{rule(`[^\s(){;"'/]+|/`, Using("C#"), nil)},
		),
		"controlblock": {
			rule(`\}`, Punctuation, Mutators(Pop(1), Push("aftercontrol"))),
			Include("block"),
		},
		"aftercontrol": {
			rule(`(\s*)(else\s+if|else|catch|finally)\b`, ByGroups(Text, Keyword), Mutators(Pop(1), Push("control"))),
			rule(`(\s*)(while)\b`, ByGroups(Text, Keyword), Mutators(Pop(1), Push("control"))),
			rule(``, nil, Pop(1)),
		},
		// Balanced parentheses, for @( ), call arguments and control headers.
		"parens": concatRules(
			[]Rule{
				rule(`\)`, Punctuation, Pop(1)),
				rule(`\(`, Punctuation, Push("parens")),
			},
			csharpAtoms,
			[]Rule{rule(`[^()"'/]+|/`, Using("C#"), nil)},
		),
		"brackets": concatRules(
			[]Rule{
				rule(`\]`, Punctuation, Pop(1)),
				rule(`\[`, Punctuation, Push("brackets")),
			},
			csharpAtoms,
			[]Rule{rule(`[^\[\]"'/]+|/`, Using("C#"), nil)},
		),
		// The tail of @Model.Items[0].Name(x): member access, calls and
		// indexers, ending at the first character that cannot continue it.
		"implicit": {
			rule(`(\??\.)([A-Za-z_]\w*)`, ByGroups(Punctuation, NameVariable), nil),
			rule(`\(`, Punctuation, Push("parens")),
			rule(`\[`, Punctuation, Push("brackets")),
			rule(``, nil, Pop(1)),
		},
		// Markup inside a C# block, up to the end of the line.
		"markupline": concatRules(
			[]Rule{
				rule(`\n`, Other, Pop(1)),
				rule(`(?:[^@\n]|(?<=[\w.+-])@){1,4096}`, Other, nil),
			},
			transitions,
			[]Rule{rule(`@`, Other, nil)},
		),
		// <text>…</text> inside a C# block: markup until the closing tag.
		"text": concatRules(
			[]Rule{
				rule(`</text>`, NameTag, Pop(1)),
				rule(`(?:[^@<]|(?<=[\w.+-])@|<(?!/text>)){1,4096}`, Other, nil),
			},
			transitions,
			[]Rule{rule(`@`, Other, nil)},
		),
	}
}

// rule builds a Rule; go vet rejects the unkeyed Rule{...} literals Chroma's
// own lexers use, and keyed ones would bury the patterns.
func rule(pattern string, emitter Emitter, mutator Mutator) Rule {
	return Rule{Pattern: pattern, Type: emitter, Mutator: mutator}
}

func concatRules(parts ...[]Rule) []Rule {
	var out []Rule
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}
