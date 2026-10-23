package main

import "strings"

type LinkerFlagValue = CompilerFlagValue

func (e *EmitContext) linkerFlagValue(args []ANY) LinkerFlagValue {
	args = internAnys(splitQuotedWords(strings.Join(anyStrs(args), " "), 1))

	return LinkerFlagValue{
		Name: internStr(strings.Join(anyStrs(args), "\x00")),
		Args: e.ctx.argSlices.internCopy(args),
	}
}

func (e *EmitContext) ownLinkerFlagValues(args []ANY) []LinkerFlagValue {
	if len(args) == 0 {
		return nil
	}

	return e.ctx.na.compilerFlags.list(e.linkerFlagValue(args))
}

func (e *EmitContext) linkerFlagExports(seen *DeDuper, peers []ResolvedPeer, capacity int, own, flat []ANY) ([]LinkerFlagValue, []ANY) {
	if capacity == 0 && len(own) == 0 {
		return nil, flat
	}

	values := e.ctx.na.compilerFlags.alloc(capacity + 1)[:0]

	seen.reset()

	collect := func(peer ResolvedPeer) {
		for _, value := range peer.result.LDFlagsGlobal {
			if seen.addStable(uint32(value.Name)) {
				values = append(values, value)
				flat = append(flat, value.Args...)
			}
		}
	}

	for _, peer := range peers {
		if peer.path == e.instance.Path.relString() {
			collect(peer)
		}
	}

	for _, peer := range peers {
		if peer.path != e.instance.Path.relString() {
			collect(peer)
		}
	}

	if len(own) > 0 {
		values = append(values, e.linkerFlagValue(own))
	}

	e.ctx.na.compilerFlags.commit(len(values))

	return values[:len(values):len(values)], flat
}
