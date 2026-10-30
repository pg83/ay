package main

import (
	"regexp/syntax"
	"strings"
	"unicode/utf8"
)

type globMatcher struct {
	words         [][]globWord
	prog          *syntax.Prog
	current, next []uint32
	seen          []uint32
	epoch         uint32
}

type globWord struct {
	text    string
	wild    byte
	exclude byte
}

func newGlobMatcher(expr string) *globMatcher {
	re, err := syntax.Parse(expr, syntax.Perl)

	if err != nil {
		panic(err)
	}

	m := &globMatcher{epoch: uint32(chaosFirstEpoch(0))}

	if re.Op == syntax.OpConcat && re.Sub[0].Op == syntax.OpBeginText && re.Sub[len(re.Sub)-1].Op == syntax.OpEndText {
		body := &syntax.Regexp{Op: syntax.OpConcat, Sub: re.Sub[1 : len(re.Sub)-1]}

		if words, ok := globWords(body); ok {
			sameAlphabet := true

			for _, sequence := range words {
				var alphabet byte

				for _, word := range sequence {
					if word.wild == 'r' {
						alphabet = 0

						continue
					}

					if word.wild == '*' || word.wild == '?' {
						if alphabet != 0 && alphabet != word.exclude {
							sameAlphabet = false
						}

						alphabet = word.exclude
					}
				}
			}

			if sameAlphabet {
				m.words = words

				return m
			}
		}
	}

	// Compile returns a program or panics; its error result is always nil.
	m.prog, _ = syntax.Compile(re.Simplify())

	m.seen = make([]uint32, len(m.prog.Inst))
	m.current = make([]uint32, 0, len(m.prog.Inst))
	m.next = make([]uint32, 0, len(m.prog.Inst))

	return m
}

func globWords(re *syntax.Regexp) ([][]globWord, bool) {
	switch re.Op {
	case syntax.OpEmptyMatch:
		return [][]globWord{nil}, true
	case syntax.OpLiteral:
		if re.Flags&syntax.FoldCase != 0 {
			return nil, false
		}

		for _, r := range re.Rune {
			if r == utf8.RuneError {
				return nil, false
			}
		}

		return [][]globWord{{{text: string(re.Rune)}}}, true
	case syntax.OpAnyCharNotNL:
		return [][]globWord{{{wild: '?', exclude: '\n'}}}, true
	case syntax.OpStar:
		if re.Sub[0].Op == syntax.OpAnyCharNotNL {
			return [][]globWord{{{wild: '*', exclude: '\n'}}}, true
		}

		if globFilenameChar(re.Sub[0]) {
			return [][]globWord{{{wild: '*', exclude: '/'}}}, true
		}
	case syntax.OpQuest:
		sub := re.Sub[0]

		if sub.Op == syntax.OpCapture {
			sub = sub.Sub[0]
		}

		if sub.Op == syntax.OpConcat && len(sub.Sub) == 2 && sub.Sub[0].Op == syntax.OpStar && sub.Sub[0].Sub[0].Op == syntax.OpAnyCharNotNL && sub.Sub[1].Op == syntax.OpLiteral && string(sub.Sub[1].Rune) == "/" {
			return [][]globWord{{{wild: 'r'}}}, true
		}
	case syntax.OpCapture:
		return globWords(re.Sub[0])
	case syntax.OpCharClass:
		if globFilenameChar(re) {
			return [][]globWord{{{wild: '?', exclude: '/'}}}, true
		}

		var words [][]globWord

		for i := 0; i < len(re.Rune); i += 2 {
			if re.Rune[i+1]-re.Rune[i] > 32 || re.Rune[i] <= utf8.RuneError && re.Rune[i+1] >= utf8.RuneError {
				return nil, false
			}

			for r := re.Rune[i]; r <= re.Rune[i+1]; r++ {
				words = append(words, []globWord{{text: string(r)}})
			}
		}

		return words, true
	case syntax.OpConcat, syntax.OpAlternate:
		words := [][]globWord{nil}

		if re.Op == syntax.OpAlternate {
			words = nil
		}

		for _, sub := range re.Sub {
			tail, ok := globWords(sub)

			if !ok {
				return nil, false
			}

			if re.Op == syntax.OpAlternate {
				words = append(words, tail...)
			} else {
				if len(words)*len(tail) > 64 {
					return nil, false
				}

				var joined [][]globWord

				for _, a := range words {
					for _, b := range tail {
						w := make([]globWord, 0, len(a)+len(b))

						w = append(w, a...)

						for _, part := range b {
							if len(w) > 0 && w[len(w)-1].wild == 0 && part.wild == 0 {
								w[len(w)-1].text += part.text
							} else {
								w = append(w, part)
							}
						}

						joined = append(joined, w)
					}
				}

				words = joined
			}

			if len(words) > 64 {
				return nil, false
			}
		}

		return words, true
	}

	return nil, false
}

