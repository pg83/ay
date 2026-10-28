package main

import (
	"strings"
	"unicode"
)

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
			values, identity := compilerFlagExpression(identity.string(), env, nil)

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

func compilerFlagForwarding(value string, env Environment, parameters map[string][]string) (string, []compilerFlagResourceOrigin) {
	var keys []STR
	env.resourceUses = &keys

	body := continueConfigMacroBody(value, env, 1, parameters)

	var origins []compilerFlagResourceOrigin

	for _, key := range keys {
		store := *env.s

		store.textInputs = make(map[ENV][]SourceTextRange)

		for id, bound := range store.resourceBindings {
			if bound == key {
				store.textInputs[ENV(id)] = []SourceTextRange{{0, len(env.string(ENV(id)))}}
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
