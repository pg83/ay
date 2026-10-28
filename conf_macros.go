package main

import (
	"crypto/md5"
	"fmt"
	"slices"
	"strings"
)

var (
	confMacroArgsMemo        DenseMap[STR, []ANY]
	confMacroBindingsArena   = newBumpAllocator[confMacroBinding]()
	confMacroExpansionsArena = newBumpAllocator[confMacroExpansion]()
	confMacroCallsArena      = newBumpAllocator[confMacroCallExpansion]()
)

const confMacroCallStackSize = 100

const (
	confKeywordFlag ConfKeywordKind = iota
	confKeywordScalar
	confKeywordArray
)

type ConfKeywordKind uint8

type ConfKeyword struct {
	name    STR
	kind    ConfKeywordKind
	present []ANY
	missing []ANY
}

type ConfMacroSignature struct {
	keywords   []ConfKeyword
	positional []STR
	vararg     STR
}

type ConfMacroCall struct {
	name STR
	args STR
}

type ConfMacro struct {
	file       string
	sig        ConfMacroSignature
	calls      []ConfMacroCall
	expansions *confMacroExpansion
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
			body.calls = append(body.calls, ConfMacroCall{name: internStr(name), args: internStr(strings.TrimSpace(args))})

			return
		}
	}

	body.calls = nil
	body.unsupported = true
}

func parseConfMacroSignature(rel, params string) (sig ConfMacroSignature, specialized bool) {
	split := splitConfMacroParams(params, confReadScratch.params[:0])

	defer func() { confReadScratch.params = split[:0] }()

	for _, param := range split {
		if strings.HasPrefix(param, `"`) {
			specialized = true

			continue
		}

		if vararg, ok := strings.CutSuffix(param, "..."); ok {
			sig.vararg = internStr(vararg)

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
			sig.positional = append(sig.positional, internStr(name))
		case rest == "[]":
			sig.keywords = append(sig.keywords, ConfKeyword{name: internStr(name), kind: confKeywordArray})
		case strings.HasPrefix(rest, "?"):
			keyword := ConfKeyword{name: internStr(name), kind: confKeywordFlag, present: []ANY{internStr(name).any()}}

			if init := strings.TrimSpace(rest[1:]); init != "" {
				onTrue, rest := cutConfQuoted(rel, init)
				onFalse, rest := cutConfQuoted(rel, strings.TrimPrefix(strings.TrimSpace(rest), ":"))

				if strings.TrimSpace(rest) != "" {
					throwFmt("conf: %s: malformed macro parameter %q", rel, param)
				}

				if onTrue != "" {
					keyword.present = []ANY{internStr(onTrue).any()}
				}

				if onFalse != "" {
					keyword.missing = []ANY{internStr(onFalse).any()}
				}
			}

			sig.keywords = append(sig.keywords, keyword)
		case strings.HasPrefix(rest, "="):
			keyword := ConfKeyword{name: internStr(name), kind: confKeywordScalar}
			value, tail := cutConfQuoted(rel, rest[1:])

			if strings.TrimSpace(tail) != "" {
				throwFmt("conf: %s: malformed macro parameter %q", rel, param)
			}

			if value != "" {
				keyword.missing = []ANY{internStr(value).any()}
			}

			sig.keywords = append(sig.keywords, keyword)
		default:
			throwFmt("conf: %s: malformed macro parameter %q", rel, param)
		}
	}

	return sig, specialized
}

