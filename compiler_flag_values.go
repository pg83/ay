package main

import "strings"

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

func (ctx *GenCtx) compilerFlagExports(seen *DeDuper, peers []ResolvedPeer, kind CompilerFlagKind, capacity int, own, flat []ANY) ([]CompilerFlagValue, []ANY) {
	if capacity == 0 && len(own) == 0 {
		return nil, flat
	}

	values := ctx.na.compilerFlags.alloc(capacity + 1)[:0]

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
		values = append(values, ctx.compilerFlagValue(own))
	}

	ctx.na.compilerFlags.commit(len(values))

	return values[:len(values):len(values)], flat
}

func (ctx *GenCtx) compilerFlagValue(args []ANY) CompilerFlagValue {
	args = ctx.argSlices.internCopy(args)

	return CompilerFlagValue{Name: internStr(strings.Join(anyStrs(args), " ")), Args: args}
}

func (ctx *GenCtx) ownCompilerFlagValues(args []ANY) []CompilerFlagValue {
	if len(args) == 0 {
		return nil
	}

	return ctx.na.compilerFlags.list(ctx.compilerFlagValue(args))
}
