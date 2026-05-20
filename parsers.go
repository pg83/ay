package main

import (
	"bytes"
	"path"
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

var swigIncludeRe = regexp.MustCompile(`^%(include|import|insert\s*\([^\)]*\))\s*([<"].*?[">])`)
var cythonCimportFromRe = regexp.MustCompile(`^\s*from\s+([A-Za-z0-9_\.]+)\s+cimport\b`)
var cythonCimportRe = regexp.MustCompile(`^\s*cimport\s+(.+)$`)
var cythonIncludeRe = regexp.MustCompile(`^\s*include\s+["']([^"']+)["']`)
var cythonExternFromRe = regexp.MustCompile(`^\s*cdef\s+extern\s+from\s+(<[^>]+>|"[^"]+"|'[^']+')`)

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
	Parse(rel string, data []byte) parsedIncludeSet
}

type includeDirectiveParserRegistry struct {
	defaultParser includeDirectiveParser
	byExt         map[string]includeDirectiveParser
}

type cIncludeDirectiveParser struct{}
type cythonIncludeDirectiveParser struct{}
type protoIncludeDirectiveParser struct{}
type ragelIncludeDirectiveParser struct{}
type swigIncludeDirectiveParser struct{}
type yasmIncludeDirectiveParser struct{}
type emptyIncludeDirectiveParser struct{}

var includeDirectiveParsers = newIncludeDirectiveParserRegistry()

func newIncludeDirectiveParserRegistry() includeDirectiveParserRegistry {
	cLike := cIncludeDirectiveParser{}
	protoLike := protoIncludeDirectiveParser{}
	ragelLike := ragelIncludeDirectiveParser{}
	swigLike := swigIncludeDirectiveParser{}
	yasm := yasmIncludeDirectiveParser{}
	empty := emptyIncludeDirectiveParser{}

	r := includeDirectiveParserRegistry{
		defaultParser: cLike,
		byExt:         make(map[string]includeDirectiveParser, 48),
	}

	// Keep the explicit ext table close to upstream parser_manager.cpp,
	// but preserve current ay behaviour by falling back to the
	// C-like parser for any not-yet-modelled extension.
	r.register(cLike,
		".cpp", ".cc", ".cxx", ".c", ".C", ".auxcpp",
		".h", ".hh", ".hpp", ".cuh", ".H", ".hxx", ".xh", ".ipp", ".ixx", ".inl",
		".vert", ".frag", ".tesc", ".tese", ".geom", ".comp",
		".cu", ".S", ".s", ".sfdl", ".m", ".mm",
		".l", ".lex", ".lpp", ".y", ".ypp", ".gperf", ".asp",
		".go",
	)
	r.register(ragelLike, ".rl", ".rh", ".rli", ".rl6", ".rl5")
	r.register(cythonIncludeDirectiveParser{}, ".pyx", ".pxd", ".pxi", ".pyx.pxi", ".pxd.pxi")
	r.register(protoLike, ".proto", ".ev", ".gzt", ".gztproto")
	r.register(swigLike, ".swg")
	r.register(yasm, ".asm", ".asi")
	r.register(empty, ".g4")

	return r
}

func (r includeDirectiveParserRegistry) register(parser includeDirectiveParser, exts ...string) {
	for _, ext := range exts {
		r.byExt[ext] = parser
	}
}

