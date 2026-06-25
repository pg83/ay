package main

import (
	"bytes"
	"path"
	"strings"
)

type CythonIncludeDirectiveParser struct{}

func (CythonIncludeDirectiveParser) id() uint32 {
	return 2
}

func (CythonIncludeDirectiveParser) parse(rel string, data []byte, a *BumpAllocator[IncludeDirective]) ParsedIncludeSet {
	block := a.alloc(directiveBlockHint)
	k := 0
	add := func(d IncludeDirective) {
		k = addDirective(block, k, d)
	}

	if strings.HasSuffix(rel, ".pyx") {
		add(IncludeDirective{kind: includeCythonSibling, target: internV(path.Base(rel[:len(rel)-len(".pyx")]), ".pxd")})
	}

	eachLine(data, func(line []byte) {
		t := bytes.TrimSpace(line)

		if len(t) == 0 || t[0] == '#' {
			return
		}

		if m := cythonIncludeRe.FindSubmatch(t); len(m) == 2 {
			add(IncludeDirective{kind: includeQuoted, target: internBytes(m[1])})

			return
		}

		if m := cythonExternFromRe.FindSubmatch(t); len(m) == 2 {
			target, kind, ok := parseDelimitedIncludeTarget(string(m[1]))

			if ok {
				add(IncludeDirective{kind: kind, target: internStr(target)})
			}

			return
		}

		if m := cythonCimportFromRe.FindSubmatch(t); len(m) == 3 {
			addCythonCimportFrom(add, string(m[1]), string(m[2]))

			return
		}

		if m := cythonCimportRe.FindSubmatch(t); len(m) == 2 {
			for _, part := range strings.Split(string(m[1]), ",") {
				part = strings.TrimSpace(part)

				if part == "" {
					continue
				}

				if idx := strings.IndexAny(part, " \t"); idx >= 0 {
					part = part[:idx]
				}

				addCythonPxdCandidates(add, strings.ReplaceAll(part, ".", "/"))
			}
		}
	})

	a.commit(k)

	if k == 0 {
		return ParsedIncludeSet{}
	}

	return ParsedIncludeSet{parsedIncludesLocal: block[:k:k]}
}

func addCythonPxdCandidates(add func(IncludeDirective), path string) {
	if path == "" {
		return
	}

	if first, _ := firstComponent(path); first == "cython" {
		return
	}

	add(IncludeDirective{kind: includeCythonOptional, target: internV(path, ".pxd")})
	add(IncludeDirective{kind: includeCythonFallback, target: internV(path, "/__init__.pxd")})
}

func addCythonCimportFrom(add func(IncludeDirective), module, names string) {
	searchPath, ok := cythonFromSearchPath(module)

	if !ok {
		return
	}

	if first, _ := firstComponent(searchPath); first == "cython" {
		return
	}

	emit := func(kind IncludeKind, rel string) {
		add(IncludeDirective{kind: kind, target: internStr(rel)})
	}

	if searchPath == "" {
		emit(includeCythonOptional, "__init__.pxd")
	} else {
		emit(includeCythonOptional, searchPath+"/__init__.pxd")
		emit(includeCythonModule, searchPath+".pxd")
	}

	for _, name := range parseCythonCimportNames(names) {
		base := name

		if searchPath != "" {
			base = searchPath + "/" + name
		}

		emit(includeCythonName, base+"/__init__.pxd")
		emit(includeCythonFallback, base+".pxd")
	}
}

func cythonFromSearchPath(module string) (string, bool) {
	dots := 0

	for dots < len(module) && module[dots] == '.' {
		dots++
	}

	rest := strings.ReplaceAll(module[dots:], ".", "/")

	if dots == 0 {
		return rest, true
	}

	return strings.Repeat("../", dots-1) + rest, true
}

func parseCythonCimportNames(names string) []string {
	if i := strings.IndexByte(names, '#'); i >= 0 {
		names = names[:i]
	}

	names = strings.TrimSpace(names)
	names = strings.TrimPrefix(names, "(")
	names = strings.TrimSuffix(names, ")")

	var out []string

	for _, part := range strings.Split(names, ",") {
		part = strings.TrimSpace(part)

		if idx := strings.IndexAny(part, " \t"); idx >= 0 {
			part = part[:idx]
		}

		if part == "" || part == "*" {
			continue
		}

		out = append(out, part)
	}

	return out
}
