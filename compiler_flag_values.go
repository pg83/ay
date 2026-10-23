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
	Name STR
	Args []ANY
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
		values = append(values, e.compilerFlagValue(own))
	}

	e.ctx.na.compilerFlags.commit(len(values))

	return values[:len(values):len(values)], flat
}

func (e *EmitContext) compilerFlagValue(args []ANY) CompilerFlagValue {
	value := compilerCommandFlags(args, Environment{s: &e.frame.ifEnv})

	value.Args = e.ctx.argSlices.internCopy(value.Args)

	return value
}

func (e *EmitContext) ownCompilerFlagValues(args []ANY) []CompilerFlagValue {
	if len(args) == 0 {
		return nil
	}

	return e.ctx.na.compilerFlags.list(e.compilerFlagValue(args))
}

func compilerCommandFlags(args []ANY, env Environment) CompilerFlagValue {
	text := strings.Join(anyStrs(args), " ")

	for _, arg := range args {
		if strings.ContainsAny(arg.string(), "$\\\"'&") || strings.ContainsFunc(arg.string(), unicode.IsSpace) {
			values, identity := compilerFlagExpression(text, env, nil)

			return CompilerFlagValue{Name: internStr(identity), Args: internAnys(values)}
		}
	}

	return CompilerFlagValue{Name: internStr(text), Args: args}
}

func ownCompilerFlagArgs(values []CompilerFlagValue, own []ANY) []ANY {
	if len(own) == 0 {
		return nil
	}

	return values[len(values)-1].Args
}
