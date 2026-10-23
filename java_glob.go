package main

import (
	"path"
	"regexp"
	"sort"
	"strings"
)

const (
	javaGlobFixed javaGlobPartKind = iota
	javaGlobPattern
	javaGlobAnything
	javaGlobRecursive
)

type javaGlobPartKind uint8

type javaGlobPart struct {
	kind javaGlobPartKind
	data string
	re   *regexp.Regexp
}

func javaGlobSegmentToRegex(b *strings.Builder, segment string, single bool) {
	for i := 0; i < len(segment); i++ {
		switch ch := segment[i]; ch {
		case '*':
			if single {
				b.WriteString(".*")
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			if single {
				b.WriteString(".")
			} else {
				b.WriteString("[^/]")
			}
		case '.':
			b.WriteString(`\.`)
		default:
			b.WriteByte(ch)
		}
	}
}

func parseJavaGlob(pattern string) []javaGlobPart {
	var parts []javaGlobPart
	var fixed []string

	flush := func() {
		if len(fixed) > 0 {
			parts = append(parts, javaGlobPart{kind: javaGlobFixed, data: strings.Join(fixed, "/")})
			fixed = nil
		}
	}

	for _, p := range strings.Split(pattern, "/") {
		switch {
		case p == "":
			continue
		case p == "*":
			flush()
			parts = append(parts, javaGlobPart{kind: javaGlobAnything})
		case p == "**":
			flush()
			parts = append(parts, javaGlobPart{kind: javaGlobRecursive})
		case strings.ContainsAny(p, "*?"):
			flush()

			var b strings.Builder

			b.WriteByte('^')
			javaGlobSegmentToRegex(&b, p, true)
			b.WriteByte('$')

			parts = append(parts, javaGlobPart{kind: javaGlobPattern, re: regexp.MustCompile(b.String())})
		default:
			fixed = append(fixed, p)
		}
	}

	flush()

	return parts
}

type javaExcludeMatcher struct {
	res []*regexp.Regexp
}

func javaExcludeRegex(pattern string) *regexp.Regexp {
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

		javaGlobSegmentToRegex(&b, seg, false)
	}

	b.WriteByte('$')

	return regexp.MustCompile(b.String())
}

func newJavaExcludeMatcher(moddir string, patterns []string) *javaExcludeMatcher {
	m := &javaExcludeMatcher{}

	for _, p := range patterns {
		if rel, ok := strings.CutPrefix(p, "$(S)/"); ok {
			p = rel
		} else {
			p = moddir + "/" + p
		}

		m.res = append(m.res, javaExcludeRegex(p))
	}

	return m
}

func (m *javaExcludeMatcher) excluded(rel string, isDir bool) bool {
	for _, re := range m.res {
		if re.MatchString(rel) || isDir && re.MatchString(rel+"/") {
			return true
		}
	}

	return false
}

func (ctx *GenCtx) javaListAll(dir string) []javaDirEntry {
	view := ctx.fs.listdir(dirKey(dir))
	out := make([]javaDirEntry, 0, len(view.names))

	for _, packed := range view.names {
		name := STR(packed >> 1).string()
		p := name

		if dir != "" {
			p = dir + "/" + name
		}

		out = append(out, javaDirEntry{path: p, isDir: packed&1 != 0})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })

	return out
}

type javaDirEntry struct {
	path  string
	isDir bool
}

func (ctx *GenCtx) javaGlob(moddir, pattern string, ex *javaExcludeMatcher) []string {
	root := moddir

	if rel, ok := strings.CutPrefix(pattern, "$(S)/"); ok {
		root = ""
		pattern = rel
	} else if strings.HasPrefix(pattern, "$") {
		return nil
	}

	parts := parseJavaGlob(pattern)

	var matches []string

	dirs := []string{root}

	for i, part := range parts {
		last := i == len(parts)-1

		var next []string

		for _, dir := range dirs {
			switch part.kind {
			case javaGlobFixed:
				p := part.data

				if dir != "" {
					p = dir + "/" + p
				}

				p = path.Clean(p)

				if p == "." {
					p = ""
				}

				present, isDir := ctx.fs.exists(srcRootRel, p)

				switch {
				case !present:
				case isDir:
					next = append(next, p)
				case last && !ex.excluded(p, false):
					matches = append(matches, p)
				}
			case javaGlobPattern, javaGlobAnything:
				for _, e := range ctx.javaListAll(dir) {
					if !e.isDir && !last {
						continue
					}

					if part.kind == javaGlobPattern && !part.re.MatchString(javaPathBasename(e.path)) {
						continue
					}

					switch {
					case e.isDir:
						next = append(next, e.path)
					case !ex.excluded(e.path, false):
						matches = append(matches, e.path)
					}
				}
			case javaGlobRecursive:
				ctx.fs.walk(dir, func(rel string, isDir bool) bool {
					if rel == dir {
						return true
					}

					if !isDir || ex.excluded(rel, true) {
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

	sort.Strings(matches)

	return matches
}
