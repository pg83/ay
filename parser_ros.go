package main

import (
	"regexp"
	"strings"
)

var (
	rosEnumPattern  = regexp.MustCompile(`^enum\s+(\w+)\s*(\w+)?$`)
	rosFieldPattern = regexp.MustCompile(`^(?:Optional<([\w/]+)(?:\[\d*\])?>|([\w/]+))(?:\[\d*\])?\s+\w+$`)
)

type RosIncludeDirectiveParser struct{}

func (RosIncludeDirectiveParser) parse(_ string, data [][]byte, a *BumpAllocator[IncludeDirective]) ParsedIncludeSet {
	block := a.alloc(directiveBlockHint)
	k := 0
	enums := map[string]bool{}

	eachLine(data, func(line []byte) {
		text, _, _ := strings.Cut(bytesString(line), "#")

		text = strings.TrimSpace(text)

		if enum := rosEnumPattern.FindStringSubmatch(text); enum != nil {
			enums[enum[1]] = true

			return
		}

		fields := rosFieldPattern.FindStringSubmatch(text)

		if fields == nil {
			return
		}

		name := fields[1]

		if name == "" {
			name = fields[2]
		}

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
