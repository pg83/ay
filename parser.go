package main

import (
	"bytes"
)

var (
	blockCommentOpen       = []byte("/*")
	blockCommentClose      = []byte("*/")
	backtraceHeaderInclude = []byte("BACKTRACE_HEADER")
	opensslUnistdInclude   = []byte("OPENSSL_UNISTD")
	protoImportKw          = []byte("import")
	protoConfigIncludeKw   = []byte("option")
	protoConfigIncludeExt  = []byte("(NProtoConfig.Include)")
)

var swigImplicitDirectives = func() []IncludeDirective {
	names := []string{"swig.swg", "go.swg", "java.swg", "perl5.swg", "python.swg"}
	out := make([]IncludeDirective, 0, len(names))

	for _, n := range names {
		out = append(out, IncludeDirective{kind: includeSystem, target: includeTarget(internStr(n).any())})
	}

	return out
}()

var statelessIncludeParsers = [...]IncludeDirectiveParser{
	includeParserNone:        nil,
	includeParserC:           CIncludeDirectiveParser{},
	includeParserGoCgo:       GoCgoIncludeDirectiveParser{},
	includeParserRagel:       RagelIncludeDirectiveParser{},
	includeParserFlatbuffers: FlatbuffersIncludeDirectiveParser{},
	includeParserYasm:        YasmIncludeDirectiveParser{},
	includeParserCython:      CythonIncludeDirectiveParser{},
	includeParserSwig:        SwigIncludeDirectiveParser{},
	includeParserSc:          ScIncludeDirectiveParser{},
	includeParserLex:         LexIncludeDirectiveParser{},
	includeParserTS:          TSIncludeDirectiveParser{},
	includeParserRos:         RosIncludeDirectiveParser{},
	includeParserFortran:     FortranIncludeDirectiveParser{},
	includeParserNlg:         NlgIncludeDirectiveParser{},
	includeParserSsqls:       SsqlsIncludeDirectiveParser{},
}

const directiveBlockHint = 1 << 14

const (
	includeParserNone IncludeParserKind = iota
	includeParserC
	includeParserGoCgo
	includeParserRagel
	includeParserFlatbuffers
	includeParserYasm
	includeParserCython
	includeParserSwig
	includeParserSc
	includeParserLex
	includeParserTS
	includeParserRos
	includeParserFortran
	includeParserNlg
	includeParserSsqls
	includeParserProto
	includeParserCfgProto
)

func isParserSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n' || b == '\f' || b == '\v'
}

func trimParserSpace(b []byte) []byte {
	for len(b) > 0 && isParserSpace(b[0]) {
		b = b[1:]
	}

	for len(b) > 0 && isParserSpace(b[len(b)-1]) {
		b = b[:len(b)-1]
	}

	return b
}

func cutParserKeyword(b []byte, keyword string) ([]byte, bool) {
	if len(b) <= len(keyword) || b[0] != keyword[0] || !bytes.Equal(b[:len(keyword)], strBytes(keyword)) || !isParserSpace(b[len(keyword)]) {
		return nil, false
	}

	b = b[len(keyword)+1:]

	for len(b) > 0 && isParserSpace(b[0]) {
		b = b[1:]
	}

	return b, len(b) > 0
}

type IncludeParserKind uint8

func includeParserKind(parser IncludeDirectiveParser) IncludeParserKind {
	switch parser.(type) {
	case CIncludeDirectiveParser:
		return includeParserC
	case GoCgoIncludeDirectiveParser:
		return includeParserGoCgo
	case RagelIncludeDirectiveParser:
		return includeParserRagel
	case FlatbuffersIncludeDirectiveParser:
		return includeParserFlatbuffers
	case YasmIncludeDirectiveParser:
		return includeParserYasm
	case CythonIncludeDirectiveParser:
		return includeParserCython
	case SwigIncludeDirectiveParser:
		return includeParserSwig
	case ScIncludeDirectiveParser:
		return includeParserSc
	case LexIncludeDirectiveParser:
		return includeParserLex
	case TSIncludeDirectiveParser:
		return includeParserTS
	case RosIncludeDirectiveParser:
		return includeParserRos
	case FortranIncludeDirectiveParser:
		return includeParserFortran
	case NlgIncludeDirectiveParser:
		return includeParserNlg
	case SsqlsIncludeDirectiveParser:
		return includeParserSsqls
	case ProtoIncludeDirectiveParser:
		return includeParserProto
	case CfgProtoIncludeDirectiveParser:
		return includeParserCfgProto
	}

	return includeParserNone
}

