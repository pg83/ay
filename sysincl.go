package main

import (
	"fmt"
	"regexp"
	"regexp/syntax"
	"sort"
	"strings"
	"sync"
)

var sysInclYamlSequence = []sysInclEntry{
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
	{file: "opensource.yml"},
	{file: "libc-to-musl.yml"},
	{file: "linux-musl-aarch64.yml", predicate: archIs("aarch64")},
	{file: "linux-musl.yml", predicate: archIs("x86_64")},
	{file: "emscripten-to-nothing.yml"},
	{file: "nvidia-cccl.yml"},
	{file: "stl-to-libcxx.yml"},
	{file: "libc-musl-libcxx.yml"},

	{file: "python-2-disable.yml"},
	{file: "python-2-disable-numpy.yml"},
}

var supportedSysInclArchs = map[string]struct{}{
	"aarch64": {},
	"x86_64":  {},
}

var _ = sort.Strings

type SysIncl struct {
	Filter         *sourceFilter
	KeyBySource    bool
	HasMultiTarget bool

	CaseInsensitive bool
	Mappings        map[string][]string
}

type SysInclSet []SysIncl

func recordKey(rec *SysIncl, k string) string {
	if rec.CaseInsensitive {
		return strings.ToLower(k)
	}
	return k
}

func recordQuery(rec *SysIncl, header string) string {
	if rec.CaseInsensitive {
		return strings.ToLower(header)
	}
	return header
}

func (s SysInclSet) Lookup(sourcePath, includerPath, header string) ([]string, bool) {
	view := s.PreparePerSource(sourcePath)

	return view.Lookup(includerPath, header)
}

type PerSourceView struct {
	activeSourceKeyed []*SysIncl

	includerKeyed []*SysIncl

	includerFilterCache *includerFilterCache
}

type includerFilterCache struct {
	mu sync.RWMutex

	active map[string][]*SysIncl
}

func newIncluderFilterCache() *includerFilterCache {
	return &includerFilterCache{active: make(map[string][]*SysIncl, 64)}
}

func (s SysInclSet) PreparePerSource(sourcePath string) PerSourceView {
	view := PerSourceView{
		activeSourceKeyed:   make([]*SysIncl, 0, len(s)),
		includerKeyed:       make([]*SysIncl, 0, len(s)),
		includerFilterCache: newIncluderFilterCache(),
	}

	for i := range s {
		rec := &s[i]

		if rec.KeyBySource {
			if rec.Filter == nil || rec.Filter.match(sourcePath) {
				view.activeSourceKeyed = append(view.activeSourceKeyed, rec)
			}

			continue
		}

		view.includerKeyed = append(view.includerKeyed, rec)
	}

	return view
}

func (v PerSourceView) Lookup(includerPath, header string) ([]string, bool) {
	srcOut, srcFound, _ := v.LookupSourceKeyed(header)
	incOut, incFound, _ := v.LookupIncluderKeyed(includerPath, header)

	if !srcFound && !incFound {
		return nil, false
	}

	if len(srcOut) == 0 {
		return incOut, true
	}

	if len(incOut) == 0 {
		return srcOut, true
	}

	out := make([]string, 0, len(srcOut)+len(incOut))
	seen := make(map[string]struct{}, len(srcOut)+len(incOut))

	for _, p := range srcOut {
		if _, dup := seen[p]; dup {
			continue
		}

		seen[p] = struct{}{}
		out = append(out, p)
	}

	for _, p := range incOut {
		if _, dup := seen[p]; dup {
			continue
		}

		seen[p] = struct{}{}
		out = append(out, p)
	}

	return out, true
}

func (v PerSourceView) LookupSourceKeyed(header string) ([]string, bool, bool) {
	var (
		out            []string
		found          bool
		hasMultiTarget bool
		seen           map[string]struct{}
	)

	for _, rec := range v.activeSourceKeyed {
		paths, ok := rec.Mappings[recordQuery(rec, header)]

		if !ok {
			continue
		}

		found = true

		if rec.HasMultiTarget {
			count := 0

			for _, p := range paths {
				if p != "" {
					count++
				}
			}

			if count >= 2 {
				hasMultiTarget = true
			}
		}

		for _, p := range paths {
			if p == "" {
				continue
			}

			if seen == nil {
				seen = make(map[string]struct{}, 4)
			}

			if _, dup := seen[p]; dup {
				continue
			}

			seen[p] = struct{}{}
			out = append(out, p)
		}
	}

	return out, found, hasMultiTarget
}

func (v PerSourceView) LookupIncluderKeyed(includerPath, header string) ([]string, bool, bool) {
	return unionIncluderMappings(v.activeIncluderRecords(includerPath), header)
}

