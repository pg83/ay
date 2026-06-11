package main

import (
	"bytes"
	"regexp"
	"strings"
)

var (
	yasmIncludeRe           = regexp.MustCompile(`(?i)^\s*%\s*include\s*[<"]([^>"]+)[>"]`)
	swigIncludeRe           = regexp.MustCompile(`^%(include|import|insert\s*\([^\)]*\))\s*([<"].*?[">])`)
	cythonCimportFromRe     = regexp.MustCompile(`^\s*from\s+([A-Za-z0-9_\.]+)\s+cimport\b`)
	cythonCimportRe         = regexp.MustCompile(`^\s*cimport\s+(.+)$`)
	cythonIncludeRe         = regexp.MustCompile(`^\s*include\s+["']([^"']+)["']`)
	cythonExternFromRe      = regexp.MustCompile(`^\s*cdef\s+extern\s+from\s+(<[^>]+>|"[^"]+"|'[^']+')`)
	flatbuffersIncludeRe    = regexp.MustCompile(`^\s*include\s+"([^"]+)"\s*;`)
	includeDirectiveParsers = newIncludeDirectiveParserRegistry()
	blockCommentOpen        = []byte("/*")
	blockCommentClose       = []byte("*/")
	backtraceHeaderInclude  = []byte("BACKTRACE_HEADER")
	opensslUnistdInclude    = []byte("OPENSSL_UNISTD")
)

// swigImplicitDirectives are swig's implicit %includes (swig/Source/Modules/
// main.cxx; the SWIG_IMPLICIT_INCLUDES conf var), prepended by the parser to
// every root .swg file outside the swig library — upstream's
// TSwigIncludeProcessor::AddImplicitIncludes, as a parse property.
var swigImplicitDirectives = func() []IncludeDirective {
	names := []string{"swig.swg", "go.swg", "java.swg", "perl5.swg", "python.swg"}
	out := make([]IncludeDirective, 0, len(names))

	for _, n := range names {
		out = append(out, IncludeDirective{kind: includeSystem, target: internStr(n)})
	}

	return out
}()

type IncludeDirectiveParser interface {
	parse(rel string, data []byte) ParsedIncludeSet
	// id is the parser's small stable identity — the second component of the
	// ambiguous-ext parse-cache key (a file with an unregistered extension
	// parses under the scan context's parser, so one file may carry one parse
	// result per parser).
	id() uint32
}

type IncludeDirectiveParserRegistry struct {
	defaultParser IncludeDirectiveParser
}

type CIncludeDirectiveParser struct{}
type CythonIncludeDirectiveParser struct{}
type FlatbuffersIncludeDirectiveParser struct{}
type ProtoIncludeDirectiveParser struct{}
type RagelIncludeDirectiveParser struct{}
type SwigIncludeDirectiveParser struct{}
type YasmIncludeDirectiveParser struct{}
type EmptyIncludeDirectiveParser struct{}

func (CIncludeDirectiveParser) id() uint32 {
	return 1
}

func (CythonIncludeDirectiveParser) id() uint32 {
	return 2
}

func (FlatbuffersIncludeDirectiveParser) id() uint32 {
	return 3
}

func (ProtoIncludeDirectiveParser) id() uint32 {
	return 4
}

func (RagelIncludeDirectiveParser) id() uint32 {
	return 5
}

func (SwigIncludeDirectiveParser) id() uint32 {
	return 6
}

func (YasmIncludeDirectiveParser) id() uint32 {
	return 7
}

func (EmptyIncludeDirectiveParser) id() uint32 {
	return 8
}

// walkableBucketFor selects the directive bucket the scanner closes over for
// a file — a pure function of the file's TYPE, so the context-free scanCache
// stays valid. Ragel files walk their native %include edges (upstream
// TRagelIncludeProcessor: native → direct node dep), every other type walks
// the local bucket; a ragel file's C/C++ directives ride as induced h+cpp on
// its generated output instead.
func walkableBucketFor(rel string) ParsedIncludeBucket {
	if _, ok := includeDirectiveParsers.registeredParserFor(rel).(RagelIncludeDirectiveParser); ok {
		return parsedIncludesRagelNative
	}

	return parsedIncludesLocal
}

func newIncludeDirectiveParserRegistry() IncludeDirectiveParserRegistry {
	return IncludeDirectiveParserRegistry{defaultParser: CIncludeDirectiveParser{}}
}

