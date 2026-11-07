package main

import (
	"path"
	"slices"
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
	res     []*globMatcher
	slashed []byte
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
		if re.match(rel) {
			return true
		}

		if isDir {
			m.slashed = append(append(m.slashed[:0], rel...), '/')

			if re.match(bytesString(m.slashed)) {
				return true
			}
		}
	}

	return false
}

func (ctx *GenCtx) javaGlob(moddir, pattern string, ex *javaExcludeMatcher) []STR {
	root := moddir

	if rel, ok := strings.CutPrefix(pattern, "$(S)/"); ok {
		root = ""
		pattern = rel
	} else if strings.HasPrefix(pattern, "$") {
		return nil
	}

	parts := parseJavaGlob(pattern)

	var matches []STR
	var joinBuf [256]byte

	dirs := []STR{dirKey(root)}

	for i, part := range parts {
		last := i == len(parts)-1

		var next []STR

		for _, dirID := range dirs {
			dir := dirID.string()

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
					next = append(next, internStr(p))
				case last && !ex.excluded(p, false):
					matches = append(matches, internStr(p))
				}
			case javaGlobPattern, javaGlobAnything:
				for _, packed := range ctx.fs.listdir(dirID).names {
					isDir := packed&1 != 0

					if !isDir && !last {
						continue
					}

					nameID := STR(packed >> 1)
					name := nameID.string()

					if part.kind == javaGlobPattern && !part.re.match(name) || isDir && last {
						continue
					}

					p := name

					if dir != "" {
						p = bytesString(append(append(append(joinBuf[:0], dir...), '/'), name...))
					}

					switch {
					case isDir && dir == "":
						next = append(next, nameID)
					case isDir:
						next = append(next, internStr(p))
					case ex.excluded(p, false):
					case dir == "":
						matches = append(matches, nameID)
					default:
						matches = append(matches, internStr(p))
					}
				}
			case javaGlobRecursive:
				ctx.fs.walkDirID(dirID, func(rel string, id STR, isDir bool) bool {
					if id == dirID {
						return true
					}

					if !isDir || ex.excluded(rel, true) {
						return false
					}

					next = append(next, id)

					return true
				})

				next = append(next, dirID)
			}
		}

		dirs = next
	}

	keys := ctx.javaGlobKeys[:0]

	for _, m := range matches {
		keys = append(keys, javaGlobKey{text: m.string(), id: m})
	}

	slices.SortFunc(keys, func(a, b javaGlobKey) int { return strings.Compare(a.text, b.text) })

	for i, k := range keys {
		matches[i] = k.id
	}

	ctx.javaGlobKeys = keys[:0]

	return matches
}

type javaGlobKey struct {
	text string
	id   STR
}
