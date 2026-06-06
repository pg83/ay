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
)

var macroIndirectIncludes = map[string][]macroIndirectInclude{
	"contrib/libs/openssl/crypto/rand/rand_egd.c": {{target: "unistd.h", kind: includeSystem}},
	"contrib/libs/openssl/crypto/uid.c":           {{target: "unistd.h", kind: includeSystem}},
	"contrib/libs/pugixml/pugixml.hpp":            {{target: "pugixml.cpp", kind: includeQuoted}},
}

// macroIncludeDrops suppresses specific parsed include targets per-file, for
// directives the C-style parser unavoidably emits but which never resolve and
// upstream does NOT add as inputs either. These are pure -k noise:
//   - BACKTRACE_HEADER in llvm Signals.inc is a macro expansion
//     (build/sysincl/macro.yml maps it to $U/execinfo.h, but the $U placeholder
//     is not substituted in our resolver; the bareword include cannot resolve).
//   - <types.h> in Poco SocketDefs.h sits inside the VxWorks/VMS branch — a
//     platform we do not target and that has no header to find.
//
// In both cases the byte-exact closure already matches ref without these
// directives; dropping them keeps the warning gate clean.
var macroIncludeDrops = map[string][]string{
	"contrib/libs/llvm16/lib/Support/Unix/Signals.inc":    {"BACKTRACE_HEADER"},
	"contrib/libs/poco/Net/include/Poco/Net/SocketDefs.h": {"types.h"},
}

type macroIndirectInclude struct {
	target string
	kind   includeKind
}

func filterDroppedDirectives(out []includeDirective, drops []string) []includeDirective {
	if len(out) == 0 || len(drops) == 0 {
		return out
	}

	filtered := out[:0]

	for _, d := range out {
		t := d.target.String()
		drop := false

		for _, name := range drops {
			if t == name {
				drop = true
				break
			}
		}

		if !drop {
			filtered = append(filtered, d)
		}
	}

	return filtered
}

type includeDirectiveParser interface {
	Parse(rel string, data []byte) parsedIncludeSet
}

type includeDirectiveParserRegistry struct {
	defaultParser includeDirectiveParser
	byExt         map[string]includeDirectiveParser

	// lastExt/lastParser memoize the previous parserFor resolution. Parsed files
	// arrive in scan order, where one extension tends to run consecutively (a
	// .cpp's includes are mostly .h), so this one-entry cache skips most byExt
	// probes. lastValid distinguishes "not cached" from a cached empty extension.
	// Single-goroutine gen, so no locking.
	lastExt    string
	lastParser includeDirectiveParser
	lastValid  bool
}
type cIncludeDirectiveParser struct{}
type cythonIncludeDirectiveParser struct{}
type flatbuffersIncludeDirectiveParser struct{}
type protoIncludeDirectiveParser struct{}
type ragelIncludeDirectiveParser struct{}
type swigIncludeDirectiveParser struct{}
type yasmIncludeDirectiveParser struct{}
type emptyIncludeDirectiveParser struct{}

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
	r.register(empty, ".g4", ".stg", ".m4")

	return r
}

func (r includeDirectiveParserRegistry) register(parser includeDirectiveParser, exts ...string) {
	for _, ext := range exts {
		r.byExt[ext] = parser
	}
}

func (r *includeDirectiveParserRegistry) parserFor(rel string) includeDirectiveParser {
	ext := directiveParserExt(rel)

	if r.lastValid && ext == r.lastExt {
		return r.lastParser
	}

	parser := r.defaultParser

	if p, ok := r.byExt[ext]; ok {
		parser = p
	}

	r.lastExt = ext
	r.lastParser = parser
	r.lastValid = true

	return parser
}