func (r includeDirectiveParserRegistry) parserFor(rel string) includeDirectiveParser {
	if parser, ok := r.byExt[directiveParserExt(rel)]; ok {
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

func (cIncludeDirectiveParser) Parse(rel string, data []byte) parsedIncludeSet {
	out := parseCIncludes(data)

	if extras, ok := macroIndirectIncludes[rel]; ok {
		for _, m := range extras {
			out = append(out, includeDirective{kind: m.kind, target: m.target})
		}
	}

	return rawParsedIncludeSet(parsedIncludesLocal, out...)
}

func (cythonIncludeDirectiveParser) Parse(rel string, data []byte) parsedIncludeSet {
	out := make([]includeDirective, 0, 8)
	seen := make(map[includeDirective]struct{}, 8)
	add := func(d includeDirective) {
		if _, dup := seen[d]; dup {
			return
		}
		seen[d] = struct{}{}
		out = append(out, d)
	}

	if strings.HasSuffix(rel, ".pyx") {
		base := strings.TrimSuffix(rel, ".pyx")
		add(includeDirective{kind: includeQuoted, target: path.Base(base) + ".pxd"})
	}

	eachLine(data, func(line []byte) {
		s := strings.TrimSpace(string(line))
		if s == "" || strings.HasPrefix(s, "#") {
			return
		}

		if m := cythonIncludeRe.FindStringSubmatch(s); len(m) == 2 {
			add(includeDirective{kind: includeQuoted, target: m[1]})
			return
		}

		if m := cythonExternFromRe.FindStringSubmatch(s); len(m) == 2 {
			target, kind, ok := parseDelimitedIncludeTarget(m[1])
			if ok {
				add(includeDirective{kind: kind, target: target})
			}
			return
		}

		if m := cythonCimportFromRe.FindStringSubmatch(s); len(m) == 2 {
			add(includeDirective{kind: includeQuoted, target: cythonPxdTarget(m[1])})
			return
		}

		if m := cythonCimportRe.FindStringSubmatch(s); len(m) == 2 {
			for _, part := range strings.Split(m[1], ",") {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				if idx := strings.IndexAny(part, " \t"); idx >= 0 {
					part = part[:idx]
				}
				add(includeDirective{kind: includeQuoted, target: cythonPxdTarget(part)})
			}
		}
	})

	return rawParsedIncludeSet(parsedIncludesLocal, out...)
}

func cythonPxdTarget(module string) string {
	switch module {
	case "cpython", "libc", "libcpp", "cython":
		return module + "/__init__.pxd"
	default:
		return strings.ReplaceAll(module, ".", "/") + ".pxd"
	}
}

func (protoIncludeDirectiveParser) Parse(_ string, data []byte) parsedIncludeSet {
	local := make([]includeDirective, 0, 8)
	hcpp := make([]includeDirective, 0, 8)

	eachLine(data, func(line []byte) {
		target, kind, ok := parseProtoImportLine(line)
		if !ok {
			return
		}

		local = append(local, includeDirective{kind: kind, target: target})

		// HCPP bucket carries the codegen schema: protoc-generated
		// outputs `#include` the .pb.h / .ev.pb.h of each import.
		// Paths are import-relative — the walker applies the codegen
		// output-root prefix and the runtime-base prefix for the
		// descriptor.proto special case.
		switch {
		case strings.HasSuffix(target, ".ev"):
			hcpp = append(hcpp, includeDirective{kind: kind, target: strings.TrimSuffix(target, ".ev") + ".ev.pb.h"})
		case strings.HasSuffix(target, ".proto"):
			hcpp = append(hcpp, includeDirective{kind: kind, target: strings.TrimSuffix(target, ".proto") + ".pb.h"})
		}
	})

	set := rawParsedIncludeSet(parsedIncludesLocal, local...)
	set = appendParsedDirectives(set, parsedIncludesHCPP, hcpp...)
	return set
}

func (ragelIncludeDirectiveParser) Parse(rel string, data []byte) parsedIncludeSet {
	local := parseCIncludes(data)
	if len(local) == 0 {
		local = make([]includeDirective, 0, 4)
	}

	seenNative := make(map[string]struct{}, 4)
	inSpecification := false

	eachLine(data, func(line []byte) {
		trimmed := strings.TrimLeft(string(line), " \t")
		if trimmed == "" {
			return
		}

		switch {
		case strings.HasPrefix(trimmed, "%%{"):
			inSpecification = true
			return
		case strings.HasPrefix(trimmed, "}%%"):
			inSpecification = false
			return
		}

		if !inSpecification {
			return
		}

		target, ok := parseRagelNativeIncludeLine(trimmed)
		if !ok {
			return
		}

		if _, dup := seenNative[target]; dup {
			return
		}
		seenNative[target] = struct{}{}
		local = append(local, includeDirective{kind: includeQuoted, target: target})
	})

	var set parsedIncludeSet
	set = appendParsedDirectives(set, parsedIncludesLocal, local...)
	set = appendParsedDirectives(set, parsedIncludesHCPP, includeDirective{
		kind:   includeQuoted,
		target: rel,
	})

	return set
}

func (swigIncludeDirectiveParser) Parse(_ string, data []byte) parsedIncludeSet {
	direct := make([]includeDirective, 0, 8)
	induced := make([]includeDirective, 0, 4)
	inBlock := false

	eachLine(data, func(line []byte) {
		trimmed := strings.TrimSpace(string(line))
		if trimmed == "" {
			return
		}

		if !inBlock && (strings.HasPrefix(trimmed, "%include") || strings.HasPrefix(trimmed, "%import") || strings.HasPrefix(trimmed, "%insert")) {
			target, kind, ok := parseSwigIncludeLine(trimmed)
			if ok {
				direct = append(direct, includeDirective{kind: kind, target: target})
			}
		}

		if strings.HasPrefix(trimmed, "%{") || strings.HasSuffix(trimmed, "%{") {
			inBlock = true
		}

		if inBlock && strings.HasPrefix(trimmed, "#") {
			induced = append(induced, parseCIncludes([]byte(trimmed+"\n"))...)
		}

		if strings.HasSuffix(trimmed, "%}") {
			inBlock = false
		}
	})

	var set parsedIncludeSet
	set = appendParsedDirectives(set, parsedIncludesLocal, direct...)
	set = appendParsedDirectives(set, parsedIncludesHCPP, induced...)

	return set
}

func (yasmIncludeDirectiveParser) Parse(_ string, data []byte) parsedIncludeSet {
	return rawParsedIncludeSet(parsedIncludesLocal, parseYasmIncludes(data)...)
}

func (emptyIncludeDirectiveParser) Parse(_ string, _ []byte) parsedIncludeSet {
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
		// whitespace, multiplying regex cost ~3×.
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

func parseProtoImportLine(line []byte) (string, includeKind, bool) {
	trimmed := strings.TrimSpace(string(line))
	if trimmed == "" {
		return "", includeSystem, false
	}

	if idx := strings.Index(trimmed, "//"); idx >= 0 {
		trimmed = trimmed[:idx]
		trimmed = strings.TrimSpace(trimmed)
	}

	if !strings.HasPrefix(trimmed, "import") || isParserIdentContinuation(trimmed, len("import")) {
		return "", includeSystem, false
	}

	rest := strings.TrimSpace(trimmed[len("import"):])
	for _, qualifier := range [...]string{"public", "weak"} {
		if strings.HasPrefix(rest, qualifier) && !isParserIdentContinuation(rest, len(qualifier)) {
			rest = strings.TrimSpace(rest[len(qualifier):])
			break
		}
	}

	return parseDelimitedIncludeTarget(rest)
}

func parseRagelNativeIncludeLine(line string) (string, bool) {
	if idx := strings.IndexByte(line, '#'); idx >= 0 {
		line = line[:idx]
	}
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "include") || isParserIdentContinuation(line, len("include")) {
		return "", false
	}

	rest := strings.TrimSpace(line[len("include"):])
	firstQuote := strings.IndexAny(rest, `"'`)
	if firstQuote < 0 {
		return "", false
	}

	target, _, ok := parseDelimitedIncludeTarget(rest[firstQuote:])
	return target, ok
}

func parseSwigIncludeLine(line string) (string, includeKind, bool) {
	m := swigIncludeRe.FindStringSubmatch(line)
	if len(m) != 3 {
		return "", includeSystem, false
	}

	return parseDelimitedIncludeTarget(m[2])
}

func parseDelimitedIncludeTarget(s string) (string, includeKind, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", includeSystem, false
	}

	kind := includeSystem
	switch s[0] {
	case '"', '\'':
		kind = includeQuoted
	case '<':
		kind = includeSystem
	default:
		return "", includeSystem, false
	}

	close := s[0]
	if close == '<' {
		close = '>'
	}

	end := strings.IndexByte(s[1:], close)
	if end < 0 {
		return "", includeSystem, false
	}

	target := s[1 : 1+end]
	if target == "" {
		return "", includeSystem, false
	}

	if kind == includeQuoted && len(target) >= 2 && target[0] == '<' && target[len(target)-1] == '>' {
		target = target[1 : len(target)-1]
		if target == "" {
			return "", includeSystem, false
		}
		kind = includeSystem
	}

	return target, kind, true
}

func isParserIdentContinuation(s string, idx int) bool {
	if idx >= len(s) {
		return false
	}

	switch c := s[idx]; {
	case c >= 'a' && c <= 'z':
		return true
	case c >= 'A' && c <= 'Z':
		return true
	case c >= '0' && c <= '9':
		return true
	case c == '_':
		return true
	default:
		return false
	}
}

// eachLine invokes `fn` for every newline-terminated record in `data`,
// passing a sub-slice (no per-line alloc). Trailing `\r` stripped.
// The callback must not retain the slice past invocation.
func eachLine(data []byte, fn func(line []byte)) {
	start := 0

	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			line := data[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}

			fn(line)
			start = i + 1
		}
	}

	if start < len(data) {
		fn(data[start:])
	}
}

