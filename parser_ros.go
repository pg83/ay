package main

import "strings"

type RosIncludeDirectiveParser struct{}

func (RosIncludeDirectiveParser) parse(_ string, data [][]byte, a *BumpAllocator[IncludeDirective]) ParsedIncludeSet {
	block := a.alloc(directiveBlockHint)
	k := 0
	enums := map[string]bool{}

	eachLine(data, func(line []byte) {
		text, _, _ := strings.Cut(bytesString(line), "#")

		text = strings.TrimSpace(text)

		if ns, ne, ok := rosEnumMatch(text); ok {
			enums[text[ns:ne]] = true

			return
		}

		ts, te, ok := rosFieldMatch(text)

		if !ok {
			return
		}

		name := text[ts:te]

		if enums[name] {
			return
		}

		switch name {
		case "int8", "int16", "int32", "int64", "uint8", "uint16", "uint32", "uint64", "float32", "float64", "bool", "string", "time", "duration", "byte", "char":
			return
		case "Header":
			name = "std_msgs/Header"
		}

		k = addDirective(block, k, IncludeDirective{kind: includeRos, target: includeTarget(internStr(name).any())})
	})

	a.commit(k)

	return ParsedIncludeSet{parsedIncludesLocal: block[:k:k]}
}
