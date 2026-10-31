package main

import "strings"

type NlgIncludeDirectiveParser struct{}

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

		command := strings.Trim(text[2:2+end], " \t\v\f\r\n")

		text = text[end+4:]

		if !nlgImportStatement(command) {
			continue
		}

		external := strings.HasPrefix(command, "ext_")

		for pos := 0; ; {
			quote := strings.IndexAny(command[pos:], `"'`)

			if quote < 0 {
				break
			}

			ps := pos + quote + 1
			pe := ps + strings.IndexByte(command[ps:], command[ps-1])
			target := command[ps:pe]

			pos = pe + 1
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
