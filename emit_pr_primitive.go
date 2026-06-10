package main

import "strings"

type prEmitResult struct {
	Ref    NodeRef
	Inputs inputChunks
}

func EmitPR(
	instance ModuleInstance,
	stmt *RunProgramStmt,
	toolBinPath VFS,
	toolLDRef NodeRef,
	auxTools []runProgramAuxTool,
	inVFSByToken map[string]VFS,
	outVFSByToken map[string]VFS,
	stdoutVFS *VFS,
	inputClosure []VFS,
	extraDepRefs []NodeRef,
	emit Emitter,
) prEmitResult {
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	for _, kv := range stmt.EnvPairs {
		parts := strings.SplitN(kv, "=", 2)

		if len(parts) == 2 {
			env = append(env, EnvVar{Name: internEnv(parts[0]), Value: internStr(parts[1])})
		} else {
			env = append(env, EnvVar{Name: internEnv(kv), Value: strEmpty})
		}
	}

	cmdArgs := make([]STR, 0, 1+len(stmt.Args))
	cmdArgs = append(cmdArgs, (toolBinPath).str())

	for _, a := range stmt.Args {
		a = strings.ReplaceAll(a, "${ARCADIA_ROOT}", "$(S)")
		a = strings.ReplaceAll(a, "${ARCADIA_BUILD_ROOT}", "$(B)")
		a = strings.ReplaceAll(a, "${CURDIR}", instance.Path.String())
		a = strings.ReplaceAll(a, "${BINDIR}", Build(instance.Path.Rel()).String())
		a = strings.ReplaceAll(a, "${MODDIR}", instance.Path.Rel())
		a = strings.ReplaceAll(a, "$CURDIR", instance.Path.String())
		a = strings.ReplaceAll(a, "$BINDIR", Build(instance.Path.Rel()).String())

		for _, tool := range auxTools {
			if strings.Contains(a, tool.token) {
				a = strings.ReplaceAll(a, tool.token, tool.bin.String())
			}
		}

		if vfs, ok := inVFSByToken[a]; ok && !strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
			a = vfs.String()
		} else if vfs, ok := outVFSByToken[a]; ok && !strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
			a = vfs.String()
		}

		cmdArgs = append(cmdArgs, internStr(a))
	}

	head := make([]VFS, 0, 1+len(auxTools)+len(stmt.INFiles))
	deduper.reset()
	appendUnique := func(p VFS) {
		if !deduper.add(p) {
			return
		}

		head = append(head, p)
	}
	appendUnique(toolBinPath)

	for _, tool := range auxTools {
		appendUnique(tool.bin)
	}

	for _, f := range stmt.INFiles {
		appendUnique(inVFSByToken[f])
	}

	// The closure tail is filtered against the head set; filterSeen returns
	// inputClosure itself when nothing collides, so the closure is referenced,
	// not copied, into the chunk list.
	inputs := inputChunks{head, deduper.filterSeen(inputClosure)}

	var outputs []VFS
	var stdoutPath STR

	if stdoutVFS != nil {
		stdoutPath = stdoutVFS.str()
		outputs = append(outputs, *stdoutVFS)
	}

	for _, f := range stmt.OUTFiles {
		outputs = append(outputs, outVFSByToken[f])
	}

	for _, f := range stmt.OUTNoAutoFiles {
		outputs = append(outputs, outVFSByToken[f])
	}

	toolRefs := make([]NodeRef, 0, len(auxTools)+1)
	// The inputs set above is complete by now, so the deduper is free for the
	// tool-ref set.
	deduper.reset()
	appendToolRef := func(ref NodeRef) {
		if ref == (NodeRef(0)) {
			return
		}

		if !deduper.add(VFS(ref)) {
			return
		}

		toolRefs = append(toolRefs, ref)
	}

	for _, tool := range auxTools {
		appendToolRef(tool.ref)
	}

	appendToolRef(toolLDRef)

	depRefs := make([]NodeRef, 0, len(toolRefs)+len(extraDepRefs))
	depRefs = append(depRefs, toolRefs...)
	depRefs = append(depRefs, extraDepRefs...)

	var foreignDepRefs []NodeRef

	if len(toolRefs) > 0 {
		// toolRefs is a fresh local, not mutated after this; depRefs above already
		// copied out of it, so the node may share it read-only.
		foreignDepRefs = toolRefs
	}

	cmd := Cmd{
		CmdArgs: cmdArgs,
		Env:     env,
	}

	if stdoutPath != 0 {
		cmd.Stdout = stdoutPath
	}

	if stmt.CWD != nil {
		cmd.Cwd = internStr(expandRunProgramCWD(instance, *stmt.CWD))
	}

	node := &Node{
		Platform:         instance.Platform,
		Cmds:             []Cmd{cmd},
		Env:              env,
		Inputs:           inputs,
		Outputs:          outputs,
		KV:               KV{P: pkPR, PC: pcYellow, ShowOut: true},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.Rel()},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:          depRefs,
		ForeignDepRefs:   foreignDepRefs,
	}

	// The node and the result share the same chunk list: nothing mutates a
	// node's Inputs after Emit, and prOutputInputs readers copy out.
	return prEmitResult{
		Ref:    emit.Emit(node),
		Inputs: inputs,
	}
}
