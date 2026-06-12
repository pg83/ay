package main

import (
	"fmt"
	"regexp"
	"regexp/syntax"
	"sort"
	"strings"
)

var sysInclYamlSequence = []SysInclEntry{
	{file: "macro.yml"},
	{file: "libc-to-compat.yml"},
	{file: "libc-to-nothing.yml"},
	{file: "stl-to-libstdcxx.yml"},
	{file: "stl-to-nothing.yml"},
	{file: "windows.yml"},
	{file: "darwin.yml"},
	{file: "android.yml"},
	{file: "freebsd.yml"},
	{file: "freertos.yml"},
	{file: "intrinsic.yml"},
	{file: "nvidia.yml"},
	{file: "misc.yml"},
	{file: "unsorted.yml"},
	{file: "swig.yml"},
	{file: "libiconv.yml"},
	{file: "libidn.yml"},
	{file: "jdk-to-arcadia.yml"},
	// opensource.yml and proto.yml are mutually exclusive (build/conf/sysincl.conf:45
	// `when ($OPENSOURCE == "yes") { opensource.yml } otherwise { proto.yml }`). proto.yml
	// resolves google/protobuf/*.proto to all three vendored variants (protobuf,
	// protobuf_std, protobuf_old) — needed for the internal contour's proto compiles.
	{file: "opensource.yml", predicate: opensourceOn},
	{file: "proto.yml", predicate: notOpensource},
	{file: "libc-to-musl.yml", predicate: muslOn},
	{file: "linux-musl-aarch64.yml", predicate: muslArchIs("aarch64")},
	{file: "linux-musl.yml", predicate: muslArchIs("x86_64")},
	{file: "emscripten-to-nothing.yml"},
	{file: "nvidia-cccl.yml"},
	{file: "stl-to-libcxx.yml"},
	{file: "libc-musl-libcxx.yml", predicate: muslOn},
	{file: "python-2-disable.yml"},
	{file: "python-2-disable-numpy.yml"},
}

var supportedSysInclArchs = map[string]struct{}{
	"aarch64": {},
	"x86_64":  {},
}

const (
	baseSysInclDir     = "build/sysincl"
	internalSysInclDir = "build/internal/sysincl"
)

type SysIncl struct {
	Filter         *SourceFilter
	KeyBySource    bool
	HasMultiTarget bool

	CaseInsensitive bool

	// pairs accumulates the record's cooked header->targets mappings in parse
	// order: CS keys interned (key), CI keys lowercased (keyCI). The YAMLs
	// carry intra-record duplicate headers (darwin, libc-to-musl, misc, …)
	// with last-wins semantics — the index build dedups keeping the LAST
	// occurrence, matching the old per-record map staging.
	pairs []SysinclPair
}

type SysinclPair struct {
	key   STR    // case-sensitive arm; 0 for CI records
	keyCI string // case-insensitive arm, lowercased; "" for CS records
	paths []VFS
}

type SysInclSet []SysIncl

// setMapping stores one cooked header→targets mapping into the record's arm
// (CS keys interned straight from the bytes, CI keys lowercased). paths carries
// no empty entries, so len>=2 is exactly the multi-target condition.
func (rec *SysIncl) setMapping(k []byte, paths []VFS) {
	if len(paths) >= 2 {
		rec.HasMultiTarget = true
	}

	if rec.CaseInsensitive {
		rec.pairs = append(rec.pairs, SysinclPair{keyCI: strings.ToLower(string(k)), paths: paths})
	} else {
		rec.pairs = append(rec.pairs, SysinclPair{key: internBytes(k), paths: paths})
	}
}

type SysInclEnv struct {
	arch       string
	musl       bool
	opensource bool
}

type SysInclEntry struct {
	file      string
	predicate func(SysInclEnv) bool
}

func opensourceOn(e SysInclEnv) bool {
	return e.opensource
}

func notOpensource(e SysInclEnv) bool {
	return !e.opensource
}

func archIs(want string) func(SysInclEnv) bool {
	return func(e SysInclEnv) bool { return e.arch == want }
}