// registeredParserFor returns the explicitly registered parser for rel's
// extension, or nil — an unregistered extension (swig's .i, …) parses under
// the SCAN CONTEXT's parser, resolved once from the walk's root file.
func (r *IncludeDirectiveParserRegistry) registeredParserFor(rel string) IncludeDirectiveParser {
	return lookupParserByExt(directiveParserExt(rel))
}

func (r *IncludeDirectiveParserRegistry) parserFor(rel string) IncludeDirectiveParser {
	if p := lookupParserByExt(directiveParserExt(rel)); p != nil {
		return p
	}

	return r.defaultParser
}

// hasRegisteredParser reports whether the parser dispatcher would return an
// explicitly-registered parser (as opposed to falling back to the default
// C-like parser) for a given file path. Used by callers that need a strict
// gate on "is this file in the include graph?" — e.g. RUN_PROGRAM IN
// walking, where unknown-extension data files (libmagic Magdir/cafebabe,
// Jinja .jnj templates, JSON, etc.) must not be parsed for C-style
// directives. The .in trail-strip in directiveParserExt makes .h.in and
// .cpp.in dispatch correctly.
func (r *IncludeDirectiveParserRegistry) hasRegisteredParser(rel string) bool {
	return lookupParserByExt(directiveParserExt(rel)) != nil
}

func directiveParserExt(rel string) string {
	if strings.HasSuffix(rel, ".in") {
		rel = strings.TrimSuffix(rel, ".in")
	}

	idx := strings.LastIndexByte(rel, '.')

	if idx < 0 {
		return ""
	}

	return rel[idx:]
}

func (CIncludeDirectiveParser) parse(rel string, data []byte) ParsedIncludeSet {
	out := parseCIncludes(data)

	out = dedupDirectives(out)

	if len(out) == 0 {
		return ParsedIncludeSet{}
	}

	return ParsedIncludeSet{parsedIncludesLocal: out}
}

// directiveID packs one directive's (kind, target) into a VFS — the same
// STR<<1|bit shape — so identical directives dedup through the global VFS deduper
// by a plain cast, no separate set.
func directiveID(d IncludeDirective) VFS {
	return VFS(uint32(d.target)<<1 | uint32(d.kind))
}

// dedupDirectives drops repeated (kind, target) directives in place over the
// global VFS deduper — boost's PP iteration files (forwardN_1024.hpp) repeat one
// #include up to 1024x, and each survivor otherwise drives a redundant resolve.
// In-place compaction, no array copy, no per-file map. Single-threaded gen only.
func dedupDirectives(out []IncludeDirective) []IncludeDirective {
	if len(out) < 2 {
		return out
	}

	deduper.reset()
	kept := out[:0]

	for _, d := range out {
		if deduper.add(directiveID(d)) {
			kept = append(kept, d)
		}
	}

	return kept
}

func (CythonIncludeDirectiveParser) parse(rel string, data []byte) ParsedIncludeSet {
	out := make([]IncludeDirective, 0, 8)
	add := func(d IncludeDirective) {
		out = append(out, d)
	}

	eachLine(data, func(line []byte) {
		s := strings.TrimSpace(string(line))

		if s == "" || strings.HasPrefix(s, "#") {
			return
		}

		if m := cythonIncludeRe.FindStringSubmatch(s); len(m) == 2 {
			add(IncludeDirective{kind: includeQuoted, target: internStr(m[1])})

			return
		}

		if m := cythonExternFromRe.FindStringSubmatch(s); len(m) == 2 {
			target, kind, ok := parseDelimitedIncludeTarget(m[1])

			if ok {
				add(IncludeDirective{kind: kind, target: internStr(target)})
			}

			return
		}

		if m := cythonCimportFromRe.FindStringSubmatch(s); len(m) == 2 {
			if t := cythonPxdTarget(m[1]); t != "" {
				add(IncludeDirective{kind: includeQuoted, target: internStr(t)})
			}

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

				if t := cythonPxdTarget(part); t != "" {
					add(IncludeDirective{kind: includeQuoted, target: internStr(t)})
				}
			}
		}
	})

	out = dedupDirectives(out)

	if len(out) == 0 {
		return ParsedIncludeSet{}
	}

	return ParsedIncludeSet{parsedIncludesLocal: out}
}

func cythonPxdTarget(module string) string {
	if module == "cython" || strings.HasPrefix(module, "cython.") {
		return ""
	}

	switch module {
	case "cpython", "libc", "libcpp":
		return module + "/__init__.pxd"
	default:
		return strings.ReplaceAll(module, ".", "/") + ".pxd"
	}
}

