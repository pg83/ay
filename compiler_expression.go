package main

import (
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

var (
	commandExpressionPool = sync.Pool{New: func() any { return &commandExpressionScratch{} }}
	compilerFlagExpanding = make([]string, 0, 32)
)

type commandExpressionScratch struct{ word, key []byte }

func compilerFlagExpression(text string, env Environment, expanding []string) ([]string, string) {
	return commandArgumentExpression(text, func(name string, _ bool) ([]string, string) {
		if slices.Contains(expanding, name) {
			throwFmt("self-contradictory variable definition (%s)", name)
		}

		value, _ := env.lookup(name)

		return compilerFlagExpression(value, env, append(expanding, name))
	})
}

func configuredCompilerFlags(text string, env Environment) []ANY {
	args, _ := compilerFlagExpression(text, env, nil)

	return internAnys(args)
}

func commandArgumentExpression(text string, resolve func(string, bool) ([]string, string)) (args []string, identity string) {
	scratch := commandExpressionPool.Get().(*commandExpressionScratch)

	defer commandExpressionPool.Put(scratch)

	scratch.word, scratch.key = scratch.word[:0], scratch.key[:0]
	var quote byte
	var variable []string

	hasTerm, standalone, started := false, false, false

	flush := func() {
		if standalone {
			args = append(args, variable...)
		} else if hasTerm {
			args = append(args, internBytes(scratch.word).string())
		}

		scratch.word = scratch.word[:0]
		hasTerm, standalone = false, false
	}

	for i := 0; i < len(text); {
		start := i
		ch := text[i]
		r, width := utf8.DecodeRuneInString(text[i:])

		if quote == 0 && unicode.IsSpace(r) {
			flush()
			scratch.key = utf8.AppendRune(scratch.key, r)
			i += width

			continue
		}

		if quote == 0 && ch == '&' && i+1 < len(text) && text[i+1] == '&' {
			if started {
				if !commandSeparatorTail(text[i:]) {
					throwFmt("could not parse compiler command: command separator in compiler arguments %q", text)
				}

				flush()
				scratch.key = append(scratch.key, text[i:]...)

				return args, internBytes(scratch.key).string()
			}

			scratch.key = append(scratch.key, text[i:i+2]...)
			i += 2

			continue
		}

		started = true

		if ch == '\\' {
			end := min(i+2, len(text))

			scratch.word = appendCompilerFlagLiteral(scratch.word, text[i:end], quote != 0)
			i = end
		} else if ch == quote {
			quote = 0
			i++
		} else if quote == 0 && (ch == '\'' || ch == '"') {
			quote = ch
			i++
		} else if ch == '$' {
			i++

			if i < len(text) && text[i] == '(' {
				if end := strings.IndexByte(text[i:], ')'); end >= 0 && !strings.ContainsRune(text[i+1:i+end], '(') {
					i += end + 1
				}

				scratch.word = appendCompilerFlagLiteral(scratch.word, text[start:i], quote != 0)
			} else {
				braced := i < len(text) && text[i] == '{'

				if braced {
					i++
				}

				nameStart := i

				for i < len(text) && (text[i] == '_' || text[i] >= 'a' && text[i] <= 'z' || text[i] >= 'A' && text[i] <= 'Z' || i > nameStart && text[i] >= '0' && text[i] <= '9') {
					i++
				}

				name := text[nameStart:i]

				if braced {
					literal := strings.HasPrefix(text[i:], `:"`) && (name == "quo" || name == "comma") || name == "" && i < len(text) && text[i] == '"'

					if literal {
						if name != "" {
							i++
						}

						i++

						begin := i

						for i < len(text) && text[i] != '"' {
							if text[i] == '\\' {
								i++
							}

							i++
						}

						if i >= len(text) {
							throwFmt("could not parse compiler command: unclosed string in %q", text)
						}

						value := text[begin:i]

						if name == "comma" {
							scratch.word = append(scratch.word, ',')
						} else {
							scratch.word = appendCompilerFlagLiteral(scratch.word, value, true)
						}

						i++
					} else if name == "" {
						throwFmt("could not parse compiler command: expected identifier in %q", text)
					}

					if !literal && name == "input" && i < len(text) && text[i] == ':' {
						i++

						if i < len(text) && text[i] == '"' {
							for i++; i < len(text) && text[i] != '"'; i++ {
								if text[i] == '\\' {
									i++
								}
							}

							i++
						}

						for i < len(text) && text[i] != '}' {
							i++
						}

						name = text[nameStart:min(i, len(text))]
					}

					if i == len(text) || text[i] != '}' {
						throwFmt("could not parse compiler command: expected } in %q", text)
					}

					i++

					if literal {
						scratch.key = append(scratch.key, text[start:i]...)
						hasTerm, standalone = true, false

						continue
					}
				}

				if name != "" {
					atEnd := i == len(text) || strings.HasPrefix(text[i:], "&&")

					if !atEnd {
						next, _ := utf8.DecodeRuneInString(text[i:])

						atEnd = unicode.IsSpace(next)
					}

					values, expanded := resolve(name, hasTerm || quote != 0 || !atEnd)

					standalone = !hasTerm && quote == 0
					variable = values

					for _, value := range values {
						scratch.word = append(scratch.word, value...)
					}

					scratch.key = append(scratch.key, expanded...)
					hasTerm = true

					continue
				}

				if quote != 0 {
					throwFmt("could not parse compiler command: unexpected $ in quoted string %q", text)
				}

				scratch.word = append(scratch.word, '$')
			}
		} else if quote == 0 && ch == '&' {
			if i+1 == len(text) {
				throwFmt("could not parse compiler command: command separator in compiler arguments %q", text)
			}

			_, next := utf8.DecodeRuneInString(text[i+1:])

			i += 1 + next
		} else {
			scratch.word = append(scratch.word, text[i:i+width]...)
			i += width
		}

		scratch.key = append(scratch.key, text[start:i]...)
		hasTerm, standalone = true, false
	}

	if quote != 0 {
		throwFmt("could not parse compiler command: unclosed quote in %q", text)
	}

	flush()

	return args, internBytes(scratch.key).string()
}

func commandSeparatorTail(text string) bool {
	for {
		text = strings.TrimLeftFunc(text, unicode.IsSpace)

		if text == "" {
			return true
		}

		if !strings.HasPrefix(text, "&&") {
			return false
		}

		text = text[2:]
	}
}

func appendCompilerFlagLiteral(value []byte, text string, quoted bool) []byte {
	if !strings.ContainsRune(text, '\\') {
		return append(value, text...)
	}

	for i := 0; i < len(text); i++ {
		if text[i] == '\\' {
			i++

			if i == len(text) {
				break
			}

			ch := text[i]

			if quoted && ch != '\'' && ch != '"' && ch != '\\' && ch != '/' {
				value = append(value, '\\')
			}
		}

		value = append(value, text[i])
	}

	return value
}

func (e *EmitContext) bindCompilerFlagResources(globals []ResourceDecl) {
	env := Environment{s: &e.frame.ifEnv}

	for _, decl := range globals {
		env.setResource(decl)
	}
}

type compilerFlagBlocks struct {
	arena    *NodeArenas
	platform *Platform
	input    ModuleCompileEnv
	resolved [2]CcModuleArgBlocks
	built    [2]bool
}

func compilerCommandBlocks(raw *CcModuleArgBlocks, cxx bool) *CcModuleArgBlocks {
	binding := raw.compilerFlags
	language := 0

	if cxx {
		language = 1
	}

	if binding.built[language] {
		return &binding.resolved[language]
	}

	in := binding.input
	common := compilerCommandFlags(appendBaseUnitSanitizerCFlags(in.CFlags, in.CompilerEnv), in.CompilerEnv)
	resources := resourceKeyUnion(common.Resources, in.FlagResourceUses[compilerFlagsCommon], compilerValueResourceKeys(in.CFlagValues))

	in.CFlags = common.Args

	if cxx {
		private := compilerCommandFlags(in.CXXFlags, in.CompilerEnv)

		resources = resourceKeyUnion(resources, private.Resources, in.FlagResourceUses[compilerFlagsCxx], compilerValueResourceKeys(in.CXXFlagValues))

		in.CXXFlags = private.Args
		in.COnlyFlags = nil
	} else {
		private := compilerCommandFlags(in.COnlyFlags, in.CompilerEnv)

		resources = resourceKeyUnion(resources, private.Resources, in.FlagResourceUses[compilerFlagsCOnly], compilerValueResourceKeys(in.COnlyFlagValues))

		in.COnlyFlags = private.Args
		in.CXXFlags = nil
	}

	blocks := &binding.resolved[language]

	*blocks = composeCCCommandArgBlocks(binding.arena, binding.platform, &in, raw.includes)

	extra := compilerExtraCFlagsValue(in.CompilerEnv)

	blocks.extraCFlags = extra.Args
	blocks.resources = resourceKeyUnion(resources, extra.Resources)

	binding.built[language] = true

	return blocks
}

func compilerExtraCFlags(env Environment) []ANY {
	return compilerExtraCFlagsValue(env).Args
}

func compilerExtraCFlagsValue(env Environment) CompilerFlagValue {
	return compilerCommandFlags([]ANY{strExtraCFlags.any()}, env)
}

func nextConfigMacroVariable(s string, from int) (int, int) {
	for i := from; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			_, size := utf8.DecodeRuneInString(s[i+1:])

			return i, i + 1 + size
		}

		if s[i] != '$' {
			continue
		}

		j := i + 1
		braced := j < len(s) && s[j] == '{'

		if braced {
			j++
		}

		if braced && j < len(s) && s[j] == '"' {
			j++

			for j < len(s) && s[j] != '"' {
				if s[j] == '\\' {
					j++

					if j == len(s) || s[j] == '\n' {
						break
					}
				}

				_, size := utf8.DecodeRuneInString(s[j:])

				j += size
			}

			if j+1 < len(s) && s[j] == '"' && s[j+1] == '}' {
				return i, j + 2
			}

			continue
		}

		if j == len(s) || !macroVariableLetter(s[j]) {
			continue
		}

		j++

		for j < len(s) {
			if macroVariableLetter(s[j]) || s[j] >= '0' && s[j] <= '9' {
				j++

				continue
			}

			if s[j] != '-' {
				break
			}

			end := j

			for end < len(s) && s[end] == '-' {
				end++
			}

			if end == len(s) || !(macroVariableLetter(s[end]) || s[end] >= '0' && s[end] <= '9') {
				break
			}

			j = end + 1
		}

		if !braced {
			return i, j
		}

		if j < len(s) && s[j] == '}' {
			return i, j + 1
		}
	}

	return len(s), len(s)
}

func macroVariableLetter(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

func replaceConfigMacroVariables(value string, replace func(int, int) string) string {
	var buf [256]byte

	out := buf[:0]
	cursor, copied := 0, 0
	changed := false

	for {
		start, end := nextConfigMacroVariable(value, cursor)

		if start == end {
			break
		}

		replacement := replace(start, end)

		if replacement != value[start:end] {
			out = append(out, value[copied:start]...)
			out = append(out, replacement...)

			copied = end
			changed = true
		}

		cursor = end
	}

	if !changed {
		return value
	}

	out = append(out, value[copied:]...)

	return string(out)
}
