package main

import (
	"regexp"
	"slices"
	"strings"
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
	re    *regexp.Regexp
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
			parts = append(parts, GlobPart{kind: globPattern, re: regexp.MustCompile("^" + globSegmentRegex(seg, true) + "$")})
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
				for queue := []string{dir}; len(queue) > 0; queue = queue[1:] {
					for _, packed := range fs.listdir(internStr(queue[0])).names {
						if packed&1 == 0 {
							continue
						}

						rel := joinRel(queue[0], STR(packed>>1).string())

						if !globExcluded(excludes, rel, true) {
							next = append(next, rel)
							queue = append(queue, rel)
						}
					}
				}

				next = append(next, dir)
			}
		}

		dirs = next
	}

	return matches
}
