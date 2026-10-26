package main

import "strings"

type TypedInclude struct {
	Type    STR
	Include IncludeDirective
}

func preparedInducedIncludes(args []ANY) []TypedInclude {
	var includes []TypedInclude

	text := strings.Join(anyStrs(args), " ")

	for text = strings.TrimSpace(text); text != ""; text = strings.TrimSpace(text) {
		body, ok := strings.CutPrefix(text, "$_FMT_INDUCED_DEPS(")

		if !ok {
			throwFmt("INDUCED_DEPS expects a value produced by PREPARE_INDUCED_DEPS: %s", text)
		}

		end, depth := 0, 1

		for end < len(body) && depth > 0 {
			switch body[end] {
			case '(':
				depth++
			case ')':
				depth--
			}

			end++
		}

		if depth != 0 {
			throwFmt("invalid _FMT_INDUCED_DEPS call: %s", text)
		}

		words := strings.Fields(body[:end-1])

		if len(words) == 0 {
			throwFmt("_FMT_INDUCED_DEPS expects a dependency type")
		}

		kind := internStr(words[0])

		for _, file := range words[1:] {
			includes = append(includes, TypedInclude{Type: kind, Include: IncludeDirective{kind: includeQuoted, target: includeTarget(internAny(file))}})
		}

		text = body[end:]
	}

	return includes
}

func (s *IncludeScanner) usesInducedType(file VFS, kind STR) bool {
	parser, _ := s.parsers.registry.registeredParserFor(file.relString())
	name := kind.string()

	switch parser.(type) {
	case CIncludeDirectiveParser:
		if isHeaderSource(file.relString()) {
			return name == "h" || name == "h+cpp"
		}

		return name == "cpp" || name == "h+cpp" || name == "xscpp"
	case CythonIncludeDirectiveParser:
		return name == "pyx"
	case ProtoIncludeDirectiveParser, CfgProtoIncludeDirectiveParser:
		return name == "proto"
	case GoCgoIncludeDirectiveParser:
		return name == "go"
	}

	return false
}

func (s *IncludeScanner) inheritInducedIncludes(output VFS, own []TypedInclude, inputs []VFS) {
	var inherited []TypedInclude

	for _, input := range inputs {
		for _, include := range s.codegen.inducedIncludes[input] {
			if !s.usesInducedType(input, include.Type) {
				inherited = append(inherited, include)
			}
		}
	}

	includes := own

	if len(inherited) > 0 {
		includes = append(inherited, own...)
	}

	if len(includes) == 0 {
		return
	}

	if s.codegen.inducedIncludes == nil {
		s.codegen.inducedIncludes = make(map[VFS][]TypedInclude)
	}

	s.codegen.inducedIncludes[output] = includes
}
