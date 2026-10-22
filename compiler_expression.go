package main

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// CmdLexer/CmdParser distinguish a standalone variable (argument splicing)
// from a variable inside a term (concatenation). Parse before unescaping so a
// protected dollar cannot turn back into an expression during substitution.
func compilerFlagExpression(text string, env Environment, expanding []string) (args []string, identity string) {
	var word, key strings.Builder
	var quote byte
	var variable []string
	hasTerm, standalone := false, false
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
				// TEXT_NOP: internal runtime roots such as $(S) are literals.
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
					// Source EscapeArgs emits quo for whole && and ; arguments.
					literal := strings.HasPrefix(text[i:], `:"`) && name == "quo" || name == "" && i < len(text) && text[i] == '"'
					if literal {
						if name == "quo" {
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
						word.WriteString(compilerFlagLiteral(value, true))
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
					if slices.Contains(expanding, name) {
						throwFmt("self-contradictory variable definition (%s)", name)
					}
					value, _ := env.lookup(name)
					values, expanded := compilerFlagExpression(value, env, append(expanding, name))
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

// The command parser unescapes raw and quoted text differently. TEXT_NOP
// bypasses variable parsing, but still uses the same text unescaping.
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

// Bind reserved resource roots only after the module body and peer collection.
// Source conditions see the original bindings; no body is interpreted again.
func (e *EmitContext) bindCompilerFlagResources(globals []ResourceDecl) {
	env := Environment{s: &e.frame.ifEnv}
	for _, decl := range globals {
		env.setVFS(internEnvSTR(decl.GlobalVar), decl.Value)
	}

}

// Command blocks retain their module bindings until a command consumes the
// flags. An unused private variable need not be a valid command expression.
// Cache only these pure argument blocks; module statements are never replayed.
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
	in.CFlags = compilerCommandFlags(in.CFlags, in.CompilerEnv).Args
	if cxx {
		in.CXXFlags = compilerCommandFlags(in.CXXFlags, in.CompilerEnv).Args
		in.COnlyFlags = nil
	} else {
		in.COnlyFlags = compilerCommandFlags(in.COnlyFlags, in.CompilerEnv).Args
		in.CXXFlags = nil
	}
	blocks := composeCCModuleArgBlocks(binding.arena, binding.platform, &in)
	binding.resolved[language] = &blocks
	return &blocks
}
