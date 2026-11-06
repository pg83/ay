package main

import (
	"crypto/md5"
	"os"
	"slices"
	"strings"
)

var (
	strSubstBufs       [2][]byte
	scalarVarRefPolicy = &substPolicy{makefile: true, emptyUnknown: true, literal: pathArgumentsReservedVar}
	substVariables     = &substPolicy{literal: rootVar}
	substCommand       = &substPolicy{emptyUnknown: true, command: true}
	rootVarNames       = [...]string{envBINDIR.string(), envCURDIR.string(), envARCADIA_ROOT.string(), envARCADIA_BUILD_ROOT.string()}
)

var macroModifierNames = map[string]macroFlags{
	"input": modInput, "tool": modTool, "output_include": modOutputInclude, "tmp": modTmp, "pre": modPrefix,
	"suf": modSuffix, "ext": modExtFilter, "stdout": modStdout, "cwd": modCwd, "env": modEnv, "noext": modCutExt,
	"noauto": modNoAuto, "hide": modHide, "tobindir": modToBinDir, "notransformbuilddir": modNoTransformBuildDir,
	"norel": modNoRel, "join": modJoin, "rootrel": modRootRel, "rootdir": modRootDir, "quo": modQuote,
	"bq": modBreakQuote, "qe": modQuoteEach, "bksl": modBackslash, "addincl": modAddIncl, "defext": modDefaultExt,
	"noallext": modCutAllExt, "nopath": modCutPath, "toupper": modToUpper, "tolower": modToLower,
	"dirallowed": modDirAllowed, "cmdsep": modCmdSep, "add_to_outs": modAddToOuts, "kv": modKeyValue,
	"result": modResult, "lastext": modLastExt, "main": modMain, "requirements": modRequirements,
	"modlocal": modModLocal, "comma": modComma, "tags_in": modTagsIn, "tags_out": modTagsOut, "resource": modResource,
	"context": modContext, "global": modGlobal, "tared": modTared, "hash": modHash, "induced_deps": modInducedDeps,
	"to_namespace": modNamespace, "skip_by_ext": modSkipByExt, "from_input": modFromInput,
}

const (
	modInput macroFlags = 1 << iota
	modTool
	modOutputInclude
	modTmp
	modPrefix
	modSuffix
	modExtFilter
	modStdout
	modCwd
	modEnv
	modCutExt
	modNoAuto
	modHide
	modToBinDir
	modNoTransformBuildDir
	modNoRel
	modJoin
	modRootRel
	modRootDir
	modQuote
	modBreakQuote
	modQuoteEach
	modBackslash
	modAddIncl
	modDefaultExt
	modCutAllExt
	modCutPath
	modToUpper
	modToLower
	modDirAllowed
	modCmdSep
	modAddToOuts
	modKeyValue
	modResult
	modLastExt
	modMain
	modRequirements
	modModLocal
	modComma
	modTagsIn
	modTagsOut
	modResource
	modContext
	modGlobal
	modTared
	modHash
	modInducedDeps
	modNamespace
	modSkipByExt
	modFromInput
	modUnknown
)

type substPolicy struct {
	makefile      bool
	command       bool
	emptyUnknown  bool
	keepModifiers bool
	literal       func(name string) bool
	reserved      func(name string) bool
	track         *SourceTextTracker
}

func rootVar(name string) bool {
	return slices.Contains(rootVarNames[:], name)
}

type macroFlags uint64

type macroRef struct {
	start, end int
	name       string
	flags      macroFlags
	raw        bool
	process    bool
	prefix     string
	suffix     string
	join       string
	ext        string
	skipExt    string
}

func (ref *macroRef) setMod(name, value string) {
	flag, known := macroModifierNames[name]

	if !known {
		flag = modUnknown
	}

	switch flag {
	case modPrefix:
		ref.prefix = value
	case modSuffix, modDefaultExt:
		ref.suffix = value
	case modJoin:
		ref.join = value
	case modExtFilter:
		ref.ext = value
	case modSkipByExt:
		ref.skipExt = value
	case modToUpper:
		if ref.flags&modToLower != 0 {
			return
		}
	case modToLower:
		if ref.flags&modToUpper != 0 {
			return
		}
	}

	ref.flags |= flag
}

