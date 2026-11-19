package main

import (
	"slices"
	"strconv"
	"strings"
)

var (
	globSegmentMemo = newSparseMap[STR, *globMatcher](256)
	globExcludeMemo = newSparseMap[STR, *globMatcher](64)
	globPartsMemo   = newSparseMap[STR, []GlobPart](64)
)

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
	re    *globMatcher
}

func globSegmentMatcher(segment string) *globMatcher {
	id := internStr(segment)

	if re, ok := globSegmentMemo.get(id); ok {
		return re
	}

	re := newGlobMatcher("^" + globSegmentRegex(segment, true) + "$")

	globSegmentMemo.put(id, re)

	return re
}

func globParts(pattern string) []GlobPart {
	id := internStr(pattern)

	if parts, ok := globPartsMemo.get(id); ok {
		return parts
	}

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

	globPartsMemo.put(id, parts)

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

func globExcludeMatcher(pattern string) *globMatcher {
	id := internStr(pattern)

	if matcher, ok := globExcludeMemo.get(id); ok {
		return matcher
	}

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

	matcher := newGlobMatcher(b.String())

	globExcludeMemo.put(id, matcher)

	return matcher
}

func globExcluded(excludes []*globMatcher, rel string, isDir bool) bool {
	return slices.ContainsFunc(excludes, func(re *globMatcher) bool {
		return re.match(rel) || isDir && re.match(rel+"/")
	})
}

func ymakeGlob(fs FS, pattern string, excludePatterns ...string) []string {
	ids := ymakeGlobIDs(fs, pattern, excludePatterns...)
	matches := make([]string, len(ids))

	for i, id := range ids {
		matches[i] = id.string()
	}

	return matches
}

func ymakeGlobIDs(fs FS, pattern string, excludePatterns ...string) []STR {
	return ymakeGlobIDsStat(fs, pattern, nil, excludePatterns...)
}

type globStat struct {
	matched int
	watched map[STR]bool
}

func (s *globStat) watch(dir STR) {
	if s != nil {
		s.watched[dir] = true
	}
}

func ymakeGlobIDsStat(fs FS, pattern string, stat *globStat, excludePatterns ...string) []STR {
	excludes := make([]*globMatcher, len(excludePatterns))

	for i, p := range excludePatterns {
		excludes[i] = globExcludeMatcher(p)
	}

	parts := globParts(pattern)
	dirs := []globDir{{id: strEmpty}}

	var matches []STR

	for pi, part := range parts {
		last := pi == len(parts)-1

		var next []globDir

		for _, d := range dirs {
			dir := d.text

			switch part.kind {
			case globFixed:
				relID := internJoined(dir, part.fixed)
				rel := relID.string()
				present, isDir := fs.exists(srcRootRel, rel)

				if stat != nil && (last || !present) {
					stat.watch(internStr(pathDir(rel)))
				}

				switch {
				case !present:
				case isDir:
					next = append(next, globDir{rel, relID})
				case last && !globExcluded(excludes, rel, false):
					matches = append(matches, relID)
				}
			case globPattern, globAnything:
				stat.watch(d.key())

				for _, packed := range fs.listdir(d.key()).names {
					name, isDir := STR(packed>>1).string(), packed&1 != 0

					if !isDir && !last || part.kind == globPattern && !part.re.match(name) {
						continue
					}

					relID := internJoined(dir, name)
					rel := relID.string()

					if isDir {
						next = append(next, globDir{rel, relID})
					} else if !globExcluded(excludes, rel, false) {
						matches = append(matches, relID)
					}
				}
			case globRecursive:
				root := cleanRel(dir)

				if root == dir && d.id != 0 {
					fs.walkDirID(d.id, func(rel string, id STR, isDir bool) bool {
						if id == d.id {
							return true
						}

						if !isDir || globExcluded(excludes, rel, true) {
							return false
						}

						next = append(next, globDir{rel, id})

						return true
					})
				} else {
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

						next = append(next, globDir{text: rel})

						return true
					})
				}

				next = append(next, d)

				if stat != nil {
					for _, n := range next {
						stat.watch(n.key())
					}
				}
			}
		}

		if stat != nil && !last && (part.kind == globPattern || part.kind == globAnything) {
			for _, n := range next {
				stat.watch(n.key())
			}
		}

		dirs = next
	}

	if stat != nil {
		stat.matched += len(matches)
	}

	return matches
}

