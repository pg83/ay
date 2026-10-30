package main

import (
	"bytes"
	"path"
	"strings"
)

type CythonIncludeDirectiveParser struct{}

func (pm *IncludeParserManager) cythonOutputIncludes(set ParsedIncludeSet) ParsedIncludeSet {
	local := pm.cache.directives.alloc(len(set[parsedIncludesLocal]))[:0]

	var cpp []IncludeDirective

	for _, directive := range set[parsedIncludesLocal] {
		parser, _ := pm.registry.matcher.match(directive.target.string())
		cLanguage := false

		switch parser.(type) {
		case CIncludeDirectiveParser, LexIncludeDirectiveParser, FortranIncludeDirectiveParser, GoCgoIncludeDirectiveParser:
			cLanguage = true
		}

		if !cLanguage {
			local = append(local, directive)
		}

		if parser != cythonParser {
			cpp = append(cpp, directive)
		}
	}

	pm.cache.directives.commit(len(local))
	set[parsedIncludesLocal] = local[:len(local):len(local)]

	if len(cpp) > 0 {
		stored := pm.cache.directives.alloc(len(set[parsedIncludesCpp]) + len(cpp))
		k := copy(stored, set[parsedIncludesCpp])

		k += copy(stored[k:], cpp)
		pm.cache.directives.commit(k)
		set[parsedIncludesCpp] = stored[:k:k]
	}

	return set
}

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

	raw := concatChunks(data)

	for len(raw) > 0 {
		line, rest, newline := bytes.Cut(raw, []byte{'\n'})
		t := trimParserSpace(line)
		statement := raw

		raw = rest

		if len(t) == 0 || t[0] == '#' {
			continue
		}

		if !newline {
			break
		}

		if rest, ok := cutParserKeyword(t, "include"); ok && (rest[0] == '"' || rest[0] == '\'') {
			if target, kind, ok := parseDelimitedIncludeTarget(rest); ok {
				add(IncludeDirective{kind: kind, target: includeTarget(internBytes(target).any())})
			}

			continue
		}

		if rest, ok := cutParserKeyword(t, "cdef"); ok {
			if rest, ok = cutParserKeyword(rest, "extern"); ok {
				if rest, ok = cutParserKeyword(rest, "from"); ok {
					if target, ok := parseCythonCdefTarget(rest); ok {
						cpp = append(cpp, IncludeDirective{kind: includeSystem, target: includeTarget(internBytes(target).any())})
					}
				}
			}

			continue
		}

		_, from := cutParserKeyword(t, "from")
		_, direct := cutParserKeyword(t, "cimport")

		if !from && !direct {
			continue
		}

		end, module, list, ok := cythonCimport(statement)

		if !ok {
			continue
		}

		raw = statement[end:]

		names := bytesString(list)

		if from {
			addCythonCimportFrom(add, bytesString(module), names)
		} else {
			eachCythonCimportName(names, func(name string) { addCythonPxdCandidates(add, name) })
		}
	}

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
	names = strings.ReplaceAll(names, "\\\n", "")
	names = strings.TrimSpace(names)
	names = strings.TrimPrefix(names, "(")
	names = strings.TrimSuffix(names, ")")

	for _, part := range strings.Split(names, ",") {
		part = strings.TrimSpace(part)

		if idx := strings.IndexAny(part, " \t\v\f\r\n"); idx >= 0 {
			part = part[:idx]
		}

		if part == "" || part == "*" {
			continue
		}

		fn(strings.ReplaceAll(part, ".", "/"))
	}
}

func cythonCimport(b []byte) (end int, module, names []byte, ok bool) {
	i := cythonSkipSpace(b, 0)

	if bytes.HasPrefix(b[i:], []byte("from")) {
		j := cythonSkipSpace(b, i+4)

		k := cythonNameEnd(b, j)

		if k == j {
			return
		}

		module = b[j:k]
		i = cythonSkipSpace(b, k)

		if i == k {
			return
		}
	}

	if !bytes.HasPrefix(b[i:], []byte("cimport")) {
		return
	}

	j := cythonSkipSpace(b, i+7)

	if j == i+7 {
		return
	}

	start := j

	if j < len(b) && b[j] == '(' {
		j, ok = cythonParenList(b, j+1)
	} else {
		j, ok = cythonPlainList(b, j)
	}

	if !ok {
		return 0, nil, nil, false
	}

	names = b[start:j]
	j = cythonSkipSpace(b, j)

	if j < len(b) && b[j] == '#' {
		for j < len(b) && b[j] != '\n' {
			j++
		}
	}

	if j >= len(b) || b[j] != '\n' {
		return 0, nil, nil, false
	}

	return j + 1, module, names, true
}

func cythonPlainList(b []byte, i int) (int, bool) {
	i, ok := cythonItem(b, i)

	if !ok {
		return i, false
	}

	for {
		j := cythonSkipContinued(b, i)

		if j >= len(b) || b[j] != ',' {
			return i, true
		}

		if i, ok = cythonItem(b, cythonSkipContinued(b, j+1)); !ok {
			return i, false
		}
	}
}

func cythonParenList(b []byte, i int) (int, bool) {
	i, ok := cythonItem(b, cythonSkipSpaceLines(b, i))

	if !ok {
		return i, false
	}

	for {
		j := cythonSkipSpaceLines(b, i)

		if j < len(b) && b[j] == ',' {
			k := cythonSkipSpaceLines(b, j+1)

			if m, ok := cythonItem(b, k); ok {
				i = m

				continue
			}

			j = k
		}

		if j < len(b) && b[j] == ')' {
			return j + 1, true
		}

		return j, false
	}
}

func cythonItem(b []byte, i int) (int, bool) {
	k := cythonNameEnd(b, i)

	if k == i {
		return i, false
	}

	if j := cythonSkipSpace(b, k); j > k && bytes.HasPrefix(b[j:], []byte("as")) {
		if m := cythonSkipSpace(b, j+2); m > j+2 {
			if n := cythonNameEnd(b, m); n > m {
				return n, true
			}
		}
	}

	return k, true
}

func cythonSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\v' || c == '\f' || c == '\r'
}

func cythonNameEnd(b []byte, i int) int {
	for i < len(b) {
		switch b[i] {
		case ' ', '\t', '\v', '\f', '\r', '\n', '#', ',', '(', ')', '\\':
			return i
		}

		i++
	}

	return i
}

func cythonSkipSpace(b []byte, i int) int {
	for i < len(b) && cythonSpace(b[i]) {
		i++
	}

	return i
}

func cythonSkipSpaceLines(b []byte, i int) int {
	for i < len(b) && (cythonSpace(b[i]) || b[i] == '\n') {
		i++
	}

	return i
}

func cythonSkipContinued(b []byte, i int) int {
	for i < len(b) {
		if cythonSpace(b[i]) {
			i++
		} else if b[i] == '\\' && i+1 < len(b) && b[i+1] == '\n' {
			i += 2
		} else {
			break
		}
	}

	return i
}
