package main

import (
	"bytes"
	"path"
	"regexp"
	"strings"
)

var cythonCimportPattern = regexp.MustCompile(`^[\t\v\f\r ]*(?:from[\t\v\f\r ]+(` + cythonImportName + `)[\t\v\f\r ]+)?cimport[\t\v\f\r ]+(` + cythonImportList + `|` + cythonImportParens + `)[\t\v\f\r ]*(?:#[^\n]*)?\n`)

const (
	cythonImportName   = `[^\t\v\f\r\n #,()\\]+`
	cythonImportItem   = cythonImportName + `(?:[\t\v\f\r ]+as[\t\v\f\r ]+` + cythonImportName + `)?`
	cythonImportWS     = `(?:[\t\v\f\r ]|\\\n)*`
	cythonImportList   = cythonImportItem + `(?:` + cythonImportWS + `,` + cythonImportWS + cythonImportItem + `)*`
	cythonImportParens = `\([\t\v\f\r\n ]*` + cythonImportItem + `(?:[\t\v\f\r\n ]*,[\t\v\f\r\n ]*` + cythonImportItem + `)*[\t\v\f\r\n ]*,?[\t\v\f\r\n ]*\)`
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

		match := cythonCimportPattern.FindSubmatchIndex(statement)

		if match == nil {
			continue
		}

		raw = statement[match[1]:]

		names := bytesString(statement[match[4]:match[5]])

		if from {
			addCythonCimportFrom(add, bytesString(statement[match[2]:match[3]]), names)
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