// indexOfAngleOrQuote returns the index of the first `<` or `"` in `b`,
// or -1 when neither is present.
func indexOfAngleOrQuote(b []byte) int {
	for i := 0; i < len(b); i++ {
		c := b[i]

		if c == '<' || c == '"' {
			return i
		}
	}

	return -1
}

// stripComments blanks C/C++ comment bytes (spaces, newlines preserved)
// so the include regex cannot match inside non-code spans. String and
// char literals are walked but left intact — `#include "header.h"` is
// itself a string literal at lexer level. Raw string bodies
// (`R"delim(...)delim"`) are blanked to suppress protoc codegen
// templates like `R"(#include "$path$")"`. Mutates `data` in place. No
// trigraphs, no line-continuation splicing, no `%:include`.
func stripComments(data []byte) []byte {
	hasTrigger := false

	for i := 0; i < len(data); i++ {
		c := data[i]

		if c == '/' || c == '"' || c == '\'' {
			hasTrigger = true

			break
		}
	}

	if !hasTrigger {
		return data
	}

	n := len(data)
	i := 0
	atLineStart := true

	for i < n {
		c := data[i]

		if atLineStart {
			if next, ok := scanIncludeDirectiveTarget(data, i); ok {
				i = next
				atLineStart = false
				continue
			}
		}

		if c == '/' && i+1 < n && data[i+1] == '/' {
			data[i] = ' '
			data[i+1] = ' '
			i += 2

			for i < n && data[i] != '\n' {
				data[i] = ' '
				i++
			}
			atLineStart = true

			continue
		}

		if c == '/' && i+1 < n && data[i+1] == '*' {
			data[i] = ' '
			data[i+1] = ' '
			i += 2

			for i < n {
				if i+1 < n && data[i] == '*' && data[i+1] == '/' {
					data[i] = ' '
					data[i+1] = ' '
					i += 2

					break
				}

				if data[i] != '\n' {
					data[i] = ' '
				}

				i++
			}
			atLineStart = true

			continue
		}

		if c == 'R' && i+1 < n && data[i+1] == '"' && !isIdentByte(prevByte(data, i)) {
			delimStart := i + 2
			j := delimStart

			for j < n && data[j] != '(' && data[j] != '\n' && j-delimStart < 16 {
				j++
			}

			if j >= n || data[j] != '(' {
				i++

				continue
			}

			delim := make([]byte, j-delimStart)
			copy(delim, data[delimStart:j])

			i = j + 1

			for i < n {
				if data[i] == ')' && i+1+len(delim)+1 <= n {
					match := true

					for k, b := range delim {
						if data[i+1+k] != b {
							match = false

							break
						}
					}

					if match && data[i+1+len(delim)] == '"' {
						for k := 0; k <= len(delim); k++ {
							data[i+k] = ' '
						}
						data[i+1+len(delim)] = ' '
						i += 1 + len(delim) + 1

						break
					}
				}

				if data[i] != '\n' {
					data[i] = ' '
				}
				i++
			}

			continue
		}

		if c == '"' {
			i++

			for i < n {
				if data[i] == '\\' && i+1 < n && data[i+1] != '\n' {
					i += 2

					continue
				}

				if data[i] == '"' {
					i++

					break
				}

				if data[i] == '\n' {
					break
				}

				i++
			}

			continue
		}

		if c == '\'' {
			i++

			for i < n {
				if data[i] == '\\' && i+1 < n && data[i+1] != '\n' {
					i += 2

					continue
				}

				if data[i] == '\'' {
					i++

					break
				}

				if data[i] == '\n' {
					break
				}

				i++
			}

			continue
		}

		i++
		atLineStart = c == '\n'
	}

	return data
}

