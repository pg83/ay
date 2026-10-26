package main

import (
	"crypto/md5"
	"fmt"
	"strings"
)

const confMacroCallStackSize = 100

const (
	confKeywordFlag ConfKeywordKind = iota
	confKeywordScalar
	confKeywordArray
)

type ConfKeywordKind uint8

type ConfKeyword struct {
	name    string
	kind    ConfKeywordKind
	present []string
	missing []string
}

type ConfMacroSignature struct {
	keywords   []ConfKeyword
	positional []string
	vararg     string
}

type ConfMacroCall struct {
	name STR
	args string
}

type ConfMacro struct {
	file  string
	sig   ConfMacroSignature
	calls []ConfMacroCall
}

type confMacroBody struct {
	header      string
	calls       []ConfMacroCall
	unsupported bool
}

func (f *ConfFile) addMacro(header string, calls []ConfMacroCall) {
	name, params, opened := strings.Cut(strings.TrimPrefix(header, "macro "), "(")
	params, closed := strings.CutSuffix(strings.TrimSpace(params), ")")

	if !opened || !closed {
		throwFmt("conf: %s: malformed macro header %q", f.rel, header)
	}

	sig, specialized := parseConfMacroSignature(f.rel, params)
	key := internStr(strings.TrimSpace(name))

	if !specialized && len(calls) == 0 {
		return
	}

	if f.macros == nil {
		f.macros = map[STR]*ConfMacro{}
	}

	if specialized {
		f.macros[key] = nil

		return
	}

	if _, ok := f.macros[key]; !ok {
		f.macros[key] = &ConfMacro{file: f.rel, sig: sig, calls: calls}
	}
}

func (body *confMacroBody) addLine(line string) {
	if body.unsupported {
		return
	}

	if property, ok := strings.CutPrefix(line, "."); ok {
		key, _, _ := strings.Cut(property, "=")

		switch strings.TrimSpace(key) {
		case "ALLOWED_IN_LINTERS_MAKE", "SEM", "STRUCT_SEM":
			return
		}
	} else {
		name, args, opened := strings.Cut(line, "(")

		name = strings.TrimSpace(name)

		args, closed := strings.CutSuffix(args, ")")

		if opened && closed && isIdentShapedName(name) {
			body.calls = append(body.calls, ConfMacroCall{name: internStr(name), args: strings.TrimSpace(args)})

			return
		}
	}

	body.calls = nil
	body.unsupported = true
}

func parseConfMacroSignature(rel, params string) (sig ConfMacroSignature, specialized bool) {
	for _, param := range splitConfMacroParams(params) {
		if strings.HasPrefix(param, `"`) {
			specialized = true

			continue
		}

		if vararg, ok := strings.CutSuffix(param, "..."); ok {
			sig.vararg = vararg

			continue
		}

		n := 0

		for n < len(param) && (isConfIdentByte(param[n], n == 0) || n > 0 && param[n] == '-') {
			n++
		}

		name, rest := param[:n], strings.TrimSpace(param[n:])

		if strings.HasPrefix(rest, "{") {
			end := strings.IndexByte(rest, '}')

			if end < 0 {
				throwFmt("conf: %s: malformed macro parameter %q", rel, param)
			}

			rest = strings.TrimSpace(rest[end+1:])
		}

		switch {
		case rest == "":
			sig.positional = append(sig.positional, name)
		case rest == "[]":
			sig.keywords = append(sig.keywords, ConfKeyword{name: name, kind: confKeywordArray})
		case strings.HasPrefix(rest, "?"):
			keyword := ConfKeyword{name: name, kind: confKeywordFlag, present: []string{name}}

			if init := strings.TrimSpace(rest[1:]); init != "" {
				onTrue, rest := cutConfQuoted(rel, init)
				onFalse, rest := cutConfQuoted(rel, strings.TrimPrefix(strings.TrimSpace(rest), ":"))

				if strings.TrimSpace(rest) != "" {
					throwFmt("conf: %s: malformed macro parameter %q", rel, param)
				}

				if onTrue != "" {
					keyword.present = []string{onTrue}
				}

				if onFalse != "" {
					keyword.missing = []string{onFalse}
				}
			}

			sig.keywords = append(sig.keywords, keyword)
		case strings.HasPrefix(rest, "="):
			keyword := ConfKeyword{name: name, kind: confKeywordScalar}
			value, tail := cutConfQuoted(rel, rest[1:])

			if strings.TrimSpace(tail) != "" {
				throwFmt("conf: %s: malformed macro parameter %q", rel, param)
			}

			if value != "" {
				keyword.missing = []string{value}
			}

			sig.keywords = append(sig.keywords, keyword)
		default:
			throwFmt("conf: %s: malformed macro parameter %q", rel, param)
		}
	}

	return sig, specialized
}

func splitConfMacroParams(params string) []string {
	var out []string

	quoted, start := false, 0

	for i := 0; i <= len(params); i++ {
		if i < len(params) && params[i] == '"' {
			quoted = !quoted
		}

		if i == len(params) || params[i] == ',' && !quoted {
			if param := strings.TrimSpace(params[start:i]); param != "" {
				out = append(out, param)
			}

			start = i + 1
		}
	}

	return out
}

