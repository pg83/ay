package main

import (
	"strconv"
	"strings"
)

var cmdModifierNames = func() map[string]bool {
	names := map[string]bool{}

	for _, name := range strings.Fields(`add_to_outs addincl at bksl bq clear cmdsep comma context cwd dbgfail defext
		dirallowed empty env ext from_input glob global hash hide hideempty induced_deps input join kv lastext late_out
		main modlocal noallext noauto nobuildroot noext nopath norel not notransformbuilddir output output_include
		parse_bool pre qe quo requirements resource result rootdir rootrel skip_by_ext split stdout suf tags_cut tags_in
		tags_out tared tmp to_namespace tobindir tolower tool toupper trim`) {
		names[name] = true
	}

	return names
}()

type CmdToken struct {
	kind       byte
	start, end int
}

func cmdSyntaxError(text string) string {
	unknown := ""

	for i := 0; i < len(text); {
		switch {
		case text[i] == '\\':
			i += 2
		case strings.HasPrefix(text[i:], "$("):
			if j := strings.IndexAny(text[i+2:], "()"); j >= 0 && text[i+2+j] == ')' {
				i += j + 3
			} else {
				i++
			}
		case strings.HasPrefix(text[i:], "${"):
			end, at, message, mod := cmdTransformation(text, i+2)

			if message != "" {
				line := 1 + strings.Count(text[:at], "\n")
				col := at - strings.LastIndexByte(text[:at], '\n') - 1

				return "could not parse command (" + strconv.Itoa(line) + ":" + strconv.Itoa(col) + "): " + message + "\n" + text
			}

			if unknown == "" {
				unknown = mod
			}

			i = end
		default:
			i++
		}
	}

	if unknown != "" {
		return "unknown modifier " + unknown
	}

	return ""
}

func cmdTransformation(text string, k int) (end, at int, message, unknown string) {
	recognition := func(start, stop int) string {
		return "token recognition error at: '" + text[start:min(stop+1, len(text))] + "'"
	}

	lex := func() (CmdToken, string) {
		start := k

		if k == len(text) {
			return CmdToken{start: k, end: k}, ""
		}

		switch c := text[k]; {
		case c == '}' || c == '=' || c == ';' || c == ':':
			k++

			return CmdToken{kind: c, start: start, end: k}, ""
		case c == '"':
			for k++; k < len(text) && text[k] != '"'; k++ {
				if text[k] == '\\' {
					k++
				}
			}

			if k >= len(text) {
				return CmdToken{start: start}, recognition(start, len(text))
			}

			k++

			return CmdToken{kind: 's', start: start, end: k}, ""
		case c == '_' || 'a' <= c|0x20 && c|0x20 <= 'z':
			k += cmdIdentLen(text[k:])

			return CmdToken{kind: 'i', start: start, end: k}, ""
		}

		return CmdToken{start: start}, recognition(start, k)
	}

	modValue := func() (int, string) {
		for k < len(text) && text[k] != ';' && text[k] != ':' {
			switch {
			case text[k] == '\\':
				k += 2
			case strings.HasPrefix(text[k:], "${"):
				n := cmdIdentLen(text[k+2:])

				if n == 0 || k+2+n == len(text) || text[k+2+n] != '}' {
					return k, recognition(k, k+2+n)
				}

				k += n + 3
			case text[k] == '$':
				n := cmdIdentLen(text[k+1:])

				if n == 0 {
					return k, recognition(k, k+1)
				}

				k += n + 1
			default:
				k++
			}
		}

		k = min(k, len(text))

		return 0, ""
	}

	var toks []CmdToken

	for {
		t, message := lex()

		if message != "" {
			return 0, t.start, message, ""
		}

		toks = append(toks, t)

		if t.kind == '=' {
			start := k

			if pos, message := modValue(); message != "" {
				return 0, pos, message, ""
			}

			toks = append(toks, CmdToken{kind: 'v', start: start, end: k})
		}

		if t.kind == '}' || t.kind == 0 {
			break
		}
	}

	display := func(t CmdToken) string {
		if t.kind == 0 {
			return "'<EOF>'"
		}

		return "'" + text[t.start:t.end] + "'"
	}

	mismatched := func(t CmdToken, expecting string) (int, int, string, string) {
		return 0, t.start, "mismatched input " + display(t) + " expecting " + expecting, ""
	}

	noViable := func(from, t CmdToken) (int, int, string, string) {
		return 0, t.start, "no viable alternative at input '" + text[from.start:max(t.start, t.end)] + "'", ""
	}

	p := 0

	if toks[0].kind == 'i' && toks[1].kind != '}' {
		for {
			key := toks[p]

			if key.kind != 'i' {
				return mismatched(key, "IDENTIFIER")
			}

			if name := text[key.start:key.end]; !cmdModifierNames[name] && unknown == "" {
				unknown = name
			}

			p++

			if toks[p].kind == '=' {
				p += 2
			}

			sep := toks[p]

			p++

			if sep.kind == ':' {
				break
			}

			if sep.kind != ';' {
				return noViable(key, sep)
			}
		}
	}

	if body := toks[p]; body.kind != 'i' && body.kind != 's' {
		return mismatched(body, "{STRING, IDENTIFIER}")
	}

	if closing := toks[p+1]; closing.kind == 0 {
		return 0, closing.start, "missing '}' at '<EOF>'", ""
	} else if closing.kind != '}' {
		return mismatched(closing, "'}'")
	}

	return toks[p+1].end, 0, "", unknown
}
