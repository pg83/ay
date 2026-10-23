package main

import (
	"fmt"
	"regexp"
	"regexp/syntax"
	"slices"
	"strings"
)

var envSYSINCL = internEnv(sysinclVar)

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

type SysInclSet []SysIncl

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

type ConfStmtRef struct {
	file *ConfFile
	stmt ConfStmt
}

func (pm *IncludeParserManager) confFile(rel string) *ConfFile {
	key := internStr(rel)

	if f, ok := pm.cache.confFiles.get(key); ok {
		return f
	}

	if !pm.fs.isFile(srcRootRel, rel) {
		throwFmt("conf: @import %s: no such file", rel)
	}

	f := readConfFile(rel, string(concatChunks(pm.fs.readPath(key))))

	pm.cache.confFiles.put(key, f)

	return f
}

func sysinclFiles(pm *IncludeParserManager, root *ConfFile, platformEnv Environment) ([]string, map[string]*ConfStyleMacro) {
	macros := map[string]*ConfStyleMacro{}

	var (
		stmts   []ConfStmtRef
		walking []string
		walk    func(f *ConfFile)
	)

	walk = func(f *ConfFile) {
		for name, macro := range f.styleMacros {
			macros[name] = macro
		}

		for _, s := range f.stmts {
			if s.name != "" {
				stmts = append(stmts, ConfStmtRef{file: f, stmt: s})

				continue
			}

			if slices.Contains(walking, s.value) {
				throwFmt("conf: @import loop: %s -> %s", strings.Join(walking, " -> "), s.value)
			}

			walking = append(walking, s.value)
			walk(pm.confFile(s.value))
			walking = walking[:len(walking)-1]
		}
	}

	walk(root)

	needed := map[string]bool{sysinclVar: true, "__COMMA__": true, importMaskVariable: true, "DEFAULT_ALLOCATOR": true}
	used := make([]bool, len(stmts))
	need := func(name string) { needed[name] = true }

	for grown := true; grown; {
		grown = false

		for i, r := range stmts {
			if used[i] || !needed[r.stmt.name] {
				continue
			}

			used[i], grown = true, true
			confValueVars(r.stmt.value, need)
			r.file.condVars(r.stmt.branch, need)
		}
	}

	env := platformEnv.clone()

	for name := range needed {
		env.unbind(internEnv(name))
	}

	for i, r := range stmts {
		if used[i] && r.stmt.branch == 0 {
			applyConfStmt(env, r.stmt, false)
		}
	}

	for i, r := range stmts {
		if used[i] && r.stmt.branch != 0 && r.file.holds(r.stmt.branch, env) {
			applyConfStmt(env, r.stmt, true)
		}
	}

	bindConfCommandVariable("__COMMA__", env, platformEnv, map[string]bool{})
	bindConfCommandVariable(importMaskVariable, env, platformEnv, map[string]bool{})
	bindConfCommandVariable("DEFAULT_ALLOCATOR", env, platformEnv, map[string]bool{})

	return strings.Fields(expandConfScalar(env.string(envSYSINCL), env)), macros
}

func bindConfCommandVariable(name string, values, target Environment, seen map[string]bool) {
	if seen[name] {
		return
	}

	seen[name] = true

	key := internEnv(name)

	if values.hasBindingID(key) {
		value := values.string(key)

		target.setString(key, value)
		confValueVars(value, func(dependency string) { bindConfCommandVariable(dependency, values, target, seen) })
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

func extractPrefixedNegativeLookahead(pat string) (literal string, excludes []string, residual string, ok bool) {
	if !strings.HasPrefix(pat, "^") {
		return "", nil, "", false
	}

	body := pat[1:]
	i := strings.Index(body, "(?!")

	if i <= 0 {
		return "", nil, "", false
	}

	literal = body[:i]

	if containsRegexMeta(literal) {
		return "", nil, "", false
	}

	ex, res, _ := extractNegativeLookahead("^" + body[i:])

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