// muslOn / muslArchIs gate the musl libc & stl sysincl files, which upstream
// loads only under MUSL=yes (build/conf/sysincl.conf:52,
// build/ymake.core.conf:349). Under glibc these must not apply, or libc headers
// remap into contrib/libs/musl.
func muslOn(e SysInclEnv) bool {
	return e.musl
}

func muslArchIs(want string) func(SysInclEnv) bool {
	return func(e SysInclEnv) bool { return e.musl && e.arch == want }
}

func loadSysInclSetForFS(fs FS, arch string, musl, opensource bool, onWarn func(Warn)) SysInclSet {
	if !fs.isDir(srcRootVFS, baseSysInclDir) {
		return nil
	}

	if _, ok := supportedSysInclArchs[arch]; !ok {
		throwFmt("LoadSysInclSetFor: unsupported arch %q (want aarch64 or x86_64)", arch)
	}

	env := SysInclEnv{arch: arch, musl: musl, opensource: opensource}
	var set SysInclSet
	sysinclDir := dirKey(baseSysInclDir)

	for _, entry := range sysInclYamlSequence {
		if entry.predicate != nil && !entry.predicate(env) {
			continue
		}

		if !fs.isFile(sysinclDir, entry.file) {
			continue
		}

		records := parseSysInclYAML(entry.file, fs.read(baseSysInclDir+"/"+entry.file), onWarn)
		set = append(set, records...)
	}

	// The internal contour (OPENSOURCE != yes) layers build/internal/sysincl/* on top
	// of the base set (build/internal/conf/sysincl.conf). Rather than track the curated
	// list, load every .yml in that directory — files for absent platforms/projects
	// (qt, smart_devices_*, …) map headers that never appear in these sources, so they
	// are inert. Sorted for deterministic precedence; loaded after the base set so they
	// override it (e.g. taxi.yml's <errno.h>/<pthread.h> → userver libc_workarounds).
	if !opensource {
		set = append(set, loadSysInclDir(fs, internalSysInclDir, onWarn)...)
	}

	return set
}

// loadSysInclDir parses every *.yml in a source-tree sysincl directory, in sorted
// filename order. Absent directory → no records.
func loadSysInclDir(fs FS, dir string, onWarn func(Warn)) SysInclSet {
	if !fs.isDir(srcRootVFS, dir) {
		return nil
	}

	names := make([]string, 0, len(fs.listdir(source(dir))))

	for name := range fs.listdir(source(dir)) {
		if strings.HasSuffix(name, ".yml") {
			names = append(names, name)
		}
	}

	sort.Strings(names)

	var set SysInclSet

	for _, name := range names {
		records := parseSysInclYAML(name, fs.read(dir+"/"+name), onWarn)
		set = append(set, records...)
	}

	return set
}

type SourceFilter struct {
	alts        []FilterAlt
	unsupported bool
}

type FilterAlt struct {
	excludePrefixes []string
	literalPrefix   string

	containsLit string

	reGuard string
	re      *regexp.Regexp
}

func (f *SourceFilter) match(sourcePath string) bool {
	if f.unsupported {
		return false
	}

	for i := range f.alts {
		if f.alts[i].matches(sourcePath) {
			return true
		}
	}

	return false
}

func (a *FilterAlt) matches(sourcePath string) bool {
	for _, p := range a.excludePrefixes {
		if strings.HasPrefix(sourcePath, p) {
			return false
		}
	}

	if a.literalPrefix != "" {
		return strings.HasPrefix(sourcePath, a.literalPrefix)
	}

	if a.containsLit != "" {
		return strings.Contains(sourcePath, a.containsLit)
	}

	if a.re == nil {
		return true
	}

	if a.reGuard != "" && !strings.HasPrefix(sourcePath, a.reGuard) {
		return false
	}

	return a.re.MatchString(sourcePath)
}

func literalContainsPattern(pat string) (string, bool) {
	mid, ok := strings.CutPrefix(pat, ".*")

	if !ok {
		return "", false
	}

	mid, ok = strings.CutSuffix(mid, ".*")

	if !ok || mid == "" || regexp.QuoteMeta(mid) != mid {
		return "", false
	}

	return mid, true
}

const maxLiteralAltExpansion = 64