func splitConfMacroParams(params string, out []string) []string {
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

type confMacroBinding struct {
	name   STR
	values []ANY
	bound  bool
}

type confMacroCallExpansion struct {
	args     []ANY
	constant bool
}

type confMacroExpansion struct {
	next    *confMacroExpansion
	args    []ANY
	locals  []confMacroBinding
	calls   []confMacroCallExpansion
	failure string
}

func (macro *ConfMacro) bind(args []ANY) *confMacroExpansion {
	for expansion := macro.expansions; expansion != nil; expansion = expansion.next {
		if slices.Equal(expansion.args, args) {
			return expansion
		}
	}

	expansion := confMacroExpansionsArena.one()

	expansion.next = macro.expansions
	expansion.args = astArgs.list(args...)
	expansion.locals, expansion.failure = macro.sig.bind(expansion.args)
	expansion.calls = confMacroCallsArena.alloc(len(macro.calls))[:len(macro.calls):len(macro.calls)]
	confMacroCallsArena.commit(len(macro.calls))
	macro.expansions = expansion

	return expansion
}

func (sig *ConfMacroSignature) keyword(arg ANY) int {
	for i := range sig.keywords {
		if sig.keywords[i].name.any() == arg {
			return i
		}
	}

	return -1
}

func (sig *ConfMacroSignature) arrayEnd(args []ANY, i int) int {
	for i < len(args) && sig.keyword(args[i]) < 0 {
		i++
	}

	return i
}

func (binding *confMacroBinding) append(values []ANY) {
	if len(binding.values) == 0 {
		binding.values = values
	} else if len(values) > 0 {
		block := astArgs.alloc(len(binding.values) + len(values))
		n := copy(block, binding.values)

		n += copy(block[n:], values)
		astArgs.commit(n)
		binding.values = block[:n:n]
	}

	binding.bound = true
}

func (sig *ConfMacroSignature) bind(args []ANY) ([]confMacroBinding, string) {
	n := len(sig.keywords) + len(sig.positional)

	if sig.vararg != 0 {
		n++
	}

	locals := confMacroBindingsArena.alloc(n)[:n:n]

	defer confMacroBindingsArena.commit(n)

	for i, keyword := range sig.keywords {
		locals[i].name = keyword.name
	}

	for i, name := range sig.positional {
		locals[len(sig.keywords)+i].name = name
	}

	if sig.vararg != 0 {
		locals[n-1].name = sig.vararg
	}

	next, array := 0, STR(0)

	for i := 0; i < len(args) || next < len(sig.positional); {
		if i == len(args) {
			return locals, fmt.Sprintf("Value for '%s' argument is missing.", sig.positional[next].string())
		}

		if k := sig.keyword(args[i]); k >= 0 {
			keyword := sig.keywords[k]
			binding := &locals[k]

			if binding.bound && keyword.kind != confKeywordArray {
				return locals, fmt.Sprintf("Keyword '%s' appears more than once and is not an array.", keyword.name.string())
			}

			i++

			switch keyword.kind {
			case confKeywordFlag:
				binding.append(keyword.present)
			case confKeywordScalar:
				if i == len(args) || sig.keyword(args[i]) >= 0 {
					return locals, fmt.Sprintf("Missing value for scalar keyword '%s'.", keyword.name.string())
				}

				binding.append(args[i : i+1])
				i++
			case confKeywordArray:
				end := sig.arrayEnd(args, i)

				array = keyword.name
				binding.append(args[i:end])
				i = end
			}

			continue
		}

		if array != 0 {
			return locals, fmt.Sprintf("Mixing positional arguments with named arrays is error prone and forbidden.\n\tGot positional arg '%s' after named array '%s' has been started.", args[i].string(), array.string())
		}

		if next < len(sig.positional) {
			locals[len(sig.keywords)+next].append(args[i : i+1])
			next++
			i++

			continue
		}

		if sig.vararg == 0 {
			return locals, fmt.Sprintf("More positional arguments passed than expected. Can't handle arg: '%s'.", args[i].string())
		}

		end := sig.arrayEnd(args, i)

		locals[n-1].append(args[i:end])
		i = end
	}

	for i, keyword := range sig.keywords {
		if !locals[i].bound {
			locals[i].append(keyword.missing)
		}
	}

	if sig.vararg != 0 {
		locals[n-1].bound = true
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

	expansion := macro.bind(v.Args)
	locals, failure := expansion.locals, expansion.failure

	if failure != "" {
		onWarn(Warn{Kind: WarnBadMacroArgs, Message: fmt.Sprintf("%s: macro '%s': %s\n\tArgs: %s", modulePath, v.Raw.string(), failure, strings.Join(anyStrs(v.Args), ", "))})
	}

	d.confMacroDepth++

	defer func() { d.confMacroDepth-- }()

	for i, call := range macro.calls {
		cached := &expansion.calls[i]
		args := cached.args

		if !cached.constant {
			text := call.args
			constant := true

			if strHasDollar(text.any()) {
				text = internStr(substConfMacroArgs(text.string(), locals, env, &constant))
			}

			args = confMacroArgs(text)

			if constant {
				cached.args, cached.constant = args, true
			}
		}

		var scratch [1]Stmt

		stmts := appendMacroCallStmts(scratch[:0], call.name, args, args, v.Line, func(format string, a ...any) {
			throwFmt("%s: %s: "+format, append([]any{macro.file, call.name.string()}, a...)...)
		})

		collectStmts(ctx, modulePath, kind, language, stmts, env, d, onWarn, platform)
	}
}

func confMacroArgs(text STR) []ANY {
	if args, ok := confMacroArgsMemo.get(text); ok {
		return args
	}

	value := text.string()
	block := astArgs.alloc(strings.Count(value, " ") + 1)
	n := 0

	for arg := range strings.SplitSeq(value, " ") {
		if arg != "" {
			block[n] = internStr(arg).any()
			n++
		}
	}

	astArgs.commit(n)

	args := block[:n:n]

	confMacroArgsMemo.put(text, args)

	return args
}

func substConfMacroArgs(text string, locals []confMacroBinding, env Environment, constant *bool) string {
	for i := 0; strings.IndexByte(text, '$') >= 0; i++ {
		next := substConfMacroPass(text, locals, env, constant)

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

func substConfMacroPass(text string, locals []confMacroBinding, env Environment, constant *bool) string {
	var out strings.Builder

	copied := 0

	replace := func(start, end int, value string) {
		if value != text[start:end] {
			out.WriteString(text[copied:start])
			out.WriteString(value)
			copied = end
		}
	}

	for i := 0; i < len(text); {
		switch {
		case text[i] == '\\' && i+1 < len(text):
			i += 2
		case text[i] != '$':
			i++
		case i+1 < len(text) && text[i+1] == '{':
			end := confMacroRefEnd(text, i+2)

			if end < 0 {
				i = len(text)

				continue
			}

			inner := text[i+2 : end]

			switch {
			case strings.IndexByte(inner, '"') >= 0:
			case strings.IndexByte(inner, '$') >= 0:
				replace(i, end+1, "${"+substConfMacroPass(inner, locals, env, constant)+"}")
			default:
				replace(i, end+1, substConfMacroRef(text[i:end+1], inner, locals, env, constant))
			}

			i = end + 1
		default:
			n := confVarNameLen(text[i+1:])

			if n == 0 {
				i++

				continue
			}

			replace(i, i+1+n, substConfMacroRef(text[i:i+1+n], text[i+1:i+1+n], locals, env, constant))
			i += 1 + n
		}
	}

	if copied == 0 {
		return text
	}

	out.WriteString(text[copied:])

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

func substConfMacroRef(fragment, ref string, locals []confMacroBinding, env Environment, constant *bool) string {
	mods, name := "", ref

	if k := strings.LastIndexByte(ref, ':'); k >= 0 {
		mods, name = ref[:k], ref[k+1:]
	}

	if confVarNameLen(name) != len(name) {
		return fragment
	}

	var values []ANY

	bound := false
	id := interned(name)

	for _, binding := range locals {
		if binding.name == id && binding.bound {
			values, bound = binding.values, true

			break
		}
	}

	if !bound {
		switch name {
		case envBINDIR.string(), envCURDIR.string(), envARCADIA_ROOT.string(), envARCADIA_BUILD_ROOT.string():
			return fragment
		}

		*constant = false

		value, bound := env.lookup(name)

		if !bound {
			return fragment
		}

		values = []ANY{internStr(value).any()}
	}

	return applyConfMacroMods(fragment, mods, values)
}

func applyConfMacroMods(fragment, mods string, values []ANY) string {
	if mods == "" {
		return internAnyWords(values).string()
	}

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

	for i, arg := range values {
		value := arg.string()
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
