package main

import (
	"path"
	"strings"
)

// String-path helpers: pure functions over source-root-relative paths, no FS state.

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
