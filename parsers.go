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
var flatbuffersIncludeRe = regexp.MustCompile(`^\s*include\s+"([^"]+)"\s*;`)

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
type flatbuffersIncludeDirectiveParser struct{}
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
	r.register(flatbuffersIncludeDirectiveParser{}, ".fbs")
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
			out = append(out, includeDirective{kind: m.kind, target: internString(m.target)})
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
		add(includeDirective{kind: includeQuoted, target: internString(path.Base(base) + ".pxd")})
	}

	eachLine(data, func(line []byte) {
		s := strings.TrimSpace(string(line))
		if s == "" || strings.HasPrefix(s, "#") {
			return
		}

		if m := cythonIncludeRe.FindStringSubmatch(s); len(m) == 2 {
			add(includeDirective{kind: includeQuoted, target: internString(m[1])})
			return
		}

		if m := cythonExternFromRe.FindStringSubmatch(s); len(m) == 2 {
			target, kind, ok := parseDelimitedIncludeTarget(m[1])
			if ok {
				add(includeDirective{kind: kind, target: internString(target)})
			}
			return
		}

		if m := cythonCimportFromRe.FindStringSubmatch(s); len(m) == 2 {
			add(includeDirective{kind: includeQuoted, target: internString(cythonPxdTarget(m[1]))})
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
				add(includeDirective{kind: includeQuoted, target: internString(cythonPxdTarget(part))})
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

func (flatbuffersIncludeDirectiveParser) Parse(_ string, data []byte) parsedIncludeSet {
	data = stripComments(data)

	out := make([]includeDirective, 0, 4)

	eachLine(data, func(line []byte) {
		m := flatbuffersIncludeRe.FindSubmatch(line)
		if len(m) != 2 {
			return
		}

		out = append(out, includeDirective{kind: includeQuoted, target: internString(string(m[1]))})
	})

	return rawParsedIncludeSet(parsedIncludesLocal, out...)
}

func (protoIncludeDirectiveParser) Parse(_ string, data []byte) parsedIncludeSet {
	local := make([]includeDirective, 0, 8)
	hcpp := make([]includeDirective, 0, 8)

	eachLine(data, func(line []byte) {
		target, kind, ok := parseProtoImportLine(line)
		if !ok {
			return
		}

		local = append(local, includeDirective{kind: kind, target: internString(target)})

		// HCPP bucket carries the codegen schema: protoc-generated
		// outputs `#include` the .pb.h / .ev.pb.h of each import.
		// Paths are import-relative — the walker applies the codegen
		// output-root prefix and the runtime-base prefix for the
		// descriptor.proto special case.
		switch {
		case strings.HasSuffix(target, ".ev"):
			hcpp = append(hcpp, includeDirective{kind: kind, target: internString(strings.TrimSuffix(target, ".ev") + ".ev.pb.h")})
		case strings.HasSuffix(target, ".proto"):
			hcpp = append(hcpp, includeDirective{kind: kind, target: internString(strings.TrimSuffix(target, ".proto") + ".pb.h")})
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
		local = append(local, includeDirective{kind: includeQuoted, target: internString(target)})
	})

	var set parsedIncludeSet
	set = appendParsedDirectives(set, parsedIncludesLocal, local...)
	set = appendParsedDirectives(set, parsedIncludesHCPP, includeDirective{
		kind:   includeQuoted,
		target: internString(rel),
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
				direct = append(direct, includeDirective{kind: kind, target: internString(target)})
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

// parseCIncludes extracts C/C++ `#include` / `#import` directives from `data`,
// byte-for-byte matching upstream ymake's line-oriented ScanCppIncludes across
// the whole ydb tree. It jumps to each '#' with a single IndexByte (most bytes
// are not directives), then accepts it only when it is the first non-ws,
// non-block-comment byte of its line (skipWSAndBlockComments(ls) == hi) AND is
// not inside a multiline line-leading `/* */` block comment that opened earlier
// (leadingBlockCoversHi). The target is read literally between the brackets, so
// it excludes `\n` and `$` (macro-expanded `<$X>` and protoc templates like
// `R"(#include "$path$")"` are dropped) but keeps embedded `//`; a trailing
// `// Y_IGNORE` suppresses the directive. `#include_next`/bare-macro
// `#include FOO` fall through as a macro-form token, as upstream.
//
// No comment-strip buffer and no regex: the line-leading rule sidesteps '#'
// inside strings/code, and the literal target read avoids mis-stripping '//'
// in include paths — both of which a comment-stripping scan gets wrong.
func parseCIncludes(data []byte) []includeDirective {
	out := make([]includeDirective, 0, 8)
	n := len(data)
	p := 0
	clean := 0 // last position confirmed to be OUTSIDE any block comment

	for p < n {
		rel := bytes.IndexByte(data[p:], '#')
		if rel < 0 {
			break
		}
		hi := p + rel

		ls := hi
		for ls > 0 && data[ls-1] != '\n' {
			ls--
		}

		if q0 := skipWSAndBlockComments(data, ls); q0 != hi {
			// '#' is not the line's first real byte (it is in code/a string, or a
			// leading block comment consumed past it). Resume past whatever the
			// skip consumed — never inside it. Do NOT advance `clean`: ls may sit
			// inside a multiline comment whose '/*' we have not yet seen.
			p = nextLineStart(data, q0)

			continue
		}

		// '#' is its line's first real byte. But a line-leading `/*` may have
		// opened on an EARLIER line and still span hi (libcxx synopsis,
		// simdjson's doc block). Scan [clean, hi) — clean is comment-clean and
		// advances monotonically, so these gaps are disjoint (O(data) overall).
		if end, covered := leadingBlockCoversHi(data, clean, hi); covered {
			p, clean = end, end

			continue
		}

		d, ok, next := parseDirectiveInline(data, hi)
		if ok {
			out = append(out, d)
		}
		p, clean = next, next // hi was outside any comment, so `next` is clean
	}

	return out
}

// parseDirectiveInline parses the `#include`/`#import` directive at hashPos
// (data[hashPos] == '#'), returning the directive (when matched), whether it
// matched, and the resume position (start of the next line). Mirrors
// parseCIncludes's per-directive grammar; interns the target from the slice.
func parseDirectiveInline(data []byte, hashPos int) (includeDirective, bool, int) {
	n := len(data)
	q := skipWSAndBlockComments(data, hashPos+1)

	switch {
	case bytesHasPrefixAt(data, q, "include"):
		q += len("include")
	case bytesHasPrefixAt(data, q, "import"):
		q += len("import")
	default:
		return includeDirective{}, false, nextLineStart(data, q)
	}

	q = skipWSAndBlockComments(data, q)
	if q >= n {
		return includeDirective{}, false, n
	}

	var closeCh byte
	switch data[q] {
	case '<':
		closeCh = '>'
	case '"':
		closeCh = '"'
	default:
		start := q
		for q < n {
			if c := data[q]; c == ' ' || c == '\t' || c == '\v' || c == '\f' || c == '\r' || c == '\n' {
				break
			}
			q++
		}
		if q > start && data[start] != '$' && !hasYIgnoreComment(data, q) {
			return includeDirective{kind: includeQuoted, target: internBytes(data[start:q])}, true, nextLineStart(data, q)
		}

		return includeDirective{}, false, nextLineStart(data, q)
	}
	q++

	start := q
	for q < n && data[q] != closeCh && data[q] != '\n' && data[q] != '$' {
		q++
	}
	if q >= n || data[q] != closeCh {
		return includeDirective{}, false, nextLineStart(data, q)
	}
	targetBytes := data[start:q]
	q++

	kind := includeSystem
	if closeCh == '"' {
		kind = includeQuoted
	}

	if !hasYIgnoreComment(data, q) {
		return includeDirective{kind: kind, target: internBytes(targetBytes)}, true, nextLineStart(data, q)
	}

	return includeDirective{}, false, nextLineStart(data, q)
}

// isCWSByte reports the C "whitespace" bytes the directive grammar skips
// (matching skipWSAndBlockComments; newline is a line boundary, not ws).
func isCWSByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\v' || c == '\f' || c == '\r'
}

// leadingBlockCoversHi reports whether hi falls inside a `/* ... */` block
// comment whose `/*` is the first non-ws byte of its line (the only comment
// shape the old parser consumes across lines) and which opens within [from,
// hi). Returns the comment's end so the caller resumes past it. A mid-line
// `/*` is ignored — the old parser does not track those, so neither do we.
func leadingBlockCoversHi(data []byte, from, hi int) (int, bool) {
	for i := from; i < hi; {
		rel := bytes.Index(data[i:hi], blockCommentOpen)
		if rel < 0 {
			return 0, false
		}
		open := i + rel

		lineLeading := true
		for k := open; k > 0 && data[k-1] != '\n'; k-- {
			if !isCWSByte(data[k-1]) {
				lineLeading = false

				break
			}
		}

		end := len(data)
		if cl := bytes.Index(data[open+2:], blockCommentClose); cl >= 0 {
			end = open + 2 + cl + 2
		}

		if lineLeading {
			if end > hi {
				return end, true // the comment spans hi
			}
			i = end // line-leading comment that closed before hi; resume past it
		} else {
			i = open + 2 // mid-line '/*': not consumed by the old parser
		}
	}

	return 0, false
}

var (
	blockCommentOpen  = []byte("/*")
	blockCommentClose = []byte("*/")
)

// skipWSAndBlockComments advances past spaces/tabs/\v/\f/\r and `/* */`
// block comments (which may span newlines), mirroring the Ragel `ws` rule.
// Newlines outside a block comment are NOT whitespace and stop the scan.
func skipWSAndBlockComments(data []byte, i int) int {
	n := len(data)
	for i < n {
		switch data[i] {
		case ' ', '\t', '\v', '\f', '\r':
			i++
		case '/':
			if i+1 < n && data[i+1] == '*' {
				i += 2
				for i+1 < n && !(data[i] == '*' && data[i+1] == '/') {
					i++
				}
				if i+1 < n {
					i += 2
				} else {
					i = n
				}
				continue
			}
			return i
		default:
			return i
		}
	}
	return i
}

// nextLineStart returns the index just past the next '\n' at or after i, or
// len(data) if none — the include scanner's "skip the rest of this line".
func nextLineStart(data []byte, i int) int {
	if i >= len(data) {
		return len(data)
	}
	nl := bytes.IndexByte(data[i:], '\n')
	if nl < 0 {
		return len(data)
	}
	return i + nl + 1
}

// bytesHasPrefixAt reports whether data[i:] begins with s.
func bytesHasPrefixAt(data []byte, i int, s string) bool {
	if i+len(s) > len(data) {
		return false
	}
	for k := 0; k < len(s); k++ {
		if data[i+k] != s[k] {
			return false
		}
	}
	return true
}

// hasYIgnoreComment reports whether the text at i (after optional ws/block
// comments) is `// Y_IGNORE` — upstream's marker to drop the just-parsed
// include (Ragel `ws "//" ' '* "Y_IGNORE"`).
func hasYIgnoreComment(data []byte, i int) bool {
	n := len(data)
	i = skipWSAndBlockComments(data, i)
	if i+2 > n || data[i] != '/' || data[i+1] != '/' {
		return false
	}
	i += 2
	for i < n && data[i] == ' ' {
		i++
	}
	return bytesHasPrefixAt(data, i, "Y_IGNORE")
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

		out = append(out, includeDirective{kind: kind, next: false, target: internString(target)})
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
	if strings.HasPrefix(rest, "public") && !isParserIdentContinuation(rest, len("public")) {
		rest = strings.TrimSpace(rest[len("public"):])
	} else if strings.HasPrefix(rest, "weak") && !isParserIdentContinuation(rest, len("weak")) {
		rest = strings.TrimSpace(rest[len("weak"):])
	}

	target, kind, ok := parseDelimitedIncludeTarget(rest)
	return target, kind, ok
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