func cutConfQuoted(rel, value string) (string, string) {
	value = strings.TrimSpace(value)

	if value == "" || value[0] != '"' {
		throwFmt("conf: %s: expected a quoted macro parameter value, got %q", rel, value)
	}

	for i := 1; i < len(value); i++ {
		switch value[i] {
		case '\\':
			i++
		case '"':
			return value[1:i], value[i+1:]
		}
	}

	throwFmt("conf: %s: unterminated macro parameter value %q", rel, value)

	return "", ""
}

func (sig *ConfMacroSignature) keyword(arg string) int {
	for i := range sig.keywords {
		if sig.keywords[i].name == arg {
			return i
		}
	}

	return -1
}

func (sig *ConfMacroSignature) arrayEnd(args []string, i int) int {
	for i < len(args) && sig.keyword(args[i]) < 0 {
		i++
	}

	return i
}

func (sig *ConfMacroSignature) bind(args []string) (map[string][]string, string) {
	locals := map[string][]string{}
	seen := make([]bool, len(sig.keywords))
	next, array := 0, ""

	for i := 0; i < len(args) || next < len(sig.positional); {
		if i == len(args) {
			return locals, fmt.Sprintf("Value for '%s' argument is missing.", sig.positional[next])
		}

		if k := sig.keyword(args[i]); k >= 0 {
			keyword := sig.keywords[k]

			if seen[k] && keyword.kind != confKeywordArray {
				return locals, fmt.Sprintf("Keyword '%s' appears more than once and is not an array.", keyword.name)
			}

			seen[k] = true
			i++

			switch keyword.kind {
			case confKeywordFlag:
				locals[keyword.name] = append(locals[keyword.name], keyword.present...)
			case confKeywordScalar:
				if i == len(args) || sig.keyword(args[i]) >= 0 {
					return locals, fmt.Sprintf("Missing value for scalar keyword '%s'.", keyword.name)
				}

				locals[keyword.name] = append(locals[keyword.name], args[i])
				i++
			case confKeywordArray:
				end := sig.arrayEnd(args, i)

				array = keyword.name
				locals[keyword.name] = append(locals[keyword.name], args[i:end]...)
				i = end
			}

			continue
		}

		if array != "" {
			return locals, fmt.Sprintf("Mixing positional arguments with named arrays is error prone and forbidden.\n\tGot positional arg '%s' after named array '%s' has been started.", args[i], array)
		}

		if next < len(sig.positional) {
			locals[sig.positional[next]] = append(locals[sig.positional[next]], args[i])
			next++
			i++

			continue
		}

		if sig.vararg == "" {
			return locals, fmt.Sprintf("More positional arguments passed than expected. Can't handle arg: '%s'.", args[i])
		}

		end := sig.arrayEnd(args, i)

		locals[sig.vararg] = append(locals[sig.vararg], args[i:end]...)
		i = end
	}

	for _, keyword := range sig.keywords {
		if _, ok := locals[keyword.name]; !ok {
			locals[keyword.name] = keyword.missing
		}
	}

	if _, ok := locals[sig.vararg]; !ok && sig.vararg != "" {
		locals[sig.vararg] = nil
	}

	return locals, ""
}

func expandConfMacro(ctx *GenCtx, modulePath string, kind ModuleKind, language Language, v UnknownStmt, env Environment, d *ModuleData, onWarn func(Warn), platform *Platform) {
	macro := platform.confMacros[v.Raw]

	switch {
	case macro != nil:
	case v.Name == tokInvalid:
		onWarn(Warn{Kind: WarnUnknownMacro, Message: fmt.Sprintf("%s: macro %q is not in the modelled set (line %d); skipped", modulePath, v.Raw.string(), v.Line)})

		return
	default:
		warnUnmodelledMacro(onWarn, modulePath, v.Name)

		return
	}

	if d.confMacroDepth == confMacroCallStackSize {
		throwFmt("%s: config error: recursion in the calls of macro %s", modulePath, v.Raw.string())
	}

	locals, failure := macro.sig.bind(anyStrs(v.Args))

	if failure != "" {
		onWarn(Warn{Kind: WarnBadMacroArgs, Message: fmt.Sprintf("%s: macro '%s': %s\n\tArgs: %s", modulePath, v.Raw.string(), failure, strings.Join(anyStrs(v.Args), ", "))})
	}

	d.confMacroDepth++

	defer func() { d.confMacroDepth-- }()

	for _, call := range macro.calls {
		var args []ANY

		for _, arg := range strings.Split(substConfMacroArgs(call.args, locals, env), " ") {
			if arg != "" {
				args = append(args, internStr(arg).any())
			}
		}

		stmts := appendMacroCallStmts(nil, call.name, args, args, v.Line, func(format string, a ...any) {
			throwFmt("%s: %s: "+format, append([]any{macro.file, call.name.string()}, a...)...)
		})

		collectStmts(ctx, modulePath, kind, language, stmts, env, d, onWarn, platform)
	}
}

