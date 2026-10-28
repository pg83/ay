package main

import (
	"strings"
	"unicode"
)

var literalCompilerFlagMemo TwoBitSet

const (
	compilerFlagsCommon CompilerFlagKind = iota
	compilerFlagsCxx
	compilerFlagsCOnly
)

type CompilerFlagValue struct {
	Resources []STR
	Name      STR
	Args      []ANY
}

type CompilerFlagKind uint8

func (r *ModuleEmitResult) compilerFlagValues(kind CompilerFlagKind) []CompilerFlagValue {
	switch kind {
	case compilerFlagsCommon:
		return r.CFlagsGlobal
	case compilerFlagsCxx:
		return r.CXXFlagsGlobal
	default:
		return r.COnlyFlagsGlobal
	}
}

func (e *EmitContext) compilerFlagExports(seen *DeDuper, peers []ResolvedPeer, kind CompilerFlagKind, capacity int, own, flat []ANY) ([]CompilerFlagValue, []ANY) {
	if capacity == 0 && len(own) == 0 {
		return nil, flat
	}

	values := e.ctx.na.compilerFlags.alloc(capacity + 1)[:0]

	seen.reset()

	for _, peer := range peers {
		for _, value := range peer.result.compilerFlagValues(kind) {
			if seen.addStable(uint32(value.Name)) {
				values = append(values, value)
				flat = append(flat, value.Args...)
			}
		}
	}

	if len(own) > 0 {
		values = append(values, e.compilerFlagValue(own, kind))
	}

	e.ctx.na.compilerFlags.commit(len(values))

	return values[:len(values):len(values)], flat
}

func (e *EmitContext) compilerFlagValue(args []ANY, kind CompilerFlagKind) CompilerFlagValue {
	value := compilerCommandFlags(args, Environment{s: &e.frame.ifEnv})

	value.Args = e.ctx.argSlices.internCopy(value.Args)
	value.Resources = resourceKeyUnion(value.Resources, e.d.compilerFlagUses[kind].Global)

	return value
}

func (e *EmitContext) ownCompilerFlagValues(args []ANY, kind CompilerFlagKind) []CompilerFlagValue {
	if len(args) == 0 {
		return nil
	}

	return e.ctx.na.compilerFlags.list(e.compilerFlagValue(args, kind))
}

func compilerCommandFlags(args []ANY, env Environment) CompilerFlagValue {
	var resources []STR

	parentUses := env.resourceUses

	env.resourceUses = &resources

	identity := internAnyWords(args)

	for _, arg := range args {
		text := arg.relOrSelf().string()

		if arg.vfs() != 0 || strings.ContainsAny(text, "$\\\"'&") || strings.ContainsFunc(text, unicode.IsSpace) {
			values, identity := compilerFlagExpression(identity.string(), env, compilerFlagExpanding[:0])

			if parentUses != nil {
				*parentUses = resourceKeyUnion(*parentUses, resources)
			}

			return CompilerFlagValue{Name: internStr(identity), Args: internAnys(values), Resources: resources}
		}
	}

	return CompilerFlagValue{Name: identity, Args: args}
}

func ownCompilerFlagArgs(values []CompilerFlagValue, own []ANY) []ANY {
	if len(own) == 0 {
		return nil
	}

	return values[len(values)-1].Args
}

type CompilerFlagResourceUses struct {
	Global  []STR
	Private []STR
}

type compilerFlagResourceOrigin struct {
	key   STR
	track SourceTextTracker
}

func literalCompilerFlag(arg ANY) bool {
	if arg.vfs() != 0 {
		return false
	}

	id := arg.str()

	if state := literalCompilerFlagMemo.get(uint32(id)); state != 0 {
		return state == 1
	}

	text := id.string()
	literal := text != "" && !strings.ContainsAny(text, "$ \t\r\n\\\"'&;")
	state := uint8(2)

	if literal {
		state = 1
	}

	literalCompilerFlagMemo.set(uint32(id), state)

	return literal
}

func appendLiteralCompilerFlags(args []ANY, env Environment, name string, allGlobal bool, global, own []ANY) ([]ANY, []ANY) {
	globalStart, ownStart := len(global), len(own)
	nextGlobal := false

	for _, arg := range args {
		if arg == kwGLOBAL.any() {
			nextGlobal = true

			continue
		}

		if allGlobal || nextGlobal {
			global = append(global, arg)
		} else {
			own = append(own, arg)
		}

		nextGlobal = false
	}

	if len(own) > ownStart {
		appendMacroEnv(env, name, internAnyWords(own[ownStart:]).string())
	}

	if len(global) > globalStart {
		appendMacroEnv(env, name+"_GLOBAL", internAnyWords(global[globalStart:]).string())
	}

	return global, own
}

func compilerFlagForwardingWithoutRoots(value string, env Environment, parameters map[string][]string) (string, []compilerFlagResourceOrigin) {
	roots := [...]ENV{envARCADIA_ROOT, envARCADIA_BUILD_ROOT, envCURDIR, envBINDIR}

	var kinds [len(roots)]EnvKind

	for i, root := range roots {
		kinds[i] = env.unbind(root)
	}

	defer func() {
		for i, root := range roots {
			env.rebind(root, kinds[i])
		}
	}()

	return compilerFlagForwarding(value, env, parameters)
}

func compilerFlagForwarding(value string, env Environment, parameters map[string][]string) (string, []compilerFlagResourceOrigin) {
	var keys []STR
	env.resourceUses = &keys

	body := continueConfigMacroBody(value, env, 1, parameters)

	var origins []compilerFlagResourceOrigin

	for _, key := range keys {
		store := *env.s

		store.textInputs = make(map[ENV][]SourceTextRange)

		for _, bound := range store.resourceBindings {
			if bound.key == key {
				store.textInputs[bound.id] = []SourceTextRange{{0, len(env.string(bound.id))}}
			}
		}

		origin := compilerFlagResourceOrigin{key: key}

		continueConfigMacroBodyTracked(value, Environment{s: &store}, 1, parameters, &origin.track)
		origins = append(origins, origin)
	}

	return body, origins
}

func compilerFlagWordResources(origins []compilerFlagResourceOrigin, span SourceTextRange) []STR {
	var keys []STR

	for _, origin := range origins {
		if origin.track.contains(span.Start, span.End) {
			keys = append(keys, origin.key)
		}
	}

	return keys
}
