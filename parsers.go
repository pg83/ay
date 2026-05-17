package main

import (
	"bytes"
	"regexp"
	"strings"
)

// parsers.go — raw include-directive scanners, selected by file
// extension before any resolution/search-path/sysincl logic runs.
//
// This mirrors the first half of upstream ymake's parser manager:
// choose a parser by ext, scan raw directives, then feed the result into
// the separate include resolver.

// includeRe matches `#include` / `#include_next` directives in their
// angle-bracket and quoted-string forms, tolerating arbitrary
// whitespace between `#`, the keyword, and the bracket. Two capture
// groups: directive (`include` or `include_next`) and target.
var includeRe = regexp.MustCompile(`^\s*#\s*(include|include_next)\s*[<"]([^>"]+)[>"]`)

// yasmIncludeRe matches NASM/yasm `%include` directives in `.asm` /
// `.asi` sources. Token is case-insensitive (`%include` and `%INCLUDE`
// both occur in asmlib). Both quoted and angle-bracket forms accepted;
// only quoted appears in practice. Single capture group: target.
var yasmIncludeRe = regexp.MustCompile(`(?i)^\s*%\s*include\s*[<"]([^>"]+)[>"]`)

// macroIndirectIncludes augments the C-like raw scanner for sources
// that use macro-indirect `#include MACRO_NAME` forms. The text-blind
// scanner cannot expand macros, so e.g. `#include OPENSSL_UNISTD`
// parses to nothing. Each entry lists the include targets the source's
// macro-indirect lines expand to on a linux-musl target — what the
// upstream scanner emits. Resolution flows through the normal
// resolve()/sysincl pipeline.
type macroIndirectInclude struct {
	target string
	kind   includeKind
}

var macroIndirectIncludes = map[string][]macroIndirectInclude{
	"contrib/libs/openssl/crypto/rand/rand_egd.c": {{target: "unistd.h", kind: includeSystem}},
	"contrib/libs/openssl/crypto/uid.c":           {{target: "unistd.h", kind: includeSystem}},
	// pugixml.hpp's header-only-mode trailer:
	//   #if defined(PUGIXML_HEADER_ONLY) && !defined(PUGIXML_SOURCE)
	//   #    define PUGIXML_SOURCE "pugixml.cpp"
	//   #    include PUGIXML_SOURCE
	// The macro-indirect `#include PUGIXML_SOURCE` expands to a quoted
	// include of pugixml.cpp, which then pulls in the standard <float.h>
	// + <setjmp.h> XPath dependencies — both reach musl-self on linux.
	"contrib/libs/pugixml/pugixml.hpp": {{target: "pugixml.cpp", kind: includeQuoted}},
}

type includeDirectiveParser interface {
	Parse(vfsPath VFS, data []byte) []includeDirective
}

type includeDirectiveParserRegistry struct {
	defaultParser includeDirectiveParser
	byExt         map[string]includeDirectiveParser
}

type cIncludeDirectiveParser struct{}
type yasmIncludeDirectiveParser struct{}
type emptyIncludeDirectiveParser struct{}

var includeDirectiveParsers = newIncludeDirectiveParserRegistry()

func newIncludeDirectiveParserRegistry() includeDirectiveParserRegistry {
	cLike := cIncludeDirectiveParser{}
	yasm := yasmIncludeDirectiveParser{}
	empty := emptyIncludeDirectiveParser{}

	r := includeDirectiveParserRegistry{
		defaultParser: cLike,
		byExt:         make(map[string]includeDirectiveParser, 48),
	}

	// Keep the explicit ext table close to upstream parser_manager.cpp,
	// but preserve current yatool behaviour by falling back to the
	// C-like parser for any not-yet-modelled extension.
	r.register(cLike,
		".cpp", ".cc", ".cxx", ".c", ".C", ".auxcpp",
		".h", ".hh", ".hpp", ".cuh", ".H", ".hxx", ".xh", ".ipp", ".ixx", ".inl",
		".vert", ".frag", ".tesc", ".tese", ".geom", ".comp",
		".cu", ".S", ".s", ".sfdl", ".m", ".mm",
		".l", ".lex", ".lpp", ".y", ".ypp", ".gperf", ".asp",
		".rl", ".rh", ".rli", ".rl6", ".rl5",
		".proto", ".ev", ".gzt", ".gztproto",
		".go",
	)
	r.register(yasm, ".asm", ".asi")
	r.register(empty, ".g4")

	return r
}