func globFilenameChar(re *syntax.Regexp) bool {
	return re.Op == syntax.OpCharClass && len(re.Rune) == 4 && re.Rune[0] == 0 && re.Rune[1] == '/'-1 && re.Rune[2] == '/'+1 && re.Rune[3] == utf8.MaxRune
}

func globRuneSize(s string) int {
	if s[0] < utf8.RuneSelf {
		return 1
	}

	_, size := utf8.DecodeRuneInString(s)

	return size
}

func matchGlobWords(words []globWord, name string) bool {
	if len(words) == 2 && words[0].wild == '*' && words[1].wild == 0 {
		suffix := words[1].text

		return strings.HasSuffix(name, suffix) && !strings.ContainsRune(name[:len(name)-len(suffix)], rune(words[0].exclude))
	}

	wi, pos, star, retry := 0, 0, -1, 0

	for {
		if wi == len(words) {
			if pos == len(name) {
				return true
			}
		} else {
			word := words[wi]

			switch word.wild {
			case 'r':
				if matchGlobWords(words[wi+1:], name[pos:]) {
					return true
				}

				for i := pos; i < len(name) && name[i] != '\n'; i++ {
					if name[i] == '/' && matchGlobWords(words[wi+1:], name[i+1:]) {
						return true
					}
				}

			case '*':
				star, retry = wi, pos
				wi++

				continue
			case '?':
				if pos < len(name) && name[pos] != word.exclude {
					pos += globRuneSize(name[pos:])
					wi++

					continue
				}
			default:
				if strings.HasPrefix(name[pos:], word.text) {
					pos += len(word.text)
					wi++

					continue
				}
			}
		}

		if star < 0 || retry == len(name) || name[retry] == words[star].exclude {
			return false
		}

		retry += globRuneSize(name[retry:])
		wi, pos = star+1, retry
	}
}

func (m *globMatcher) match(name string) bool {
	if m.words != nil {
		for _, words := range m.words {
			if matchGlobWords(words, name) {
				return true
			}
		}

		return false
	}

	current, next := m.current[:0], m.next[:0]
	before := rune(-1)

	m.newEpoch()

	for pos := 0; ; {
		after, size := rune(-1), 0

		if pos < len(name) {
			after, size = utf8.DecodeRuneInString(name[pos:])
		}

		context := syntax.EmptyOpContext(before, after)

		current = m.add(current, uint32(m.prog.Start), context)
		m.newEpoch()

		for _, pc := range current {
			inst := &m.prog.Inst[pc]

			if inst.Op == syntax.InstMatch {
				return true
			}

			if size == 0 {
				continue
			}

			matched := false

			switch inst.Op {
			case syntax.InstRune, syntax.InstRune1:
				matched = inst.MatchRune(after)
			case syntax.InstRuneAny:
				matched = true
			case syntax.InstRuneAnyNotNL:
				matched = after != '\n'
			}

			if matched {
				following := rune(-1)

				if pos+size < len(name) {
					following, _ = utf8.DecodeRuneInString(name[pos+size:])
				}

				next = m.add(next, inst.Out, syntax.EmptyOpContext(after, following))
			}
		}

		if size == 0 {
			return false
		}

		current, next = next, current[:0]
		before, pos = after, pos+size
	}
}

func (m *globMatcher) newEpoch() {
	m.epoch++

	if m.epoch == 0 {
		clear(m.seen)
		m.epoch = 1
	}
}

func (m *globMatcher) add(dst []uint32, pc uint32, context syntax.EmptyOp) []uint32 {
	if m.seen[pc] == m.epoch {
		return dst
	}

	m.seen[pc] = m.epoch

	inst := &m.prog.Inst[pc]

	switch inst.Op {
	case syntax.InstAlt, syntax.InstAltMatch:
		dst = m.add(dst, inst.Out, context)

		return m.add(dst, inst.Arg, context)
	case syntax.InstCapture, syntax.InstNop:
		return m.add(dst, inst.Out, context)
	case syntax.InstEmptyWidth:
		if syntax.EmptyOp(inst.Arg)&^context == 0 {
			return m.add(dst, inst.Out, context)
		}
	case syntax.InstFail:
	default:
		dst = append(dst, pc)
	}

	return dst
}
