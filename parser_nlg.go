package main

import (
	"regexp"
	"strings"
)

var (
	nlgFromImport   = regexp.MustCompile(`^\s*(?:ext_)?from\s*` + nlgPathPattern + `\s*import\s+` + nlgTargetPattern + `(?:\s*,\s*` + nlgTargetPattern + `)*(?:\s+with\s+context)?\s*$`)
	nlgSimpleImport = regexp.MustCompile(`^\s*(?:ext_)?(?:import|nlgimport)\s*` + nlgModulePattern + `(?:\s*,\s*` + nlgModulePattern + `)*\s*$`)
	nlgImportPath   = regexp.MustCompile(`["']([a-zA-Z0-9_/]+\.nlg)["']`)
)

const (
	nlgPathPattern   = `(?:"[a-zA-Z0-9_/]+\.nlg"|'[a-zA-Z0-9_/]+\.nlg')`
	nlgTargetPattern = `[a-zA-Z0-9_]+`
	nlgModulePattern = nlgPathPattern + `(?:\s*as\s+` + nlgTargetPattern + `)?(?:\s+with\s+context)?`
)

type NlgIncludeDirectiveParser struct{}

func (NlgIncludeDirectiveParser) id() uint32 {
	return 13
}

func (NlgIncludeDirectiveParser) parse(_ string, data [][]byte, a *BumpAllocator[IncludeDirective]) ParsedIncludeSet {
	text := bytesString(concatChunks(data))

	var native, headers []IncludeDirective

	for len(text) > 0 {
		start := strings.IndexAny(text, "{#")

		if start < 0 {
			break
		}

		text = text[start:]

		if text[0] == '#' {
			end := strings.IndexByte(text, '\n')

			if end < 0 {
				break
			}

			text = text[end+1:]

			continue
		}

		if !strings.HasPrefix(text, "{%") {
			text = text[1:]

			continue
		}

		end := strings.Index(text[2:], "%}")
		nested := strings.IndexAny(text[2:], "{#")

		if nested >= 0 && (end < 0 || nested < end) {
			text = text[2+nested:]

			continue
		}

		if end < 0 {
			break
		}

		command := strings.TrimSpace(text[2 : 2+end])

		text = text[end+4:]

		if !nlgFromImport.MatchString(command) && !nlgSimpleImport.MatchString(command) {
			continue
		}

		external := strings.HasPrefix(command, "ext_")

		for _, match := range nlgImportPath.FindAllStringSubmatch(command, -1) {
			target := match[1]

			headers = append(headers, IncludeDirective{kind: includeSystem, target: includeTarget(internV(target, ".h").any())})

			if external {
				native = append(native, IncludeDirective{kind: includeSystem, target: includeTarget(internV(target, ".pb.txt").any())})
			}
		}
	}

	store := func(directives []IncludeDirective) []IncludeDirective {
		if len(directives) == 0 {
			return nil
		}

		block := a.alloc(len(directives))

		copy(block, directives)
		a.commit(len(directives))

		return block[:len(directives):len(directives)]
	}

	native = store(native)
	headers = store(headers)

	return ParsedIncludeSet{
		parsedIncludesLocal:  native,
		parsedIncludesAll:    native,
		parsedIncludesHeader: headers,
		parsedIncludesCpp:    headers,
	}
}
