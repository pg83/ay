package main

// emitResourceFetch emits the FETCH node that downloads one external resource
// declared by a RESOURCES_LIBRARY (DECLARE_EXTERNAL_RESOURCE / _HOST_RESOURCES_*)
// into its $(B)/resources/<Name> directory, returning the node's NodeRef. The
// node is emitted at most once per resource name across the whole run (deduped
// through ctx.fetchRefs); every consumer that splices $(<Name>) into a command
// takes the fetch as a dependency from there.
func emitResourceFetch(ctx *genCtx, decl resourceDecl) NodeRef {
	name := decl.Name.String()

	if ref, ok := ctx.fetchRefs[name]; ok {
		return ref
	}

	output := Build("resources/" + name)
	node := &Node{
		Cmds: []Cmd{{
			CmdArgs: []STR{
				internStr(currentYatoolPath()),
				argFetch.str(),
				argB.str(),
				argS.str(),
				decl.URI,
				output.str(),
			},
		}},
		Inputs:           fetchScriptInputs(ctx.scripts),
		KV:               KV{P: pkFETCH, PC: pcYellow, ShowOut: "yes"},
		Outputs:          []VFS{output},
		Requirements:     Requirements{CPU: float64(1), Network: "full", RAM: float64(32)},
		Sandboxing:       true,
		TargetProperties: TargetProperties{ModuleDir: "build/resources"},
	}

	// Stable, command-independent uid (hash of URI + output), set before emit so the
	// finalizer keeps it instead of hashing the binary-path-bearing command.
	node.UID = resourceFetchUID(decl.URI.String(), output.String())
	node.SelfUID = node.UID

	ref := ctx.emit.Emit(bindNodePlatform(node, ctx.host))
	ctx.fetchRefs[name] = ref

	return ref
}