func (r includeDirectiveParserRegistry) register(parser includeDirectiveParser, exts ...string) {
	for _, ext := range exts {
		r.byExt[ext] = parser
	}
}

func (r includeDirectiveParserRegistry) parserFor(vfsPath VFS) includeDirectiveParser {
	if parser, ok := r.byExt[directiveParserExt(vfsPath.Rel)]; ok {
		return parser
	}

	return r.defaultParser
}

func directiveParserExt(rel string) string {
	// Match upstream parser-manager behaviour: `foo.ext.in` is scanned
	// with the parser for `foo.ext`, not the literal `.in` suffix.
	if strings.HasSuffix(rel, ".in") {
		rel = strings.TrimSuffix(rel, ".in")
	}

	idx := strings.LastIndexByte(rel, '.')
	if idx < 0 {
		return ""
	}

	return rel[idx:]
}

func (cIncludeDirectiveParser) Parse(vfsPath VFS, data []byte) []includeDirective {
	out := parseCIncludes(data)

	if extras, ok := macroIndirectIncludes[vfsPath.Rel]; ok {
		for _, m := range extras {
			out = append(out, includeDirective{kind: m.kind, target: m.target})
		}
	}

	return out
}

func (yasmIncludeDirectiveParser) Parse(_ VFS, data []byte) []includeDirective {
	return parseYasmIncludes(data)
}

func (emptyIncludeDirectiveParser) Parse(_ VFS, _ []byte) []includeDirective {
	return nil
}

// parseCIncludes extracts C/C++ `#include` / `#include_next`
// directives from `data`. stripComments runs first so the regex never
// matches include text inside non-code spans.
func parseCIncludes(data []byte) []includeDirective {
	data = stripComments(data)

	out := make([]includeDirective, 0, 8)

	eachLine(data, func(line []byte) {
		// Short-circuit lines without `#` before the regex.
		// stripComments fills block-comment regions with spaces, and
		// the `^\s*#` anchor would otherwise greedily match leading
		// whitespace, multiplying regex cost ~3× on the M2 closure.
		if bytes.IndexByte(line, '#') < 0 {
			return
		}

		// FindSubmatchIndex returns offsets in a stack-cap'd []int (no
		// alloc for tiny matches); the [][]byte form allocates a slice
		// header per call.
		m := includeRe.FindSubmatchIndex(line)

		if m == nil {
			return
		}

		// Determine kind by inspecting the line's bracket character
		// after the keyword.
		kind := includeSystem
		idx := indexOfAngleOrQuote(line)

		if idx >= 0 && line[idx] == '"' {
			kind = includeQuoted
		}

		// m[2:4] are start/end offsets of the directive keyword
		// (`include` or `include_next`). Comparing on length avoids
		// `string(line[m[2]:m[3]])` allocation per matched line.
		next := (m[3] - m[2]) == len("include_next")

		// m[4:6] are the target capture's byte offsets. The single
		// remaining string allocation per match is converting the
		// target bytes to a string for the cache value.
		target := string(line[m[4]:m[5]])

		out = append(out, includeDirective{kind: kind, next: next, target: target})
	})

	return out
}

// parseYasmIncludes extracts NASM/yasm `%include` directives from
// `data`. Token matches case-insensitively; yasm's `;` line comments
// are not stripped (the anchor cannot fire from a comment line, and
// yasm has no C-style block comments). String literals are preserved
// verbatim — the directive's quoted form IS a string literal at lexer
// level. Result uses includeDirective with next=false (no
// `%include_next` exists in NASM).
func parseYasmIncludes(data []byte) []includeDirective {
	out := make([]includeDirective, 0, 4)

	eachLine(data, func(line []byte) {
		// Short-circuit lines without `%` before invoking the regex
		// engine — most yasm source lines are instruction mnemonics
		// or labels that never start with `%`.
		if bytes.IndexByte(line, '%') < 0 {
			return
		}

		m := yasmIncludeRe.FindSubmatchIndex(line)

		if m == nil {
			return
		}

		// Discriminate kind by bracket character. Practice is always
		// quoted; angle-bracket branch is kept for C-scanner parity.
		kind := includeSystem

		idx := indexOfAngleOrQuote(line)
		if idx >= 0 && line[idx] == '"' {
			kind = includeQuoted
		}

		// m[2:4] are the target capture's byte offsets (the regex has
		// only one capture group; m[0:2] is the full match span).
		target := string(line[m[2]:m[3]])

		out = append(out, includeDirective{kind: kind, next: false, target: target})
	})

	return out
}
