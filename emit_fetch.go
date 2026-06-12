package main

// emitResourceFetch emits the FETCH node that downloads one external resource
// declared by a RESOURCES_LIBRARY (DECLARE_EXTERNAL_RESOURCE / _HOST_RESOURCES_*)
// into its $(B)/resources/<Name> directory, returning the node's NodeRef. The
// node is emitted at most once per resource name across the whole run (deduped
// through ctx.fetchRefs); every consumer that splices $(<Name>) into a command
// takes the fetch as a dependency from there.
func emitResourceFetch(ctx *GenCtx, decl ResourceDecl) NodeRef {
	if ref, ok := ctx.fetchRefs.get(decl.Name); ok {
		return ref
	}

	output := build("resources/" + decl.Name.string())
	node := &Node{
		Platform: ctx.host,
		Cmds: []Cmd{{
			CmdArgs: ArgChunks{[]STR{
				internStr(currentYatoolPath()),
				argFetch.str(),
				argB.str(),
				argS.str(),
				decl.URI,
				output.str(),
			}},
		}},
		Inputs:           InputChunks{fetchScriptInputs(ctx.scripts)},
		KV:               KV{P: pkFETCH, PC: pcYellow, ShowOut: true},
		Outputs:          []VFS{output},
		Requirements:     Requirements{CPU: float64(1), Network: nwFull, RAM: float64(32)},
		Sandboxing:       true,
		TargetProperties: TargetProperties{ModuleDir: "build/resources"},
	}

	// Stable, command-independent uid (hash of URI + output), set before emit so the
	// finalizer keeps it instead of hashing the binary-path-bearing command.
	node.UID = resourceFetchUID(decl.URI.string(), output.string())
	node.SelfUID = node.UID

	ref := ctx.emit.emit(node)
	ctx.fetchRefs.put(decl.Name, ref)

	return ref
}
