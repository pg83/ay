package main

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

func compilerFlagExpression(text string, env Environment, expanding []string) ([]string, string) {
	return commandArgumentExpression(text, func(name string, _ bool) ([]string, string) {
		if slices.Contains(expanding, name) {
			throwFmt("self-contradictory variable definition (%s)", name)
		}

		value, _ := env.lookup(name)

		return compilerFlagExpression(value, env, append(expanding, name))
	})
}

func commandArgumentExpression(text string, resolve func(string, bool) ([]string, string)) (args []string, identity string) {
	var word, key strings.Builder
	var quote byte
	var variable []string

	hasTerm, standalone, started := false, false, false

	flush := func() {
		if standalone {
			args = append(args, variable...)
		} else if hasTerm {
			args = append(args, word.String())
		}

		word.Reset()
		hasTerm, standalone = false, false
	}

	for i := 0; i < len(text); {
		start := i
		ch := text[i]
		r, width := utf8.DecodeRuneInString(text[i:])

		if quote == 0 && unicode.IsSpace(r) {
			flush()
			key.WriteRune(r)
			i += width

			continue
		}

		if quote == 0 && ch == '&' && i+1 < len(text) && text[i+1] == '&' {
			if started {
				if !commandSeparatorTail(text[i:]) {
					throwFmt("could not parse compiler command: command separator in compiler arguments %q", text)
				}

				flush()
				key.WriteString(text[i:])

				return args, key.String()
			}

			key.WriteString(text[i : i+2])
			i += 2

			continue
		}

		started = true

		if ch == '\\' {
			end := min(i+2, len(text))

			word.WriteString(compilerFlagLiteral(text[i:end], quote != 0))
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

				word.WriteString(compilerFlagLiteral(text[start:i], quote != 0))
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
							word.WriteByte(',')
						} else {
							word.WriteString(compilerFlagLiteral(value, true))
						}

						i++
					} else if name == "" {
						throwFmt("could not parse compiler command: expected identifier in %q", text)
					}

					if i == len(text) || text[i] != '}' {
						throwFmt("could not parse compiler command: expected } in %q", text)
					}

					i++

					if literal {
						key.WriteString(text[start:i])
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
					word.WriteString(strings.Join(values, ""))
					key.WriteString(expanded)
					hasTerm = true

					continue
				}

				if quote != 0 {
					throwFmt("could not parse compiler command: unexpected $ in quoted string %q", text)
				}

				word.WriteByte('$')
			}
		} else {
			if quote == 0 && ch == '&' {
				throwFmt("could not parse compiler command: command separator in compiler arguments %q", text)
			}

			word.WriteString(text[i : i+width])
			i += width
		}

		key.WriteString(text[start:i])
		hasTerm, standalone = true, false
	}

	if quote != 0 {
		throwFmt("could not parse compiler command: unclosed quote in %q", text)
	}

	flush()

	return args, key.String()
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

func compilerFlagLiteral(text string, quoted bool) string {
	var value strings.Builder

	for i := 0; i < len(text); i++ {
		if text[i] == '\\' {
			i++

			if i == len(text) {
				throwFmt("could not parse compiler command: incomplete escape sequence in %q", text)
			}

			ch := text[i]

			if quoted && ch != '\'' && ch != '"' && ch != '\\' && ch != '/' {
				value.WriteByte('\\')
			}
		}

		value.WriteByte(text[i])
	}

	return value.String()
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
	resolved [2]*CcModuleArgBlocks
}

func compilerCommandBlocks(raw *CcModuleArgBlocks, cxx bool) *CcModuleArgBlocks {
	binding := raw.compilerFlags
	language := 0

	if cxx {
		language = 1
	}

	if blocks := binding.resolved[language]; blocks != nil {
		return blocks
	}

	in := binding.input
	common := compilerCommandFlags(in.CFlags, in.CompilerEnv)
	resources := resourceKeyUnion(common.Resources, compilerValueResourceKeys(in.CFlagValues))

	in.CFlags = common.Args

	if cxx {
		private := compilerCommandFlags(in.CXXFlags, in.CompilerEnv)

		resources = resourceKeyUnion(resources, private.Resources, compilerValueResourceKeys(in.CXXFlagValues))

		in.CXXFlags = private.Args
		in.COnlyFlags = nil
	} else {
		private := compilerCommandFlags(in.COnlyFlags, in.CompilerEnv)

		resources = resourceKeyUnion(resources, private.Resources, compilerValueResourceKeys(in.COnlyFlagValues))

		in.COnlyFlags = private.Args
		in.CXXFlags = nil
	}

	blocks := composeCCModuleArgBlocks(binding.arena, binding.platform, &in)
	extra := compilerExtraCFlagsValue(in.CompilerEnv)

	blocks.extraCFlags = extra.Args
	blocks.resources = resourceKeyUnion(resources, extra.Resources)

	binding.resolved[language] = &blocks

	return &blocks
}

func compilerExtraCFlags(env Environment) []ANY {
	return compilerExtraCFlagsValue(env).Args
}

func compilerExtraCFlagsValue(env Environment) CompilerFlagValue {
	return compilerCommandFlags([]ANY{strExtraCFlags.any()}, env)
}