func (r *IncludeDirectiveParserRegistry) parser(kind IncludeParserKind) IncludeDirectiveParser {
	switch kind {
	case includeParserProto:
		return r.proto
	case includeParserCfgProto:
		return CfgProtoIncludeDirectiveParser{proto: r.proto}
	default:
		return statelessIncludeParsers[kind]
	}
}

type IncludeDirectiveParser interface {
	parse(rel string, data [][]byte, a *BumpAllocator[IncludeDirective]) ParsedIncludeSet
}

type IncludeDirectiveParserRegistry struct {
	matcher       *ExtMatcher[IncludeDirectiveParser]
	defaultParser IncludeDirectiveParser
	proto         ProtoIncludeDirectiveParser
}

func newIncludeDirectiveParserRegistry() IncludeDirectiveParserRegistry {
	proto := ProtoIncludeDirectiveParser{induced: newIntMap[STR](1 << 12)}

	return IncludeDirectiveParserRegistry{
		matcher:       buildParserExtMatcher(proto),
		defaultParser: CIncludeDirectiveParser{},
		proto:         proto,
	}
}

func (r *IncludeDirectiveParserRegistry) registeredParserFor(rel string) (IncludeDirectiveParser, bool) {
	if extIsTemplateIn(rel) {
		rel = rel[:len(rel)-len(".in")]
	}

	return r.matcher.match(rel)
}

func (r *IncludeDirectiveParserRegistry) scansIncludes(rel string) bool {
	p, _ := r.registeredParserFor(rel)

	return p != nil
}

func countDirective(block []IncludeDirective, k int, d IncludeDirective) int {
	if k < len(block) {
		block[k] = d
	}

	return k + 1
}

func countedDirectives(a *BumpAllocator[IncludeDirective], fill func([]IncludeDirective) int) []IncludeDirective {
	block := a.alloc(directiveBlockHint)
	k := fill(block)

	if k > len(block) {
		a.commit(0)
		block = a.alloc(k)
		k = fill(block)
	}

	a.commit(k)

	return block[:k:k]
}

func addDirective(block []IncludeDirective, k int, d IncludeDirective) int {
	if k == len(block) {
		throwFmt("directive block overflowed %d entries — raise directiveBlockHint", len(block))
	}

	block[k] = d

	return k + 1
}

func parseDelimitedIncludeTarget(b []byte) ([]byte, IncludeKind, bool) {
	for len(b) > 0 && isParserSpace(b[0]) {
		b = b[1:]
	}

	if len(b) == 0 {
		return nil, includeSystem, false
	}

	kind := includeSystem

	switch b[0] {
	case '"', '\'':
		kind = includeQuoted
	case '<':
		kind = includeSystem
	default:
		return nil, includeSystem, false
	}

	close := b[0]

	if close == '<' {
		close = '>'
	}

	end := bytes.IndexByte(b[1:], close)

	if end < 0 {
		return nil, includeSystem, false
	}

	target := b[1 : 1+end]

	if len(target) == 0 {
		return nil, includeSystem, false
	}

	if kind == includeQuoted && len(target) >= 2 && target[0] == '<' && target[len(target)-1] == '>' {
		target = target[1 : len(target)-1]

		if len(target) == 0 {
			return nil, includeSystem, false
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

func eachLine(chunks [][]byte, fn func(line []byte)) {
	if len(chunks) == 1 {
		data := chunks[0]

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

		return
	}

	var pending []byte

	for chunkIdx, chunk := range chunks {
		last := chunkIdx == len(chunks)-1

		if len(pending) > 0 {
			i := bytes.IndexByte(chunk, '\n')

			if i < 0 {
				pending = append(pending, chunk...)

				if last {
					fn(pending)
				}

				continue
			}

			pending = append(pending, chunk[:i]...)

			line := pending

			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}

			fn(line)
			pending = pending[:0]
			chunk = chunk[i+1:]
		}

		for len(chunk) > 0 {
			i := bytes.IndexByte(chunk, '\n')

			if i < 0 {
				if last {
					fn(chunk)
				} else {
					pending = append(pending[:0], chunk...)
				}

				break
			}

			line := chunk[:i]

			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}

			fn(line)
			chunk = chunk[i+1:]
		}
	}
}
