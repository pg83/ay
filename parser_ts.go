package main

import (
	"path"
	"strings"
)

type TSIncludeDirectiveParser struct{}

func (TSIncludeDirectiveParser) parse(_ string, data [][]byte, a *BumpAllocator[IncludeDirective]) ParsedIncludeSet {
	b := concatChunksSentinel(data)
	s := bytesString(b[:len(b)-1])
	block := a.alloc(directiveBlockHint)
	k := 0
	ignore := false

	for i := 0; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], "//"):
			end := strings.IndexByte(s[i:], '\n')

			if end < 0 {
				end = len(s) - i
			}

			if strings.HasPrefix(strings.TrimSpace(s[i+2:i+end]), "nots-ignore-import") {
				ignore = true
			}

			i += end
		case strings.HasPrefix(s[i:], "/*"):
			end := strings.Index(s[i+2:], "*/")

			if end < 0 {
				i = len(s)
			} else {
				i += end + 4
			}
		case s[i] == '\'' || s[i] == '"' || s[i] == '`':
			quote := s[i]
			body := i + 1

			for i = body; i < len(s); i++ {
				if s[i] == quote && i > body && s[i-1] != '\\' {
					i++

					break
				}
			}
		case isParserIdentContinuation(s, i) || s[i] == '$':
			start := i

			for i < len(s) && (isParserIdentContinuation(s, i) || s[i] == '$') {
				i++
			}

			word := s[start:i]

			if start > 0 && s[start-1] == '.' || word != "import" && word != "export" && word != "require" {
				continue
			}

			end, qs, qe, ok := 0, 0, 0, false

			if word != "export" {
				end, qs, qe, ok = tsImportCallMatch(b, i)
			}

			if !ok && word != "require" {
				end, qs, qe, ok = tsImportDeclMatch(b, i)
			}

			if !ok {
				continue
			}

			target := s[qs+1 : qe-1]

			if !ignore && (strings.HasPrefix(target, "./") || strings.HasPrefix(target, "../") || target == "." || target == "..") {
				k = addDirective(block, k, IncludeDirective{kind: includeTypeScript, target: internStr(target).any()})
			}

			ignore = false
			i = end
		default:
			i++
		}
	}

	a.commit(k)

	return ParsedIncludeSet{parsedIncludesLocal: block[:k]}
}

func (pm *IncludeParserManager) resolveTSImport(dir, target string) VFS {
	base := path.Join(dir, target)

	probe := func(file string) VFS {
		if pm.fs.isFilePathClean(file) {
			return source(file)
		}

		return 0
	}

	if file := probe(base); file != 0 {
		return file
	}

	for _, candidate := range []string{base, path.Join(base, "index")} {
		for _, ext := range []string{"ts", "d.ts", "js", "tsx", "jsx"} {
			if file := probe(candidate + "." + ext); file != 0 {
				return file
			}
		}
	}

	if strings.HasSuffix(base, ".js") {
		for _, ext := range []string{".ts", ".tsx"} {
			if file := probe(strings.TrimSuffix(base, ".js") + ext); file != 0 {
				return file
			}
		}
	}

	return 0
}

func concatChunksSentinel(chunks [][]byte) []byte {
	n := 1

	for _, chunk := range chunks {
		n += len(chunk)
	}

	data := make([]byte, 0, n)

	for _, chunk := range chunks {
		data = append(data, chunk...)
	}

	return append(data, 0)
}
