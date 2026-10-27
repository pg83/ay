package main

import (
	"slices"
	"strings"
)

type LinkerFlagValue = CompilerFlagValue

func (e *EmitContext) linkerFlagValue(args []ANY) LinkerFlagValue {
	args = internAnys(splitQuotedWords(strings.ReplaceAll(strings.Join(anyStrs(args), " "), `${"$"}`, "$"), 1))

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

func (e *EmitContext) linkerFlagExports(seen *DeDuper, peers []ResolvedPeer, capacity int, own, flat []ANY, goFlags bool) ([]LinkerFlagValue, []ANY) {
	if capacity == 0 && len(own) == 0 {
		return nil, flat
	}

	values := e.ctx.na.compilerFlags.alloc(capacity + 1)[:0]

	seen.reset()

	collect := func(peer ResolvedPeer) {
		flags := peer.result.LDFlagsGlobal

		if goFlags {
			flags = peer.result.GoLDFlagsGlobal
		}

		for _, value := range flags {
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

type LocalLinkerFlags struct {
	CompilerFlagValue
	Inputs []VFS
}

func (e *EmitContext) localLinkerFlagValue(peerRPath []ANY) LocalLinkerFlags {
	if e.d.localLDFlagsReady {
		return e.d.localLDFlags
	}

	var resources []STR
	var inputs []VFS

	env := Environment{s: &e.frame.ifEnv, resourceUses: &resources}

	var expand func(string, []string) ([]string, string)
	expand = func(text string, expanding []string) ([]string, string) {
		return commandArgumentExpression(text, func(name string, _ bool) ([]string, string) {
			if slices.Contains(expanding, name) {
				throwFmt("self-contradictory variable definition (%s)", name)
			}

			if variable, input := strings.CutPrefix(name, "input:"); input {
				value := variable

				if !strings.HasPrefix(variable, `"`) {
					value = env.string(internEnv(variable))
				}

				args, _ := expand(value, append(expanding, name))

				for i, arg := range args {
					file := e.runProgramInputVFS(arg)

					inputs = append(inputs, file)
					args[i] = file.string()
				}

				return args, name
			}

			if name == "RPATH_GLOBAL" {
				return anyStrs(peerRPath), name
			}

			value, _ := env.lookup(name)
			args, identity := expand(value, append(expanding, name))

			return args, identity
		})
	}

	args, _ := expand(env.string(envLdflags), nil)
	value := resourceGlobalArgumentValue(internAnys(args), e.peers.ResourceGlobals)

	value.Resources = resourceKeyUnion(resources, value.Resources)

	e.d.localLDFlags = LocalLinkerFlags{CompilerFlagValue: value, Inputs: inputs}
	e.d.localLDFlagsReady = true

	return e.d.localLDFlags
}
