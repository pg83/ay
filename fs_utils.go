package main

import (
	"path"
	"path/filepath"
	"strings"
)

func cleanRel(rel string) string {
	if rel == "" || rel == "." {
		return ""
	}

	if pathIsClean(rel) {
		return rel
	}

	rel = path.Clean(rel)

	if rel == "." || rel == "/" {
		return ""
	}

	rel = strings.TrimPrefix(rel, "/")
	rel = strings.TrimSuffix(rel, "/")

	return rel
}

func pathIsClean(p string) bool {
	if p[0] == '/' || p[len(p)-1] == '/' {
		return false
	}

	if p[0] == '.' {
		if len(p) == 1 || p[1] == '/' || (p[1] == '.' && (len(p) == 2 || p[2] == '/')) {
			return false
		}
	}

	for i := 0; i < len(p); i++ {
		if p[i] != '/' {
			continue
		}

		if p[i+1] == '/' {
			return false
		}

		if p[i+1] == '.' {
			if i+2 == len(p) || p[i+2] == '/' {
				return false
			}

			if p[i+2] == '.' && (i+3 == len(p) || p[i+3] == '/') {
				return false
			}
		}
	}

	return true
}

func splitDirName(rel string) (string, string) {
	i := strings.LastIndexByte(rel, '/')

	if i < 0 {
		return "", rel
	}

	return rel[:i], rel[i+1:]
}

func firstComponent(p string) (first string, more bool) {
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[:i], true
	}

	return p, false
}

func trimModulePrefix(rel, dir string) string {
	if relUnderDir(rel, dir) {
		return rel[len(dir)+1:]
	}

	return rel
}

func joinRel(prefix, suffix string) string {
	switch {
	case prefix == "":
		return suffix
	case suffix == "":
		return prefix
	default:
		return prefix + "/" + suffix
	}
}

func buildJoinClean(dir, rel string) VFS {
	if dir != "" && rel != "" && pathIsClean(dir) && pathIsClean(rel) {
		return build(dir, "/", rel)
	}

	return build(filepath.ToSlash(filepath.Clean(dir + "/" + rel)))
}

func sourceClean(rel string) VFS {
	if rel != "" && pathIsClean(rel) {
		return source(rel)
	}

	return source(filepath.ToSlash(filepath.Clean(rel)))
}

func buildClean(rel string) VFS {
	if rel != "" && pathIsClean(rel) {
		return build(rel)
	}

	return build(filepath.ToSlash(filepath.Clean(rel)))
}

func sourceJoinClean(dir, rel string) VFS {
	if dir != "" && rel != "" && pathIsClean(dir) && pathIsClean(rel) {
		return source(dir, "/", rel)
	}

	return source(filepath.ToSlash(filepath.Clean(dir + "/" + rel)))
}

func baseName(s string) string {
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		return s[i+1:]
	}

	return s
}

func pathDir(p string) string {
	idx := strings.LastIndexByte(p, '/')

	if idx < 0 {
		return ""
	}

	return p[:idx]
}

func fileExt(base string) string {
	if i := strings.LastIndexByte(base, '.'); i >= 0 {
		return base[i:]
	}

	return ""
}

func relStem(rel string) string {
	return strings.TrimSuffix(rel, filepath.Ext(rel))
}

func joinRelInto(dst []byte, a, b string) []byte {
	if a != "" {
		dst = append(dst, a...)
	}

	if a != "" && b != "" {
		dst = append(dst, '/')
	}

	if b != "" {
		dst = append(dst, b...)
	}

	return dst
}

func normaliseAppend(out []byte, p string) ([]byte, bool) {
	var starts [64]int

	base := len(out)
	room := cap(out) - base
	depth := 0

	for i := 0; i <= len(p); {
		j := i

		for j < len(p) && p[j] != '/' {
			j++
		}

		seg := p[i:j]

		i = j + 1

		switch seg {
		case "", ".":
		case "..":
			if depth > 0 {
				depth--
				out = out[:starts[depth]]
			}
		default:
			if len(seg) > room-(len(out)-base)-1 || depth == len(starts) {
				return out[:base], false
			}

			starts[depth] = len(out)
			depth++

			if len(out) > base {
				out = append(out, '/')
			}

			out = append(out, seg...)
		}
	}

	return out, true
}

func normalisePath(p string) string {
	if !strings.Contains(p, "..") && !strings.Contains(p, "./") && !strings.Contains(p, "//") {
		return p
	}

	var buf [256]byte

	out, ok := normaliseAppend(buf[:0], p)

	if !ok {
		return normalisePathSlow(p)
	}

	return string(out)
}

func normalisePathSlow(p string) string {
	parts := strings.Split(p, "/")
	out := make([]string, 0, len(parts))

	for _, seg := range parts {
		switch seg {
		case "", ".":

			continue
		case "..":
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
		default:
			out = append(out, seg)
		}
	}

	return strings.Join(out, "/")
}