func literalAltsFromRegex(pat string) ([]string, bool) {
	re, err := syntax.Parse(pat, syntax.Perl)

	if err != nil {
		return nil, false
	}

	if re.Op != syntax.OpConcat || len(re.Sub) == 0 || re.Sub[0].Op != syntax.OpBeginText {
		return nil, false
	}

	acc := []string{""}

	for _, sub := range re.Sub[1:] {
		set, ok := literalSet(sub)

		if !ok {
			return nil, false
		}

		acc = crossConcat(acc, set)

		if len(acc) > maxLiteralAltExpansion {
			return nil, false
		}
	}

	return acc, true
}

func literalSet(re *syntax.Regexp) ([]string, bool) {
	switch re.Op {
	case syntax.OpEmptyMatch:
		return []string{""}, true
	case syntax.OpLiteral:
		if re.Flags&syntax.FoldCase != 0 {
			return nil, false
		}

		return []string{string(re.Rune)}, true
	case syntax.OpCapture:
		return literalSet(re.Sub[0])
	case syntax.OpConcat:
		acc := []string{""}

		for _, sub := range re.Sub {
			set, ok := literalSet(sub)

			if !ok {
				return nil, false
			}

			acc = crossConcat(acc, set)

			if len(acc) > maxLiteralAltExpansion {
				return nil, false
			}
		}

		return acc, true
	case syntax.OpAlternate:
		var out []string

		for _, sub := range re.Sub {
			set, ok := literalSet(sub)

			if !ok {
				return nil, false
			}

			out = append(out, set...)

			if len(out) > maxLiteralAltExpansion {
				return nil, false
			}
		}

		return out, true
	default:
		return nil, false
	}
}

func crossConcat(prefixes, suffixes []string) []string {
	out := make([]string, 0, len(prefixes)*len(suffixes))

	for _, p := range prefixes {
		for _, s := range suffixes {
			out = append(out, p+s)
		}
	}

	return out
}

func (alt *FilterAlt) setPositive(name string, lineno int, pat string) {
	if lit, ok := literalContainsPattern(pat); ok {
		alt.containsLit = lit

		return
	}

	re, err := regexp.Compile(pat)

	if err != nil {
		throwFmt("sysincl: %s:%d: cannot compile %q: %v", name, lineno, pat, err)
	}

	alt.re = re
	alt.reGuard, _ = re.LiteralPrefix()
}

func compileSourceFilter(name string, lineno int, pat string, onWarn func(Warn)) *SourceFilter {
	if pat == "" {
		return nil
	}

	f := &SourceFilter{}
	exc := try(func() {
		altStrs := splitTopLevelOr(pat)

		for _, altStr := range altStrs {
			excludes, residual, isExclude := extractNegativeLookahead(altStr)
			alt := FilterAlt{}

			if isExclude {
				alt.excludePrefixes = excludes

				if residual != "" {
					if strings.Contains(residual, "(?!") {
						throwFmt("sysincl: %s:%d: unsupported negative lookahead position in %q (residual after ^(?!): %q)", name, lineno, altStr, residual)
					}

					if residual == ".*" {
					} else if lit := extractLiteralAnchoredPrefix(residual); lit != "" {
						alt.literalPrefix = lit
					} else {
						alt.setPositive(name, lineno, residual)
					}
				}
			} else if lit, ex, res, okP := extractPrefixedNegativeLookahead(altStr); okP {
				// `^<literal>(?!<alts>)`: require the literal prefix, reject any
				// literal+alt. The full excluded prefix is literal+alt.
				alt.literalPrefix = lit

				for _, e := range ex {
					alt.excludePrefixes = append(alt.excludePrefixes, lit+e)
				}

				if res != "" && res != ".*" {
					throwFmt("sysincl: %s:%d: unsupported residual %q after prefixed negative lookahead in %q", name, lineno, res, altStr)
				}
			} else {
				if strings.Contains(altStr, "(?!") {
					throwFmt("sysincl: %s:%d: unsupported negative lookahead position in %q", name, lineno, altStr)
				}

				if lit := extractLiteralAnchoredPrefix(altStr); lit != "" {
					alt.literalPrefix = lit
				} else if prefixes, ok := literalAltsFromRegex(altStr); ok {
					for _, p := range prefixes {
						f.alts = append(f.alts, FilterAlt{literalPrefix: p})
					}

					continue
				} else {
					alt.setPositive(name, lineno, altStr)
				}
			}

			f.alts = append(f.alts, alt)
		}
	})

	if exc != nil {
		onWarn(Warn{
			Kind:    WarnSysIncl,
			Message: fmt.Sprintf("%s:%d: source_filter %q unsupported (%s) — record disabled", name, lineno, pat, exc.Error()),
		})

		return &SourceFilter{unsupported: true}
	}

	return f
}