func (FlatbuffersIncludeDirectiveParser) parse(_ string, data []byte) ParsedIncludeSet {
	data = stripComments(data)

	out := make([]IncludeDirective, 0, 4)

	eachLine(data, func(line []byte) {
		m := flatbuffersIncludeRe.FindSubmatch(line)

		if len(m) != 2 {
			return
		}

		out = append(out, IncludeDirective{kind: includeQuoted, target: internStr(string(m[1]))})
	})

	if len(out) == 0 {
		return ParsedIncludeSet{}
	}

	return ParsedIncludeSet{parsedIncludesLocal: out}
}

func (ProtoIncludeDirectiveParser) parse(_ string, data []byte) ParsedIncludeSet {
	local := make([]IncludeDirective, 0, 8)
	hcpp := make([]IncludeDirective, 0, 8)

	eachLine(data, func(line []byte) {
		target, kind, ok := parseProtoImportLine(line)

		if !ok {
			return
		}

		local = append(local, IncludeDirective{kind: kind, target: internStr(target)})

		switch {
		case strings.HasSuffix(target, ".ev"):
			hcpp = append(hcpp, IncludeDirective{kind: kind, target: internStr(strings.TrimSuffix(target, ".ev") + ".ev.pb.h")})
		case strings.HasSuffix(target, ".proto"):
			hcpp = append(hcpp, IncludeDirective{kind: kind, target: internStr(strings.TrimSuffix(target, ".proto") + ".pb.h")})
		}
	})

	var set ParsedIncludeSet

	if len(local) > 0 {
		set[parsedIncludesLocal] = local
	}

	// h+cpp applies to both consumer kinds; the two buckets share one buffer.
	set[parsedIncludesHeader] = hcpp
	set[parsedIncludesCpp] = hcpp

	return set
}

func (RagelIncludeDirectiveParser) parse(rel string, data []byte) ParsedIncludeSet {
	local := parseCIncludes(data)

	var native []IncludeDirective
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
		native = append(native, IncludeDirective{kind: includeQuoted, target: internStr(target)})
	})

	var set ParsedIncludeSet
	set = appendParsedDirectives(set, parsedIncludesLocal, local...)
	set = appendParsedDirectives(set, parsedIncludesRagelNative, native...)
	// Mirror upstream TRagelIncludeProcessor: a ragel file's WALKABLE edges are
	// the native %include directives; the C/C++ directives ride as the induced
	// h+cpp set applied to the generated output (AddParsedIncls("h+cpp")). The
	// self-include leads so the .rl6 itself stays an input of the consuming
	// compile.
	induced := make([]IncludeDirective, 0, 1+len(local))
	induced = append(induced, IncludeDirective{kind: includeQuoted, target: internStr(rel)})
	induced = append(induced, local...)
	set[parsedIncludesHeader] = induced
	set[parsedIncludesCpp] = induced

	return set
}