func unionIncluderMappings(active []*SysIncl, header string) ([]string, bool, bool) {
	var (
		out            []string
		found          bool
		hasMultiTarget bool
		seen           map[string]struct{}
	)

	for _, rec := range active {
		paths, ok := rec.Mappings[recordQuery(rec, header)]

		if !ok {
			continue
		}

		found = true

		if rec.HasMultiTarget {
			count := 0

			for _, p := range paths {
				if p != "" {
					count++
				}
			}

			if count >= 2 {
				hasMultiTarget = true
			}
		}

		for _, p := range paths {
			if p == "" {
				continue
			}

			if seen == nil {
				seen = make(map[string]struct{}, 4)
			}

			if _, dup := seen[p]; dup {
				continue
			}

			seen[p] = struct{}{}
			out = append(out, p)
		}
	}

	return out, found, hasMultiTarget
}

func (v PerSourceView) activeIncluderRecords(includerPath string) []*SysIncl {
	if v.includerFilterCache == nil {

		return v.computeActiveIncluderRecords(includerPath)
	}

	c := v.includerFilterCache

	c.mu.RLock()
	cached, ok := c.active[includerPath]
	c.mu.RUnlock()

	if ok {
		return cached
	}

	active := v.computeActiveIncluderRecords(includerPath)

	c.mu.Lock()

	if existing, dup := c.active[includerPath]; dup {
		c.mu.Unlock()

		return existing
	}

	c.active[includerPath] = active
	c.mu.Unlock()

	return active
}

func (v PerSourceView) computeActiveIncluderRecords(includerPath string) []*SysIncl {
	var active []*SysIncl

	for _, rec := range v.includerKeyed {
		if rec.Filter != nil && !rec.Filter.match(includerPath) {
			continue
		}

		active = append(active, rec)
	}

	return active
}

type sysInclEnv struct {
	arch string
}

type sysInclEntry struct {
	file      string
	predicate func(sysInclEnv) bool
}

func archIs(want string) func(sysInclEnv) bool {
	return func(e sysInclEnv) bool { return e.arch == want }
}

func LoadSysInclSetFor(sourceRoot, arch string, onWarn func(Warn)) SysInclSet {
	return LoadSysInclSetForFS(NewFS(sourceRoot), arch, onWarn)
}

func LoadSysInclSetForFS(fs FS, arch string, onWarn func(Warn)) SysInclSet {
	if !fs.IsDir("build/sysincl") {
		return nil
	}

	if _, ok := supportedSysInclArchs[arch]; !ok {
		ThrowFmt("LoadSysInclSetFor: unsupported arch %q (want aarch64 or x86_64)", arch)
	}

	env := sysInclEnv{arch: arch}

	var set SysInclSet

	for _, entry := range sysInclYamlSequence {
		if entry.predicate != nil && !entry.predicate(env) {
			continue
		}

		rel := "build/sysincl/" + entry.file
		if !fs.IsFile(rel) {
			continue
		}

		records := parseSysInclYAML(entry.file, string(fs.Read(rel)), onWarn)
		set = append(set, records...)
	}

	return set
}

func parseSysInclYAML(name, text string, onWarn func(Warn)) []SysIncl {
	lines := strings.Split(text, "\n")

	var (
		out     []SysIncl
		current *SysIncl

		pendingKey   string
		pendingPaths []string

		inIncludes bool
	)

	flushPending := func() {
		if pendingKey == "" {
			return
		}

		if current == nil {
			ThrowFmt("sysincl: %s: pending key %q with no active record", name, pendingKey)
		}

		key := recordKey(current, pendingKey)
		if pendingPaths == nil {
			current.Mappings[key] = nil
		} else {
			current.Mappings[key] = pendingPaths
		}

		pendingKey = ""
		pendingPaths = nil
	}

	flushRecord := func() {
		flushPending()

		if current != nil {

			for _, paths := range current.Mappings {
				count := 0

				for _, p := range paths {
					if p != "" {
						count++
					}
				}

				if count >= 2 {
					current.HasMultiTarget = true

					break
				}
			}

			out = append(out, *current)
			current = nil
			inIncludes = false
		}
	}

	for i, raw := range lines {
		lineno := i + 1

		stripped := stripComment(raw)
		trimmed := strings.TrimRight(stripped, " \t\r")

		if strings.TrimSpace(trimmed) == "" {
			continue
		}

		indent := leadingSpaces(trimmed)
		body := trimmed[indent:]

		if indent == 0 && strings.HasPrefix(body, "- ") {
			flushRecord()

			current = &SysIncl{Mappings: make(map[string][]string)}

			rest := strings.TrimSpace(body[2:])

			handleRecordHeader(name, lineno, rest, current, &inIncludes, onWarn)

			continue
		}

		if current == nil {
			ThrowFmt("sysincl: %s:%d: line outside any record: %q", name, lineno, body)
		}

		if !inIncludes {
			handleRecordHeader(name, lineno, body, current, &inIncludes, onWarn)

			continue
		}

		if !strings.HasPrefix(body, "- ") && body != "-" {
			ThrowFmt("sysincl: %s:%d: expected list entry, got %q", name, lineno, body)
		}

		var entry string

		if body == "-" {
			entry = ""
		} else {
			entry = strings.TrimSpace(body[2:])
		}

		if pendingKey != "" && !strings.Contains(entry, ":") {
			pendingPaths = append(pendingPaths, unquote(entry))

			continue
		}

		flushPending()

		key, val, hasMapping := splitKeyValue(entry)

		if !hasMapping {

			current.Mappings[recordKey(current, key)] = nil

			continue
		}

		if val == "" {

			pendingKey = key
			pendingPaths = nil

			continue
		}

		v := unquote(val)

		if v == "" {
			current.Mappings[recordKey(current, key)] = []string{""}
		} else {
			current.Mappings[recordKey(current, key)] = []string{v}
		}
	}

	flushRecord()

	return out
}