func scanIncludeDirectiveTarget(data []byte, i int) (int, bool) {
	n := len(data)
	j := i
	for j < n {
		switch data[j] {
		case ' ', '\t':
			j++
		default:
			goto nonSpace
		}
	}
	return 0, false

nonSpace:
	if data[j] != '#' {
		return 0, false
	}
	j++
	for j < n {
		switch data[j] {
		case ' ', '\t':
			j++
		default:
			goto directive
		}
	}
	return 0, false

directive:
	switch {
	case bytes.HasPrefix(data[j:], []byte("include_next")):
		j += len("include_next")
	case bytes.HasPrefix(data[j:], []byte("include")):
		j += len("include")
	default:
		return 0, false
	}
	for j < n {
		switch data[j] {
		case ' ', '\t':
			j++
		default:
			goto target
		}
	}
	return 0, false

target:
	if j >= n {
		return 0, false
	}
	var close byte
	switch data[j] {
	case '<':
		close = '>'
	case '"':
		close = '"'
	default:
		return 0, false
	}
	j++
	for j < n {
		if data[j] == '\\' && close == '"' && j+1 < n && data[j+1] != '\n' {
			j += 2
			continue
		}
		if data[j] == close {
			return j + 1, true
		}
		if data[j] == '\n' {
			return 0, false
		}
		j++
	}
	return 0, false
}

func prevByte(data []byte, i int) byte {
	if i == 0 {
		return 0
	}

	return data[i-1]
}

func isIdentByte(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '_'
}