func (SwigIncludeDirectiveParser) parse(rel string, data []byte) ParsedIncludeSet {
	direct := make([]IncludeDirective, 0, 8)
	induced := make([]IncludeDirective, 0, 4)
	inBlock := false

	if !strings.Contains(rel, "/swig/Lib/") {
		direct = append(direct, swigImplicitDirectives...)
	}

	eachLine(data, func(line []byte) {
		trimmed := strings.TrimSpace(string(line))

		if trimmed == "" {
			return
		}

		if !inBlock && (strings.HasPrefix(trimmed, "%include") || strings.HasPrefix(trimmed, "%import") || strings.HasPrefix(trimmed, "%insert")) {
			target, kind, ok := parseSwigIncludeLine(trimmed)

			if ok {
				direct = append(direct, IncludeDirective{kind: kind, target: internStr(target)})
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

	var set ParsedIncludeSet
	set = appendParsedDirectives(set, parsedIncludesLocal, direct...)
	set[parsedIncludesHeader] = induced
	set[parsedIncludesCpp] = induced

	return set
}

func (YasmIncludeDirectiveParser) parse(_ string, data []byte) ParsedIncludeSet {
	out := parseYasmIncludes(data)

	if len(out) == 0 {
		return ParsedIncludeSet{}
	}

	return ParsedIncludeSet{parsedIncludesLocal: out}
}

func (EmptyIncludeDirectiveParser) parse(_ string, _ []byte) ParsedIncludeSet {
	return ParsedIncludeSet{}
}

func parseCIncludes(data []byte) []IncludeDirective {
	out := make([]IncludeDirective, 0, 8)
	n := len(data)
	p := 0
	clean := 0

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
			p = nextLineStart(data, q0)

			continue
		}

		if end, covered := leadingBlockCoversHi(data, clean, hi); covered {
			p, clean = end, end

			continue
		}

		d, ok, next := parseDirectiveInline(data, hi)

		if ok {
			out = append(out, d)
		}

		p, clean = next, next
	}

	return out
}

func parseDirectiveInline(data []byte, hashPos int) (IncludeDirective, bool, int) {
	n := len(data)
	q := skipWSAndBlockComments(data, hashPos+1)

	switch {
	case bytesHasPrefixAt(data, q, "include") && !identByteAt(data, q+len("include")):
		q += len("include")
	case bytesHasPrefixAt(data, q, "import") && !identByteAt(data, q+len("import")):
		q += len("import")
	default:
		return IncludeDirective{}, false, nextLineStart(data, q)
	}

	q = skipWSAndBlockComments(data, q)

	if q >= n {
		return IncludeDirective{}, false, n
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

		if q > start && data[start] != '$' && !hasYIgnoreComment(data, q) && !bytes.ContainsAny(data[start:q], "[]") {
			// `#include BACKTRACE_HEADER` (llvm Signals.inc) is a macro-form
			// (computed) include, not a path — it never resolves and upstream does
			// not list it as an input. Drop it here so the scanner doesn't fail-fast
			// on the unresolved bareword. Cheap first-byte gate before the full
			// compare keeps the hot path (every bareword include) to one byte check.
			if data[start] == 'B' && bytes.Equal(data[start:q], backtraceHeaderInclude) {
				return IncludeDirective{}, false, nextLineStart(data, q)
			}

			// `#include OPENSSL_UNISTD` (openssl) is a macro that expands to
			// <unistd.h>; resolve it to that here so the unistd.h input is picked
			// up without a per-file fixup. Same cheap first-byte gate.
			if data[start] == 'O' && bytes.Equal(data[start:q], opensslUnistdInclude) {
				return IncludeDirective{kind: includeSystem, target: opensslUnistdTarget}, true, nextLineStart(data, q)
			}

			return IncludeDirective{kind: includeQuoted, target: internBytes(data[start:q])}, true, nextLineStart(data, q)
		}

		return IncludeDirective{}, false, nextLineStart(data, q)
	}

	q++

	start := q

	for q < n && data[q] != closeCh && data[q] != '\n' && data[q] != '$' {
		q++
	}

	if q >= n || data[q] != closeCh {
		return IncludeDirective{}, false, nextLineStart(data, q)
	}

	targetBytes := data[start:q]
	q++

	kind := includeSystem

	if closeCh == '"' {
		kind = includeQuoted
	}

	if !hasYIgnoreComment(data, q) && !bytes.ContainsAny(targetBytes, "[]") {
		return IncludeDirective{kind: kind, target: internBytes(targetBytes)}, true, nextLineStart(data, q)
	}

	return IncludeDirective{}, false, nextLineStart(data, q)
}

func isCWSByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\v' || c == '\f' || c == '\r'
}

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
				return end, true
			}

			i = end
		} else {
			i = open + 2
		}
	}

	return 0, false
}

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

func parseYasmIncludes(data []byte) []IncludeDirective {
	out := make([]IncludeDirective, 0, 4)

	eachLine(data, func(line []byte) {
		if bytes.IndexByte(line, '%') < 0 {
			return
		}

		m := yasmIncludeRe.FindSubmatchIndex(line)

		if m == nil {
			return
		}

		kind := includeSystem

		idx := indexOfAngleOrQuote(line)

		if idx >= 0 && line[idx] == '"' {
			kind = includeQuoted
		}

		target := string(line[m[2]:m[3]])
		out = append(out, IncludeDirective{kind: kind, target: internStr(target)})
	})

	return out
}

func parseProtoImportLine(line []byte) (string, IncludeKind, bool) {
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

func parseSwigIncludeLine(line string) (string, IncludeKind, bool) {
	m := swigIncludeRe.FindStringSubmatch(line)

	if len(m) != 3 {
		return "", includeSystem, false
	}

	return parseDelimitedIncludeTarget(m[2])
}

func parseDelimitedIncludeTarget(s string) (string, IncludeKind, bool) {
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

func indexOfAngleOrQuote(b []byte) int {
	for i := 0; i < len(b); i++ {
		c := b[i]

		if c == '<' || c == '"' {
			return i
		}
	}

	return -1
}

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

func identByteAt(data []byte, i int) bool {
	return i < len(data) && isIdentByte(data[i])
}