type globDir struct {
	text string
	id   STR
}

func (d globDir) key() STR {
	if d.id != 0 {
		return d.id
	}

	return internStr(d.text)
}

func applyGlobStmt(fs FS, module string, name TOK, args []ANY, env Environment, onWarn func(Warn)) {
	macro := name.string()

	report := func(msg string) {
		onWarn(Warn{Kind: WarnMisconfiguration, Where: makeFileWhere(module), Error: msg})
	}

	if len(args) == 0 {
		report("empty argument in [[alt1]]" + macro + "[[rst]]")

		return
	}

	markUserArg(args[0])

	var patterns, written, excludes, restrictions []string

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
			written = append(written, arg)
			patterns = append(patterns, styleSourcePath(module, arg))
		}
	}

	limits := parseGlobRestrictions(restrictions, macro, report)

	if slices.Contains(strings.Fields(env.string(envModulesWithExtendedGlobRestrictions)), module) {
		limits.maxMatches *= 10
		limits.maxWatchDirs *= 10
	}

	stat := &globStat{watched: map[STR]bool{}}

	var files []string

	for i, pattern := range patterns {
		if msg := globPatternError(written[i]); msg != "" {
			report("Invalid pattern in [[alt1]]" + macro + "[[rst]]: " + msg)

			continue
		}

		for _, id := range ymakeGlobIDsStat(fs, pattern, stat, excludes...) {
			files = append(files, "${ARCADIA_ROOT}/"+id.string())
		}
	}

	if len(stat.watched) == 0 {
		stat.watched[internStr(module)] = true
	}

	var violated []string

	if stat.matched >= limits.maxMatches {
		violated = append(violated, "too many matched files - "+strconv.Itoa(stat.matched))
	}

	if len(stat.watched) >= limits.maxWatchDirs {
		violated = append(violated, "too many watched dirs - "+strconv.Itoa(len(stat.watched)))
	}

	if len(violated) != 0 {
		report("Glob restrictions violated in [[alt1]]" + macro + "[[rst]] (" + strings.Join(written, ", ") + "): " + strings.Join(violated, ", "))
	}

	env.setString(internEnv(args[0].string()), strings.Join(dedupKeepOrder(files), " "))
}

type globLimits struct {
	maxMatches, maxWatchDirs int
}

func parseGlobRestrictions(r []string, macro string, report func(string)) globLimits {
	limits := globLimits{maxMatches: 10000, maxWatchDirs: 5000}

	for i := 0; i < len(r); i++ {
		parse := func(name string, minValue, maxValue int) int {
			if r[i] != name {
				return -1
			}

			if i+1 == len(r) {
				report("No value for " + name + " in " + macro)

				return -2
			}

			i++

			v, err := strconv.Atoi(r[i])

			if err != nil || v < minValue || v > maxValue {
				report("Invalid value '" + r[i] + "' for " + name + " in " + macro)

				return -3
			}

			return v
		}

		maxMatches := parse("MAX_MATCHES", 1, 1000000)
		maxWatchDirs := parse("MAX_WATCH_DIRS", 1, 1000000)
		skippedMin := parse("SKIPPED_MIN_MATCHES", 1, 1000000)
		skippedPercent := parse("SKIPPED_ERROR_PERCENT", 0, 99)

		if maxMatches > 0 {
			limits.maxMatches = maxMatches
		}

		if maxWatchDirs > 0 {
			limits.maxWatchDirs = maxWatchDirs
		}

		if maxMatches == -1 && maxWatchDirs == -1 && skippedMin == -1 && skippedPercent == -1 {
			report("Unknown restriction name " + r[i] + " in " + macro)
		}
	}

	return limits
}

func globPatternError(pattern string) string {
	recursive := false

	for _, part := range strings.Split(pattern, "/") {
		switch {
		case part == "**":
			if recursive {
				return "only one recursive part is allowed"
			}

			recursive = true
		case strings.Contains(part, "**"):
			return "recursive part in pattern is not allowed"
		}
	}

	return ""
}
