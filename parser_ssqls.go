package main

import (
	"bytes"
	"encoding/xml"
	"io"
	"path/filepath"
	"strings"
)

type SsqlsIncludeDirectiveParser struct{}

type SsqlsInclude struct {
	Path string
	Text string
}

type SsqlsElement struct {
	Name     xml.Name
	Include  int
	SeenText bool
}

func (SsqlsIncludeDirectiveParser) parse(rel string, data [][]byte, a *BumpAllocator[IncludeDirective]) ParsedIncludeSet {
	decoder := xml.NewDecoder(bytes.NewReader(concatChunks(data)))

	var includes []SsqlsInclude
	var ancestors []string
	var stack []SsqlsElement

	root := false

	for {
		token, err := decoder.RawToken()

		if err == io.EOF {
			break
		}

		throw(err)

		switch element := token.(type) {
		case xml.StartElement:
			root = true

			frame := SsqlsElement{Name: element.Name, Include: -1}
			path := ""

			for _, attr := range element.Attr {
				if attr.Name == (xml.Name{Local: "path"}) {
					path = attr.Value
				}
			}

			if element.Name == (xml.Name{Local: "include"}) {
				frame.Include = len(includes)
				includes = append(includes, SsqlsInclude{Path: path})
			}

			if element.Name == (xml.Name{Local: "ancestor"}) && len(stack) > 0 && stack[len(stack)-1].Name == (xml.Name{Local: "ancestors"}) && path != "" {
				ancestors = append(ancestors, path)
			}

			stack = append(stack, frame)
		case xml.EndElement:
			if len(stack) == 0 || stack[len(stack)-1].Name != element.Name {
				throwFmt("malformed SSQLS XML in %s: unexpected closing element %s", rel, element.Name.Local)
			}

			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 0 || len(bytes.TrimSpace(element)) == 0 {
				continue
			}

			frame := &stack[len(stack)-1]

			if frame.Include >= 0 && !frame.SeenText {
				includes[frame.Include].Text = string(element)
				frame.SeenText = true
			}
		}
	}

	if !root || len(stack) != 0 {
		throwFmt("malformed SSQLS XML in %s: incomplete document", rel)
	}

	var xmls, headers []string

	for _, include := range includes {
		if include.Path != "" {
			xmls = append(xmls, include.Path)
		}

		if header := strings.Trim(include.Text, "<>\""); header != "" {
			headers = append(headers, header)
		}
	}

	for _, ancestor := range ancestors {
		xmls = append(xmls, ancestor)
		headers = append(headers, strings.TrimSuffix(ancestor, filepath.Ext(ancestor))+".h")
	}

	directives := func(paths []string) []IncludeDirective {
		out := a.alloc(len(paths))[:len(paths):len(paths)]

		for i, path := range paths {
			target := internStr(path).any()

			switch {
			case strings.HasPrefix(path, "$S/"):
				target = sourceClean(path[3:]).any()
			case strings.HasPrefix(path, "$B/"):
				target = buildClean(path[3:]).any()
			}

			out[i] = IncludeDirective{kind: includeQuoted, target: includeTarget(target)}
		}

		a.commit(len(out))

		return out
	}

	cpp := a.alloc(1)[:1:1]

	cpp[0] = IncludeDirective{kind: includeQuoted, target: includeTarget(build(strings.TrimSuffix(rel, ".ssqls"), ".h").any())}
	a.commit(1)

	return ParsedIncludeSet{parsedIncludesLocal: directives(xmls), parsedIncludesHeader: directives(headers), parsedIncludesCpp: cpp}
}