func handleRecordHeader(name string, lineno int, body string, rec *SysIncl, inIncludes *bool, onWarn func(Warn)) {
	if body == "" || body == "includes:" {
		*inIncludes = true

		return
	}

	if strings.HasPrefix(body, "source_filter:") {
		rest := strings.TrimSpace(body[len("source_filter:"):])
		pat := unquote(rest)
		rec.Filter = compileSourceFilter(name, lineno, pat, onWarn)

		rec.KeyBySource = strings.Contains(pat, "(?!")

		return
	}

	if strings.HasPrefix(body, "includes:") {

		*inIncludes = true

		return
	}

	if strings.HasPrefix(body, "case_sensitive:") {
		val := strings.TrimSpace(body[len("case_sensitive:"):])
		if val == "false" {
			rec.CaseInsensitive = true
		}
		return
	}

	onWarn(Warn{
		Kind:    WarnSysIncl,
		Message: fmt.Sprintf("%s:%d: source_filter %q unsupported (unrecognised record header) — record disabled", name, lineno, body),
	})
	rec.Filter = &sourceFilter{unsupported: true}
}

func stripComment(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '#' {
			if i == 0 || s[i-1] == ' ' || s[i-1] == '\t' {
				return s[:i]
			}
		}
	}

	return s
}

func leadingSpaces(s string) int {
	i := 0

	for i < len(s) && s[i] == ' ' {
		i++
	}

	return i
}

func splitKeyValue(s string) (string, string, bool) {
	depth := 0
	colon := -1

	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ':':
			if depth == 0 {
				colon = i

				break
			}
		}

		if colon >= 0 {
			break
		}
	}

	if colon < 0 {
		return s, "", false
	}

	key := strings.TrimSpace(s[:colon])
	val := strings.TrimSpace(s[colon+1:])

	return key, val, true
}

func unquote(s string) string {
	if len(s) >= 2 {
		first := s[0]
		last := s[len(s)-1]

		if first == last && (first == '"' || first == '\'') {
			return s[1 : len(s)-1]
		}
	}

	return s
}

type sourceFilter struct {
	alts        []filterAlt
	unsupported bool
}

type filterAlt struct {
	excludePrefixes []string
	literalPrefix   string

	containsLit string

	reGuard string
	re      *regexp.Regexp
}

func (f *sourceFilter) match(sourcePath string) bool {
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

func (a *filterAlt) matches(sourcePath string) bool {
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

func (alt *filterAlt) setPositive(name string, lineno int, pat string) {
	if lit, ok := literalContainsPattern(pat); ok {
		alt.containsLit = lit

		return
	}

	re, err := regexp.Compile(pat)
	if err != nil {
		ThrowFmt("sysincl: %s:%d: cannot compile %q: %v", name, lineno, pat, err)
	}

	alt.re = re
	alt.reGuard, _ = re.LiteralPrefix()
}

func compileSourceFilter(name string, lineno int, pat string, onWarn func(Warn)) *sourceFilter {
	if pat == "" {
		return nil
	}

	f := &sourceFilter{}

	exc := Try(func() {
		altStrs := splitTopLevelOr(pat)

		for _, altStr := range altStrs {
			excludes, residual, isExclude := extractNegativeLookahead(altStr)

			alt := filterAlt{}

			if isExclude {
				alt.excludePrefixes = excludes

				if residual != "" {
					if strings.Contains(residual, "(?!") {
						ThrowFmt("sysincl: %s:%d: unsupported negative lookahead position in %q (residual after ^(?!): %q)", name, lineno, altStr, residual)
					}

					if residual == ".*" {

					} else if lit := extractLiteralAnchoredPrefix(residual); lit != "" {
						alt.literalPrefix = lit
					} else {
						alt.setPositive(name, lineno, residual)
					}
				}
			} else {
				if strings.Contains(altStr, "(?!") {
					ThrowFmt("sysincl: %s:%d: unsupported negative lookahead position in %q", name, lineno, altStr)
				}

				if lit := extractLiteralAnchoredPrefix(altStr); lit != "" {
					alt.literalPrefix = lit
				} else if prefixes, ok := literalAltsFromRegex(altStr); ok {

					for _, p := range prefixes {
						f.alts = append(f.alts, filterAlt{literalPrefix: p})
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

		return &sourceFilter{unsupported: true}
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
		ThrowFmt("sysincl: malformed negative lookahead in %q", pat)
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
			ThrowFmt("sysincl: negative-lookahead alt %q has regex metacharacters; only literal prefixes are supported", p)
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