func splitTopLevelOr(pat string) []string {
	depth := 0
	bracket := false
	out := []string{}
	last := 0

	for i := 0; i < len(pat); i++ {
		c := pat[i]

		switch {
		case c == '\\':
			i++
		case c == '[':
			bracket = true
		case c == ']':
			bracket = false
		case c == '(' && !bracket:
			depth++
		case c == ')' && !bracket:
			depth--
		case c == '|' && depth == 0 && !bracket:
			out = append(out, pat[last:i])
			last = i + 1
		}
	}

	out = append(out, pat[last:])

	return out
}

// extractPrefixedNegativeLookahead handles the well-known form
// `^<literal>(?!<alt1|alt2|…>)<residual>`, where a regex-meta-free literal sits
// between the anchor and the lookahead (e.g. `^contrib(?!/restricted/libnl)`).
// RE2 has no lookahead, so it is matched in Go as: starts-with <literal> AND not
// starts-with <literal><alt> for any alt. Returns the literal, the excluded
// suffixes (relative to the literal), the residual after the group, and ok.
func extractPrefixedNegativeLookahead(pat string) (literal string, excludes []string, residual string, ok bool) {
	if !strings.HasPrefix(pat, "^") {
		return "", nil, "", false
	}

	body := pat[1:]
	i := strings.Index(body, "(?!")

	if i <= 0 { // i<0: no lookahead; i==0: the bare ^(?! form (handled by extractNegativeLookahead)
		return "", nil, "", false
	}

	literal = body[:i]

	if containsRegexMeta(literal) {
		return "", nil, "", false
	}

	// Reuse the group scan + alt split by re-anchoring the lookahead tail.
	ex, res, isExc := extractNegativeLookahead("^" + body[i:])

	if !isExc {
		return "", nil, "", false
	}

	return literal, ex, res, true
}

func extractNegativeLookahead(pat string) ([]string, string, bool) {
	const prefix = "^(?!"

	if !strings.HasPrefix(pat, prefix) {
		return nil, "", false
	}

	rest := pat[len(prefix):]
	depth := 1
	end := -1

	for i := 0; i < len(rest); i++ {
		c := rest[i]

		switch c {
		case '\\':
			i++
		case '(':
			depth++
		case ')':
			depth--

			if depth == 0 {
				end = i
			}
		}

		if end >= 0 {
			break
		}
	}

	if end < 0 {
		throwFmt("sysincl: malformed negative lookahead in %q", pat)
	}

	inner := rest[:end]
	residual := rest[end+1:]

	excludes := parseLookaheadAlts(inner)

	return excludes, residual, true
}

func parseLookaheadAlts(body string) []string {
	body = strings.TrimSpace(body)

	if strings.HasPrefix(body, "(") && strings.HasSuffix(body, ")") {
		body = body[1 : len(body)-1]
	}

	parts := splitTopLevelOr(body)

	out := make([]string, 0, len(parts))

	for _, p := range parts {
		p = strings.TrimSpace(p)

		if p == "" {
			continue
		}

		if containsRegexMeta(p) {
			throwFmt("sysincl: negative-lookahead alt %q has regex metacharacters; only literal prefixes are supported", p)
		}

		out = append(out, p)
	}

	return out
}

func containsRegexMeta(s string) bool {
	const meta = `\.+*?[]{}()|^$`

	for i := 0; i < len(s); i++ {
		for j := 0; j < len(meta); j++ {
			if s[i] == meta[j] {
				return true
			}
		}
	}

	return false
}

func extractLiteralAnchoredPrefix(pat string) string {
	if !strings.HasPrefix(pat, "^") {
		return ""
	}

	body := pat[1:]

	if body == "" {
		return ""
	}

	if containsRegexMeta(body) {
		return ""
	}

	return body
}