func isMacroAlnum(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

func macroString(text string, i int) (int, bool) {
	for i++; i < len(text); i++ {
		switch text[i] {
		case '"':
			return i + 1, true
		case '\\':
			i++
		}
	}

	return i, false
}

func cmdIdentLen(s string) int {
	if s == "" || !(s[0] == '_' || s[0] >= 'a' && s[0] <= 'z' || s[0] >= 'A' && s[0] <= 'Z') {
		return 0
	}

	n := 1

	for n < len(s) && (s[n] == '_' || isMacroAlnum(s[n])) {
		n++
	}

	return n
}

func scanMacroRef(text string, i int, command bool) (macroRef, bool) {
	ref := macroRef{start: i}
	j := i + 1
	nameLen := confVarNameLen

	if command {
		nameLen = cmdIdentLen
	}

	if j >= len(text) {
		return ref, false
	}

	if text[j] != '{' {
		n := nameLen(text[j:])

		if n == 0 {
			return ref, false
		}

		ref.name, ref.end = text[j:j+n], j+n

		return ref, true
	}

	j++

	k := j

	for {
		nameStart := k

		for k < len(text) && (text[k] >= 'a' && text[k] <= 'z' || text[k] == '_') {
			k++
		}

		if k == nameStart {
			break
		}

		modName := text[nameStart:k]
		modValue := ""

		if k < len(text) && text[k] == '=' {
			k++

			valueStart := k

			for k < len(text) {
				switch text[k] {
				case ';', ':':
				case '\\':
					k += 2

					continue
				case '"':
					end, ok := macroString(text, k)

					if !ok {
						return ref, false
					}

					k = end

					continue
				default:
					k++

					continue
				}

				break
			}

			modValue = text[valueStart:k]
		}

		if k < len(text) && text[k] == ';' {
			ref.setMod(modName, modValue)
			k++

			continue
		}

		if k < len(text) && text[k] == ':' {
			ref.setMod(modName, modValue)
			j = k + 1
		}

		break
	}

	if j < len(text) && text[j] == '"' {
		end, ok := macroString(text, j)

		if !ok {
			return ref, false
		}

		ref.name, ref.raw = text[j+1:end-1], true
		j = end
	} else {
		n := nameLen(text[j:])

		if n == 0 {
			return ref, false
		}

		ref.name = text[j : j+n]
		j += n
	}

	if j < len(text) && text[j] == '.' {
		suffixStart := j

		for j < len(text) && text[j] == '.' {
			for j < len(text) && text[j] == '.' {
				j++
			}

			if j >= len(text) || !isMacroAlnum(text[j]) {
				return ref, false
			}

			for j < len(text) && isMacroAlnum(text[j]) {
				j++
			}
		}

		ref.setMod("suf", text[suffixStart:j])
	}

	if j >= len(text) || text[j] != '}' {
		return ref, false
	}

	ref.end = j + 1

	return ref, true
}

func scanMakefileRef(text string, i int) (macroRef, bool) {
	ref := macroRef{start: i}
	j := i + 1

	for j < len(text) && isMacroAlnum(text[j]) {
		j++
	}

	if j >= len(text) || text[j] != '{' {
		return ref, false
	}

	macro := text[i+1 : j]
	k := j + 1

	for k < len(text) && text[k] != '$' && text[k] != '{' && text[k] != '}' {
		k++
	}

	if k == j+1 || k >= len(text) || text[k] != '}' {
		return ref, false
	}

	ref.name, ref.end = text[j+1:k], k+1

	switch macro {
	case "":
	case "ENV":
		ref.process = true
	default:
		ref.raw = true
	}

	return ref, true
}

type macroValues struct {
	list   []ANY
	single string
	vfs    VFS
	one    bool
}

func (v macroValues) count() int {
	if v.one {
		return 1
	}

	return len(v.list)
}

func (v macroValues) at(i int) string {
	if v.vfs != 0 {
		return v.vfs.string()
	}

	if v.one {
		return v.single
	}

	return v.list[i].string()
}

func macroLookup(name string, locals []confMacroBinding, env Environment, constant *bool, policy *substPolicy) (macroValues, bool) {
	if len(locals) != 0 {
		if id := interned(name); id != 0 {
			for i := range locals {
				if locals[i].name == id && locals[i].bound {
					return macroValues{list: locals[i].values}, true
				}
			}
		}
	}

	if env.s == nil {
		return macroValues{}, false
	}

	if constant != nil {
		*constant = false
	}

	value, vfs, bound := env.lookupValue(name)

	if !bound {
		return macroValues{}, false
	}

	return macroValues{single: value, vfs: vfs, one: true}, true
}

func applyMacroMods(ref *macroRef, value, prefix, suffix string) (string, bool) {
	flags := ref.flags

	if flags&modComma != 0 {
		return ",", true
	}

	if flags&(modKeyValue|modRequirements) != 0 {
		return "", false
	}

	if ref.ext != "" && !strings.HasSuffix(value, ref.ext) {
		return "", false
	}

	if ref.skipExt != "" && strings.HasSuffix(value, ref.skipExt) {
		return "", false
	}

	if flags&(modCutExt|modCutAllExt) != 0 {
		slash := max(strings.LastIndexByte(value, '/'), 0)
		dot := strings.LastIndexByte(value, '.')

		if flags&modCutAllExt != 0 {
			dot = strings.IndexByte(value[slash:], '.')

			if dot >= 0 {
				dot += slash
			}
		}

		if dot >= slash {
			value = value[:dot]
		}
	}

	if flags&modLastExt != 0 {
		slash := strings.LastIndexByte(value, '/')
		dot := strings.LastIndexByte(value, '.')

		if dot >= 0 && slash < dot {
			value = value[dot+1:]
		} else {
			value = ""
		}
	}

	if flags&modCutPath != 0 {
		value = value[strings.LastIndexByte(value, '/')+1:]
	}

	if flags&modDefaultExt != 0 {
		dot := strings.LastIndexByte(value, '.')
		slash := strings.LastIndexByte(value, '/')

		if dot >= 0 && dot > slash {
			suffix = ""
		}
	}

	if prefix != "" || suffix != "" {
		value = prefix + value + suffix
	}

	if flags&modToUpper != 0 {
		value = strings.ToUpper(value)
	}

	if flags&modToLower != 0 {
		value = strings.ToLower(value)
	}

	return value, true
}

func substMacro(text string, locals []confMacroBinding, env Environment, constant *bool, policy *substPolicy) string {
	out, changed := substMacroAppend(nil, text, locals, env, constant, policy)

	if !changed {
		return text
	}

	return bytesString(out)
}

func substMacroAppend(out []byte, text string, locals []confMacroBinding, env Environment, constant *bool, policy *substPolicy) ([]byte, bool) {
	base := len(out)
	copied := 0

	for i := 0; i < len(text); {
		switch text[i] {
		case '\\':
			if policy.makefile && i+1 < len(text) {
				if copied == 0 {
					out = slices.Grow(out, len(text)+len(text)/2+32)
				}

				if policy.track != nil {
					policy.track.replace(len(out)-base+i-copied, len(out)-base+i-copied+1, 0, nil)
				}

				out = append(out, text[copied:i]...)
				copied = i + 1
			}

			i += 2

			continue
		case '$':
		default:
			i++

			continue
		}

		if policy.track.literal(len(out) - base + i - copied) {
			i++

			continue
		}

		var ref macroRef

		var ok bool

		if policy.makefile {
			ref, ok = scanMakefileRef(text, i)
		} else {
			ref, ok = scanMacroRef(text, i, policy.command)
		}

		if !ok {
			i++

			continue
		}

		i = ref.end

		if ref.raw || policy.keepModifiers && ref.flags != 0 {
			continue
		}

		var values macroValues

		var bound bool

		if ref.process {
			values, bound = macroValues{single: os.Getenv(ref.name), one: true}, true
		} else if policy.literal != nil && policy.literal(ref.name) {
			continue
		} else {
			values, bound = macroLookup(ref.name, locals, env, constant, policy)
		}

		if !bound && (!policy.emptyUnknown || policy.reserved != nil && policy.reserved(ref.name)) {
			continue
		}

		if copied == 0 {
			out = slices.Grow(out, len(text)+len(text)/2+32)
		}

		out = append(out, text[copied:ref.start]...)
		copied = ref.end

		at := len(out) - base

		var inserted []SourceTextRange

		if policy.track != nil && bound && !ref.process {
			inserted = env.s.textInputs[internedEnv(ref.name)]
		}

		if !bound || ref.flags&modHide != 0 {
			if policy.track != nil {
				policy.track.replace(at, at+ref.end-ref.start, 0, nil)
			}

			continue
		}

		prefix, suffix := ref.prefix, ref.suffix

		if strings.IndexByte(prefix, '$') >= 0 {
			prefix = substMacroDeeply(prefix, locals, env, constant, policy)
		}

		if strings.IndexByte(suffix, '$') >= 0 {
			suffix = substMacroDeeply(suffix, locals, env, constant, policy)
		}

		separator := " "

		if ref.flags&modJoin != 0 {
			separator = ref.join
		}

		mark := len(out)
		quoted := ref.flags&modQuote != 0

		if quoted {
			out = append(out, '"')
		}

		first := true

		for k := 0; k < values.count(); k++ {
			if values.vfs != 0 && ref.flags == 0 && ref.ext == "" && ref.skipExt == "" {
				out = append(appendANYText(append(out, prefix...), values.vfs.any()), suffix...)
				first = false

				continue
			}

			value, keep := applyMacroMods(&ref, values.at(k), prefix, suffix)

			if !keep {
				continue
			}

			if !first {
				out = append(out, separator...)
			}

			out = append(out, value...)
			first = false
		}

		if quoted {
			out = append(out, '"')
		}

		if ref.flags&modHash != 0 {
			sum := md5.Sum(out[mark:])

			out = out[:mark]

			for _, b := range sum {
				out = append(out, hex[b>>4], hex[b&15])
			}
		}

		if policy.track != nil {
			policy.track.replace(at, at+ref.end-ref.start, len(out)-base-at, inserted)
		}
	}

	if copied == 0 {
		return out, false
	}

	return append(out, text[copied:]...), true
}

func substMacroDeeply(text string, locals []confMacroBinding, env Environment, constant *bool, policy *substPolicy) string {
	if strings.IndexByte(text, '$') < 0 && (!policy.makefile || strings.IndexByte(text, '\\') < 0) {
		return text
	}

	if policy.makefile {
		return substMacro(text, locals, env, constant, policy)
	}

	for iteration := 1; ; iteration++ {
		next := substMacro(text, locals, env, constant, policy)

		if next == text || strings.IndexByte(next, '$') < 0 {
			return next
		}

		if iteration >= 128 {
			throwFmt("Macro subst exceeded the limit 128")
		}

		text = next
	}
}

func substMacroDeeplyInto(text string, locals []confMacroBinding, env Environment, constant *bool, policy *substPolicy, bufs *[2][]byte) string {
	if strings.IndexByte(text, '$') < 0 && (!policy.makefile || strings.IndexByte(text, '\\') < 0) {
		return text
	}

	for iteration := 1; ; iteration++ {
		k := iteration & 1
		next, changed := substMacroAppend(bufs[k][:0], text, locals, env, constant, policy)

		bufs[k] = next[:0]

		if !changed {
			return text
		}

		if policy.makefile || strings.IndexByte(bytesString(next), '$') < 0 || bytesString(next) == text {
			return bytesString(next)
		}

		if iteration >= 128 {
			throwFmt("Macro subst exceeded the limit 128")
		}

		text = bytesString(next)
	}
}

func (env Environment) subst(text string, locals []confMacroBinding, constant *bool) string {
	return substMacroDeeply(text, locals, env, constant, substVariables)
}

func (env Environment) expandVariableSTR(text string) STR {
	return internStr(substMacroDeeplyInto(text, nil, env, nil, substVariables, &strSubstBufs))
}

func (env Environment) expandVariable(text string) string {
	return substMacroDeeply(text, nil, env, nil, substVariables)
}

func (env Environment) expand(text string) string {
	return substMacroDeeply(text, nil, env, nil, substCommand)
}

func bindingsOf(parameters map[string][]string) []confMacroBinding {
	if len(parameters) == 0 {
		return nil
	}

	locals := make([]confMacroBinding, 0, len(parameters))

	for name, values := range parameters {
		locals = append(locals, confMacroBinding{name: internStr(name), values: internAnys(values), bound: true})
	}

	return locals
}

func macroRefs(text string) func(yield func(macroRef) bool) {
	return func(yield func(macroRef) bool) {
		for i := 0; i < len(text); {
			switch text[i] {
			case '\\':
				i += 2

				continue
			case '$':
			default:
				i++

				continue
			}

			ref, ok := scanMacroRef(text, i, false)

			if !ok {
				i++

				continue
			}

			if !yield(ref) {
				return
			}

			i = ref.end
		}
	}
}

func macroArgsSeq(text string) func(yield func(string) bool) {
	return func(yield func(string) bool) {
		for arg := range strings.SplitSeq(text, " ") {
			if arg != "" && !yield(arg) {
				return
			}
		}
	}
}

func commandVariableArgs(env Environment, name ENV) []ANY {
	value := env.string(name)

	if value == "" {
		return nil
	}

	return internAnys(splitShellWords(env.expand(value)))
}
