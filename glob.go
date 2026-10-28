package main

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

var globSegmentMemo DenseMap[STR, *regexp.Regexp]

const (
	globFixed GlobPartKind = iota
	globPattern
	globAnything
	globRecursive
)

type GlobPartKind uint8

type GlobPart struct {
	kind  GlobPartKind
	fixed string
	re    *regexp.Regexp
}

func globSegmentMatcher(segment string) *regexp.Regexp {
	id := internStr(segment)

	if re, ok := globSegmentMemo.get(id); ok {
		return re
	}

	re := regexp.MustCompile("^" + globSegmentRegex(segment, true) + "$")

	globSegmentMemo.put(id, re)

	return re
}

func globParts(pattern string) []GlobPart {
	var parts []GlobPart
	var fixed []string

	flush := func() {
		if len(fixed) > 0 {
			parts = append(parts, GlobPart{kind: globFixed, fixed: strings.Join(fixed, "/")})
			fixed = fixed[:0]
		}
	}

	for _, seg := range strings.Split(pattern, "/") {
		switch {
		case seg == "":
		case seg == "*":
			flush()
			parts = append(parts, GlobPart{kind: globAnything})
		case seg == "**":
			flush()
			parts = append(parts, GlobPart{kind: globRecursive})
		case strings.ContainsAny(seg, "*?"):
			flush()
			parts = append(parts, GlobPart{kind: globPattern, re: globSegmentMatcher(seg)})
		default:
			fixed = append(fixed, seg)
		}
	}

	flush()

	return parts
}

func globSegmentRegex(seg string, single bool) string {
	var b strings.Builder

	for i := 0; i < len(seg); i++ {
		switch c := seg[i]; {
		case c == '*' && single:
			b.WriteString(".*")
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?' && single:
			b.WriteString(".")
		case c == '?':
			b.WriteString("[^/]")
		case c == '.':
			b.WriteString(`\.`)
		default:
			b.WriteByte(c)
		}
	}

	return b.String()
}

func globExcludeRegex(pattern string) *regexp.Regexp {
	var b strings.Builder

	b.WriteByte('^')

	needSep := false

	for _, seg := range strings.Split(pattern, "/") {
		if seg == "" {
			continue
		}

		if needSep {
			b.WriteByte('/')
		}

		needSep = true

		if seg == "**" {
			b.WriteString("(.*/)?")

			needSep = false

			continue
		}

		b.WriteString(globSegmentRegex(seg, false))
	}

	b.WriteByte('$')

	return regexp.MustCompile(b.String())
}

func globExcluded(excludes []*regexp.Regexp, rel string, isDir bool) bool {
	return slices.ContainsFunc(excludes, func(re *regexp.Regexp) bool {
		return re.MatchString(rel) || isDir && re.MatchString(rel+"/")
	})
}

func ymakeGlob(fs FS, pattern string, excludePatterns ...string) []string {
	excludes := make([]*regexp.Regexp, len(excludePatterns))

	for i, p := range excludePatterns {
		excludes[i] = globExcludeRegex(p)
	}

	parts := globParts(pattern)
	dirs := []string{""}

	var matches []string

	for pi, part := range parts {
		last := pi == len(parts)-1

		var next []string

		for _, dir := range dirs {
			switch part.kind {
			case globFixed:
				rel := joinRel(dir, part.fixed)
				present, isDir := fs.exists(srcRootRel, rel)

				switch {
				case !present:
				case isDir:
					next = append(next, rel)
				case last && !globExcluded(excludes, rel, false):
					matches = append(matches, rel)
				}
			case globPattern, globAnything:
				for _, packed := range fs.listdir(internStr(dir)).names {
					name, isDir := STR(packed>>1).string(), packed&1 != 0

					if !isDir && !last || part.kind == globPattern && !part.re.MatchString(name) {
						continue
					}

					rel := joinRel(dir, name)

					if isDir {
						next = append(next, rel)
					} else if !globExcluded(excludes, rel, false) {
						matches = append(matches, rel)
					}
				}
			case globRecursive:
				root := cleanRel(dir)

				fs.walk(root, func(rel string, isDir bool) bool {
					if rel == root {
						return true
					}

					if root != dir {
						rel = joinRel(dir, strings.TrimPrefix(rel, root+"/"))
					}

					if !isDir || globExcluded(excludes, rel, true) {
						return false
					}

					next = append(next, rel)

					return true
				})

				next = append(next, dir)
			}
		}

		dirs = next
	}

	return matches
}

func applyGlobStmt(fs FS, module string, args []ANY, env Environment, onWarn func(Warn)) {
	if len(args) == 0 {
		throwFmt("%s: empty argument in _GLOB", module)
	}

	markUserArg(args[0])

	var patterns, excludes, restrictions []string

	section := ""

	for _, arg := range anyStrs(args[1:]) {
		switch {
		case section == "RESTRICTIONS":
			restrictions = append(restrictions, arg)
		case arg == "RESTRICTIONS" || arg == "EXCLUDE":
			section = arg
		case section == "EXCLUDE":
			excludes = append(excludes, styleSourcePath(module, arg))
		default:
			patterns = append(patterns, styleSourcePath(module, arg))
		}
	}

	for i := 0; i < len(restrictions); i += 2 {
		switch restrictions[i] {
		case "MAX_MATCHES", "MAX_WATCH_DIRS", "SKIPPED_MIN_MATCHES", "SKIPPED_ERROR_PERCENT":
		default:
			onWarn(Warn{Kind: WarnBadMacroArgs, Message: fmt.Sprintf("%s: Unknown restriction name %s in _GLOB", module, restrictions[i])})
		}
	}

	var files []string

	for _, pattern := range patterns {
		for _, file := range ymakeGlob(fs, pattern, excludes...) {
			files = append(files, "${ARCADIA_ROOT}/"+file)
		}
	}

	env.setString(internEnv(args[0].string()), strings.Join(dedupKeepOrder(files), " "))
}