func substConfMacroArgs(text string, locals map[string][]string, env Environment) string {
	for i := 0; strings.IndexByte(text, '$') >= 0; i++ {
		next := substConfMacroPass(text, locals, env)

		if next == text {
			break
		}

		if i == 127 {
			throwFmt("Macro subst exceeded the limit 128")
		}

		text = next
	}

	return text
}

func substConfMacroPass(text string, locals map[string][]string, env Environment) string {
	var out strings.Builder

	for i := 0; i < len(text); {
		switch {
		case text[i] == '\\' && i+1 < len(text):
			out.WriteString(text[i : i+2])
			i += 2
		case text[i] != '$':
			out.WriteByte(text[i])
			i++
		case i+1 < len(text) && text[i+1] == '{':
			end := confMacroRefEnd(text, i+2)

			if end < 0 {
				out.WriteString(text[i:])

				return out.String()
			}

			inner := text[i+2 : end]

			switch {
			case strings.IndexByte(inner, '"') >= 0:
				out.WriteString(text[i : end+1])
			case strings.IndexByte(inner, '$') >= 0:
				out.WriteString("${" + substConfMacroPass(inner, locals, env) + "}")
			default:
				out.WriteString(substConfMacroRef(text[i:end+1], inner, locals, env))
			}

			i = end + 1
		default:
			n := confVarNameLen(text[i+1:])

			if n == 0 {
				out.WriteByte('$')
				i++

				continue
			}

			out.WriteString(substConfMacroRef(text[i:i+1+n], text[i+1:i+1+n], locals, env))
			i += 1 + n
		}
	}

	return out.String()
}

func confMacroRefEnd(text string, i int) int {
	for depth := 1; i < len(text); i++ {
		switch text[i] {
		case '{':
			depth++
		case '}':
			depth--

			if depth == 0 {
				return i
			}
		}
	}

	return -1
}

func confVarNameLen(s string) int {
	if s == "" || !isConfIdentByte(s[0], true) {
		return 0
	}

	n := 1

	for n < len(s) {
		if isConfIdentByte(s[n], false) {
			n++

			continue
		}

		dashes := n

		for dashes < len(s) && s[dashes] == '-' {
			dashes++
		}

		if dashes == n || dashes == len(s) || !isConfIdentByte(s[dashes], false) {
			break
		}

		n = dashes
	}

	return n
}

func isConfIdentByte(b byte, first bool) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b == '_' || !first && b >= '0' && b <= '9'
}

func substConfMacroRef(fragment, ref string, locals map[string][]string, env Environment) string {
	mods, name := "", ref

	if k := strings.LastIndexByte(ref, ':'); k >= 0 {
		mods, name = ref[:k], ref[k+1:]
	}

	if confVarNameLen(name) != len(name) {
		return fragment
	}

	values, ok := locals[name]

	if !ok {
		switch name {
		case envBINDIR.string(), envCURDIR.string(), envARCADIA_ROOT.string(), envARCADIA_BUILD_ROOT.string():
			return fragment
		}

		value, bound := env.lookup(name)

		if !bound {
			return fragment
		}

		values = []string{value}
	}

	return applyConfMacroMods(fragment, mods, values)
}

func applyConfMacroMods(fragment, mods string, values []string) string {
	var prefix, suffix, join string

	var joined, hash, upper, lower, noExt, noAllExt, noPath, lastExt bool

	for _, mod := range strings.Split(mods, ";") {
		key, value, _ := strings.Cut(mod, "=")

		switch key {
		case "":
		case "pre":
			prefix = value
		case "suf":
			suffix = value
		case "join":
			join, joined = value, true
		case "hash":
			hash = true
		case "toupper":
			upper = true
		case "tolower":
			lower = true
		case "noext":
			noExt = true
		case "noallext":
			noAllExt = true
		case "nopath":
			noPath = true
		case "lastext":
			lastExt = true
		default:
			return fragment
		}
	}

	parts := make([]string, len(values))

	for i, value := range values {
		slash := strings.LastIndexByte(value, '/') + 1

		if noExt || noAllExt {
			dot := strings.LastIndexByte(value[slash:], '.')

			if noAllExt {
				dot = strings.IndexByte(value[slash:], '.')
			}

			if dot >= 0 {
				value = value[:slash+dot]
			}
		}

		if lastExt {
			if dot := strings.LastIndexByte(value[slash:], '.'); dot >= 0 {
				value = value[slash+dot+1:]
			} else {
				value = ""
			}
		}

		if noPath {
			value = value[strings.LastIndexByte(value, '/')+1:]
		}

		value = prefix + value + suffix

		if upper {
			value = strings.ToUpper(value)
		}

		if lower {
			value = strings.ToLower(value)
		}

		parts[i] = value
	}

	separator := " "

	if joined {
		separator = join
	}

	result := strings.Join(parts, separator)

	if hash {
		result = fmt.Sprintf("%x", md5.Sum([]byte(result)))
	}

	return result
}