// hasRegisteredParser reports whether the parser dispatcher would return an
// explicitly-registered parser (as opposed to falling back to the default
// C-like parser) for a given file path. Used by callers that need a strict
// gate on "is this file in the include graph?" — e.g. RUN_PROGRAM IN
// walking, where unknown-extension data files (libmagic Magdir/cafebabe,
// Jinja .jnj templates, JSON, etc.) must not be parsed for C-style
// directives. The .in trail-strip in directiveParserExt makes .h.in and
// .cpp.in dispatch correctly.
func (r includeDirectiveParserRegistry) hasRegisteredParser(rel string) bool {
	_, ok := r.byExt[directiveParserExt(rel)]
	return ok
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

func (cIncludeDirectiveParser) Parse(rel string, data []byte) parsedIncludeSet {
	out := parseCIncludes(data)

	if drops, ok := macroIncludeDrops[rel]; ok {
		out = filterDroppedDirectives(out, drops)
	}

	if extras, ok := macroIndirectIncludes[rel]; ok {
		for _, m := range extras {
			out = append(out, includeDirective{kind: m.kind, target: internString(m.target)})
		}
	}

	out = dedupDirectives(out)

	if len(out) == 0 {
		return parsedIncludeSet{}
	}

	return parsedIncludeSet{parsedIncludesLocal: out}
}

// directiveID packs one directive's (kind, target) into a VFS — the same
// STR<<1|bit shape — so identical directives dedup through the global VFS deduper
// by a plain cast, no separate set.
func directiveID(d includeDirective) VFS {
	return VFS(uint32(d.target)<<1 | uint32(d.kind))
}

// dedupDirectives drops repeated (kind, target) directives in place over the
// global VFS deduper — boost's PP iteration files (forwardN_1024.hpp) repeat one
// #include up to 1024x, and each survivor otherwise drives a redundant resolve.
// In-place compaction, no array copy, no per-file map. Single-threaded gen only.
func dedupDirectives(out []includeDirective) []includeDirective {
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

func (cythonIncludeDirectiveParser) Parse(rel string, data []byte) parsedIncludeSet {
	out := make([]includeDirective, 0, 8)
	add := func(d includeDirective) {
		out = append(out, d)
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
			if t := cythonPxdTarget(m[1]); t != "" {
				add(includeDirective{kind: includeQuoted, target: internString(t)})
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
					add(includeDirective{kind: includeQuoted, target: internString(t)})
				}
			}
		}
	})

	out = dedupDirectives(out)

	if len(out) == 0 {
		return parsedIncludeSet{}
	}

	return parsedIncludeSet{parsedIncludesLocal: out}
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

	if len(out) == 0 {
		return parsedIncludeSet{}
	}

	return parsedIncludeSet{parsedIncludesLocal: out}
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

		switch {
		case strings.HasSuffix(target, ".ev"):
			hcpp = append(hcpp, includeDirective{kind: kind, target: internString(strings.TrimSuffix(target, ".ev") + ".ev.pb.h")})
		case strings.HasSuffix(target, ".proto"):
			hcpp = append(hcpp, includeDirective{kind: kind, target: internString(strings.TrimSuffix(target, ".proto") + ".pb.h")})
		}
	})

	var set parsedIncludeSet

	if len(local) > 0 {
		set = parsedIncludeSet{parsedIncludesLocal: local}
	}

	set = appendParsedDirectives(set, parsedIncludesHCPP, hcpp...)
	return set
}

func (ragelIncludeDirectiveParser) Parse(rel string, data []byte) parsedIncludeSet {
	local := parseCIncludes(data)

	var native []includeDirective
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
		native = append(native, includeDirective{kind: includeQuoted, target: internString(target)})
	})

	var set parsedIncludeSet
	set = appendParsedDirectives(set, parsedIncludesLocal, local...)
	set = appendParsedDirectives(set, parsedIncludesRagelNative, native...)
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
	out := parseYasmIncludes(data)

	if len(out) == 0 {
		return parsedIncludeSet{}
	}

	return parsedIncludeSet{parsedIncludesLocal: out}
}

func (emptyIncludeDirectiveParser) Parse(_ string, _ []byte) parsedIncludeSet {
	return parsedIncludeSet{}
}

func parseCIncludes(data []byte) []includeDirective {
	out := make([]includeDirective, 0, 8)
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

func parseDirectiveInline(data []byte, hashPos int) (includeDirective, bool, int) {
	n := len(data)
	q := skipWSAndBlockComments(data, hashPos+1)

	switch {
	case bytesHasPrefixAt(data, q, "include") && !identByteAt(data, q+len("include")):
		q += len("include")
	case bytesHasPrefixAt(data, q, "import") && !identByteAt(data, q+len("import")):
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

		if q > start && data[start] != '$' && !hasYIgnoreComment(data, q) && !bytes.ContainsAny(data[start:q], "[]") {
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

	if !hasYIgnoreComment(data, q) && !bytes.ContainsAny(targetBytes, "[]") {
		return includeDirective{kind: kind, target: internBytes(targetBytes)}, true, nextLineStart(data, q)
	}

	return includeDirective{}, false, nextLineStart(data, q)
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

func parseYasmIncludes(data []byte) []includeDirective {
	out := make([]includeDirective, 0, 4)

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
