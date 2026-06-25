package main

import (
	"bytes"
	"regexp"
	"strings"
)

var (
	yasmIncludeRe           = regexp.MustCompile(`(?i)^\s*%\s*include\s*[<"]([^>"]+)[>"]`)
	swigIncludeRe           = regexp.MustCompile(`^%(include|import|insert\s*\([^\)]*\))\s*([<"].*?[">])`)
	cythonCimportFromRe     = regexp.MustCompile(`^\s*from\s+([A-Za-z0-9_\.]+)\s+cimport\s+(.+)$`)
	cythonCimportRe         = regexp.MustCompile(`^\s*cimport\s+(.+)$`)
	cythonIncludeRe         = regexp.MustCompile(`^\s*include\s+["']([^"']+)["']`)
	cythonExternFromRe      = regexp.MustCompile(`^\s*cdef\s+extern\s+from\s+(<[^>]+>|"[^"]+"|'[^']+')`)
	flatbuffersIncludeRe    = regexp.MustCompile(`^\s*include\s+"([^"]+)"\s*;`)
	includeDirectiveParsers = newIncludeDirectiveParserRegistry()
	blockCommentOpen        = []byte("/*")
	blockCommentClose       = []byte("*/")
	backtraceHeaderInclude  = []byte("BACKTRACE_HEADER")
	opensslUnistdInclude    = []byte("OPENSSL_UNISTD")
	protoLineComment        = []byte("//")
	protoImportKw           = []byte("import")
	protoConfigIncludeKw    = []byte("option")
	protoConfigIncludeExt   = []byte("(NProtoConfig.Include)")
)

var swigImplicitDirectives = func() []IncludeDirective {
	names := []string{"swig.swg", "go.swg", "java.swg", "perl5.swg", "python.swg"}
	out := make([]IncludeDirective, 0, len(names))

	for _, n := range names {
		out = append(out, IncludeDirective{kind: includeSystem, target: internStr(n)})
	}

	return out
}()

type IncludeDirectiveParser interface {
	parse(rel string, data []byte, a *BumpAllocator[IncludeDirective]) ParsedIncludeSet

	id() uint32
}

type IncludeDirectiveParserRegistry struct {
	defaultParser IncludeDirectiveParser
}

func walkableBucketFor(rel string) ParsedIncludeBucket {
	if _, ok := includeDirectiveParsers.registeredParserFor(rel).(RagelIncludeDirectiveParser); ok {
		return parsedIncludesRagelNative
	}

	return parsedIncludesLocal
}

func newIncludeDirectiveParserRegistry() IncludeDirectiveParserRegistry {
	return IncludeDirectiveParserRegistry{defaultParser: CIncludeDirectiveParser{}}
}

func (r *IncludeDirectiveParserRegistry) registeredParserFor(rel string) IncludeDirectiveParser {
	return lookupParserForRel(rel)
}

func (r *IncludeDirectiveParserRegistry) parserFor(rel string) IncludeDirectiveParser {
	if p := lookupParserForRel(rel); p != nil {
		return p
	}

	return r.defaultParser
}

func (r *IncludeDirectiveParserRegistry) hasRegisteredParser(rel string) bool {
	return lookupParserForRel(rel) != nil
}

const directiveBlockHint = 1 << 14

func addDirective(block []IncludeDirective, k int, d IncludeDirective) int {
	if k == len(block) {
		throwFmt("directive block overflowed %d entries — raise directiveBlockHint", len(block))
	}

	block[k] = d

	return k + 1
}

func parseDelimitedIncludeTarget(s string) (string, IncludeKind, bool) {
	s = strings.TrimSpace(s)

	if s == "" {
		return "", includeSystem, false
	}

	kind := includeSystem

	switch s[0] {
	case '"', '\'':
		kind = includeQuoted
	case '<':
		kind = includeSystem
	default:
		return "", includeSystem, false
	}

	close := s[0]

	if close == '<' {
		close = '>'
	}

	end := strings.IndexByte(s[1:], close)

	if end < 0 {
		return "", includeSystem, false
	}

	target := s[1 : 1+end]

	if target == "" {
		return "", includeSystem, false
	}

	if kind == includeQuoted && len(target) >= 2 && target[0] == '<' && target[len(target)-1] == '>' {
		target = target[1 : len(target)-1]

		if target == "" {
			return "", includeSystem, false
		}

		kind = includeSystem
	}

	return target, kind, true
}

func isParserIdentContinuation(s string, idx int) bool {
	if idx >= len(s) {
		return false
	}

	switch c := s[idx]; {
	case c >= 'a' && c <= 'z':
		return true
	case c >= 'A' && c <= 'Z':
		return true
	case c >= '0' && c <= '9':
		return true
	case c == '_':
		return true
	default:
		return false
	}
}

func eachLine(data []byte, fn func(line []byte)) {
	for len(data) > 0 {
		i := bytes.IndexByte(data, '\n')

		if i < 0 {
			fn(data)

			return
		}

		line := data[:i]

		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}

		fn(line)
		data = data[i+1:]
	}
}
