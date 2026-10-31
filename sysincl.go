package main

import (
	"fmt"
	"regexp"
	"regexp/syntax"
	"strings"
)

var (
	envSYSINCL          = internEnv(sysinclVar)
	sysinclLowerScratch []byte
)

const (
	sysinclVar             = "SYSINCL"
	maxLiteralAltExpansion = 64
)

type SysIncl struct {
	Filter          *SourceFilter
	HasMultiTarget  bool
	CaseInsensitive bool
	pairs           []SysinclPair
}

type SysinclPair struct {
	key   STR
	keyCI string
	paths []VFS
}

func (p *SysinclPair) id() STR {
	if p.key != 0 {
		return p.key
	}

	return internStr(p.keyCI)
}

type SysInclSet []SysIncl

func (rec *SysIncl) setMapping(k []byte, paths []VFS) {
	if len(paths) >= 2 {
		rec.HasMultiTarget = true
	}

	if rec.CaseInsensitive {
		lower := sysinclLowerScratch[:0]

		for _, c := range k {
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}

			lower = append(lower, c)
		}

		sysinclLowerScratch = lower
		rec.pairs = append(rec.pairs, SysinclPair{keyCI: internBytes(lower).string(), paths: paths})
	} else {
		rec.pairs = append(rec.pairs, SysinclPair{key: internBytes(k), paths: paths})
	}
}

func (pm *IncludeParserManager) sysinclRecords(rel string, onWarn func(Warn)) []SysIncl {
	key := internStr(rel)

	if records, ok := pm.cache.sysinclYAMLs.get(key); ok {
		return records
	}

	if !pm.fs.isFile(srcRootRel, rel) {
		throwFmt("sysincl: %s: no such file", rel)
	}

	records := parseSysInclYAML(rel, concatChunks(pm.fs.readPath(key)), onWarn)

	pm.cache.sysinclYAMLs.put(key, records)

	return records
}

func loadSysInclSet(pm *IncludeParserManager, p *Platform, onWarn func(Warn)) SysInclSet {
	var set SysInclSet

	for _, rel := range p.Sysincl {
		set = append(set, pm.sysinclRecords(rel, onWarn)...)
	}

	return set
}

type SourceFilter struct {
	alts        []FilterAlt
	unsupported bool
}

type FilterAlt struct {
	excludePrefixes []string
	excludeRes      []*regexp.Regexp
	residual        *regexp.Regexp
	lookPrefix      string
	lookahead       bool
	literalPrefix   string
	containsLit     string
	reGuard         string
	re              *regexp.Regexp
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

	if a.lookahead {
		tail, ok := strings.CutPrefix(sourcePath, a.lookPrefix)

		if !ok {
			return false
		}

		for _, re := range a.excludeRes {
			if re.MatchString(tail) {
				return false
			}
		}

		if a.residual != nil {
			return a.residual.MatchString(tail)
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

	if strings.HasPrefix(pat, "^") {
		alt.reGuard, _ = re.LiteralPrefix()
	}
}

func compileSourceFilter(name string, lineno int, pat string, onWarn func(Warn)) *SourceFilter {
	if pat == "" {
		return nil
	}

	f := &SourceFilter{}

	exc := try(func() {
		altStrs := splitTopLevelOr(pat)

		for _, altStr := range altStrs {
			alt := FilterAlt{}

			if strings.Contains(altStr, "(?!") {
				alt.setLookahead(name, lineno, altStr)
			} else {
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

func (alt *FilterAlt) setLookahead(name string, lineno int, pat string) {
	body, ok := strings.CutPrefix(pat, "^")

	if !ok {
		throwFmt("sysincl: %s:%d: unsupported negative lookahead position in %q", name, lineno, pat)
	}

	prefix, body := cutLiteralFilterPrefix(body)

	alt.lookahead, alt.lookPrefix = true, prefix

	for strings.HasPrefix(body, "(?!") {
		end := closingRegexParen(body)

		if end < 0 {
			throwFmt("sysincl: malformed negative lookahead in %q", pat)
		}

		if literals, ok := lookaheadLiterals(body[3:end]); ok {
			for _, l := range literals {
				alt.excludePrefixes = append(alt.excludePrefixes, prefix+l)
			}
		} else {
			alt.excludeRes = append(alt.excludeRes, compileSysinclRegex(name, lineno, "^(?:"+body[3:end]+")"))
		}

		body = body[end+1:]
	}

	switch {
	case len(alt.excludePrefixes)+len(alt.excludeRes) == 0 || strings.Contains(body, "(?!"):
		throwFmt("sysincl: %s:%d: unsupported negative lookahead position in %q", name, lineno, pat)
	case body == "" || body == ".*":
		alt.literalPrefix = prefix
	case !containsRegexMeta(body):
		alt.literalPrefix = prefix + body
	default:
		alt.residual = compileSysinclRegex(name, lineno, "^(?:"+body+")")
	}
}

func compileSysinclRegex(name string, lineno int, pat string) *regexp.Regexp {
	re, err := regexp.Compile(pat)

	if err != nil {
		throwFmt("sysincl: %s:%d: cannot compile %q: %v", name, lineno, pat, err)
	}

	return re
}

func cutLiteralFilterPrefix(body string) (string, string) {
	if strings.HasPrefix(body, "(?:") {
		if end := closingRegexParen(body); end > 0 && !containsRegexMeta(body[3:end]) {
			return body[3:end], body[end+1:]
		}

		return "", body
	}

	i := 0

	for i < len(body) && !containsRegexMeta(body[i:i+1]) {
		i++
	}

	return body[:i], body[i:]
}

func closingRegexParen(s string) int {
	depth := 0
	bracket := false

	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\\':
			i++
		case bracket:
			bracket = c != ']'
		case c == '[':
			bracket = true
		case c == '(':
			depth++
		case c == ')':
			if depth--; depth == 0 {
				return i
			}
		}
	}

	return -1
}

func lookaheadLiterals(body string) ([]string, bool) {
	if strings.HasPrefix(body, "(") && closingRegexParen(body) == len(body)-1 {
		body = body[1 : len(body)-1]
	}

	parts := splitTopLevelOr(body)

	for _, p := range parts {
		if p == "" || containsRegexMeta(p) {
			return nil, false
		}
	}

	return parts, true
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
