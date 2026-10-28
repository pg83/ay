package main

import (
	"path"
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
	re   *globMatcher
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
			parts = append(parts, javaGlobPart{kind: javaGlobPattern, re: globSegmentMatcher(p)})
		default:
			fixed = append(fixed, p)
		}
	}

	flush()

	return parts
}

type javaExcludeMatcher struct {
	res []*globMatcher
}

func newJavaExcludeMatcher(moddir string, patterns []string) *javaExcludeMatcher {
	m := &javaExcludeMatcher{}

	for _, p := range patterns {
		if rel, ok := strings.CutPrefix(p, "$(S)/"); ok {
			p = rel
		} else {
			p = moddir + "/" + p
		}

		m.res = append(m.res, globExcludeMatcher(p))
	}

	return m
}

func (m *javaExcludeMatcher) excluded(rel string, isDir bool) bool {
	for _, re := range m.res {
		if re.match(rel) || isDir && re.match(rel+"/") {
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

					if part.kind == javaGlobPattern && !part.re.match(javaPathBasename(e.path)) {
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
