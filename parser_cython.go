package main

import (
	"path"
	"strings"
)

type CythonIncludeDirectiveParser struct{}

func (CythonIncludeDirectiveParser) parse(rel string, data [][]byte, a *BumpAllocator[IncludeDirective]) ParsedIncludeSet {
	block := a.alloc(directiveBlockHint)
	k := 0

	var cppScratch [8]IncludeDirective

	cpp := cppScratch[:0]

	add := func(d IncludeDirective) {
		k = addDirective(block, k, d)
	}

	if extIsPyx(rel) {
		add(IncludeDirective{kind: includeCythonSibling, target: includeTarget(internV(path.Base(rel[:len(rel)-len(".pyx")]), ".pxd").any())})
	}

	eachLine(data, func(line []byte) {
		t := trimParserSpace(line)

		if len(t) == 0 || t[0] == '#' {
			return
		}

		if rest, ok := cutParserKeyword(t, "include"); ok && (rest[0] == '"' || rest[0] == '\'') {
			if target, kind, ok := parseDelimitedIncludeTarget(rest); ok {
				add(IncludeDirective{kind: kind, target: includeTarget(internBytes(target).any())})
			}

			return
		}

		if rest, ok := cutParserKeyword(t, "cdef"); ok {
			if rest, ok = cutParserKeyword(rest, "extern"); ok {
				if rest, ok = cutParserKeyword(rest, "from"); ok {
					if target, ok := parseCythonCdefTarget(rest); ok {
						cpp = append(cpp, IncludeDirective{kind: includeSystem, target: includeTarget(internBytes(target).any())})
					}
				}
			}

			return
		}

		if rest, ok := cutParserKeyword(t, "from"); ok {
			moduleEnd := 0

			for moduleEnd < len(rest) && isCythonModuleByte(rest[moduleEnd]) {
				moduleEnd++
			}

			if moduleEnd > 0 {
				if names, ok := cutParserKeyword(trimParserSpace(rest[moduleEnd:]), "cimport"); ok {
					addCythonCimportFrom(add, bytesString(rest[:moduleEnd]), bytesString(names))
				}
			}

			return
		}

		if rest, ok := cutParserKeyword(t, "cimport"); ok {
			for len(rest) > 0 {
				part := rest

				if comma := strings.IndexByte(bytesString(part), ','); comma >= 0 {
					part = rest[:comma]
					rest = rest[comma+1:]
				} else {
					rest = nil
				}

				part = trimParserSpace(part)

				if len(part) == 0 {
					continue
				}

				partString := bytesString(part)

				if idx := strings.IndexAny(partString, " \t"); idx >= 0 {
					part = part[:idx]
					partString = bytesString(part)
				}

				addCythonPxdCandidates(add, strings.ReplaceAll(partString, ".", "/"))
			}
		}
	})

	a.commit(k)

	var set ParsedIncludeSet

	if k > 0 {
		set[parsedIncludesLocal] = block[:k:k]
	}

	if len(cpp) > 0 {
		stored := a.alloc(len(cpp))

		copy(stored, cpp)
		a.commit(len(cpp))
		set[parsedIncludesCpp] = stored[:len(cpp):len(cpp)]
	}

	return set
}

func parseCythonCdefTarget(b []byte) ([]byte, bool) {
	if len(b) > 0 && b[0] == 'r' {
		b = b[1:]
	}

	if len(b) == 0 || b[0] != '"' && b[0] != '\'' {
		return nil, false
	}

	quote := b[0]

	b = b[1:]

	end := 0

	if len(b) > 0 && b[0] == '<' {
		b = b[1:]

		for end < len(b) && b[end] != '<' && b[end] != '>' {
			end++
		}

		if end == len(b) || b[end] != '>' || end+1 == len(b) || b[end+1] != quote {
			return nil, false
		}
	} else {
		for end < len(b) && b[end] != quote && b[end] != '<' && b[end] != '>' {
			end++
		}

		if end == len(b) || b[end] != quote {
			return nil, false
		}
	}

	return b[:end], end > 0
}

func isCythonModuleByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '.'
}

func addCythonPxdCandidates(add func(IncludeDirective), path string) {
	if first, _ := firstComponent(path); first == "cython" {
		return
	}

	add(IncludeDirective{kind: includeCythonOptional, target: includeTarget(internV(path, ".pxd").any())})
	add(IncludeDirective{kind: includeCythonFallback, target: includeTarget(internV(path, "/__init__.pxd").any())})
	add(IncludeDirective{kind: includeCythonImportEnd, target: includeTarget(internV(path, ".pxd").any())})
}

func addCythonCimportFrom(add func(IncludeDirective), module, names string) {
	searchPath := cythonFromSearchPath(module)

	if first, _ := firstComponent(searchPath); first == "cython" {
		return
	}

	emit := func(kind IncludeKind, rel string) {
		add(IncludeDirective{kind: kind, target: includeTarget(internStr(rel).any())})
	}

	if searchPath == "" {
		emit(includeCythonOptional, "__init__.pxd")
	} else {
		emit(includeCythonOptional, searchPath+"/__init__.pxd")
		emit(includeCythonModule, searchPath+".pxd")
	}

	eachCythonCimportName(names, func(name string) {
		base := name

		if searchPath != "" {
			base = searchPath + "/" + name
		}

		emit(includeCythonName, base+"/__init__.pxd")
		emit(includeCythonFallback, base+".pxd")
	})

	if searchPath == "" {
		emit(includeCythonImportEnd, "__init__.pxd")
	} else {
		emit(includeCythonImportEnd, searchPath+".pxd")
	}
}

func cythonFromSearchPath(module string) string {
	dots := 0

	for dots < len(module) && module[dots] == '.' {
		dots++
	}

	rest := strings.ReplaceAll(module[dots:], ".", "/")

	if dots == 0 {
		return rest
	}

	return strings.Repeat("../", dots-1) + rest
}

func eachCythonCimportName(names string, fn func(string)) {
	if i := strings.IndexByte(names, '#'); i >= 0 {
		names = names[:i]
	}

	names = strings.TrimSpace(names)
	names = strings.TrimPrefix(names, "(")
	names = strings.TrimSuffix(names, ")")

	for _, part := range strings.Split(names, ",") {
		part = strings.TrimSpace(part)

		if idx := strings.IndexAny(part, " \t"); idx >= 0 {
			part = part[:idx]
		}

		if part == "" || part == "*" {
			continue
		}

		fn(part)
	}
}
