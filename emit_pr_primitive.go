package main

import "strings"

type prEmitResult struct {
	Ref    NodeRef
	Inputs []VFS
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
	env := EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}}

	for _, kv := range stmt.EnvPairs {
		parts := strings.SplitN(kv, "=", 2)

		if len(parts) == 2 {
			env = append(env, EnvVar{Name: parts[0], Value: parts[1]})
		} else {
			env = append(env, EnvVar{Name: kv})
		}
	}

	cmdArgs := make([]ANY, 0, 1+len(stmt.Args))
	cmdArgs = append(cmdArgs, vfsAny(toolBinPath))

	for _, a := range stmt.Args {
		a = strings.ReplaceAll(a, "${ARCADIA_ROOT}", "$(S)")
		a = strings.ReplaceAll(a, "${ARCADIA_BUILD_ROOT}", "$(B)")
		a = strings.ReplaceAll(a, "${CURDIR}", Source(instance.Path).String())
		a = strings.ReplaceAll(a, "${BINDIR}", Build(instance.Path).String())
		a = strings.ReplaceAll(a, "${MODDIR}", instance.Path)
		a = strings.ReplaceAll(a, "$CURDIR", Source(instance.Path).String())
		a = strings.ReplaceAll(a, "$BINDIR", Build(instance.Path).String())

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

		cmdArgs = append(cmdArgs, stringAny(a))
	}

	inputs := make([]VFS, 0, 1+len(auxTools)+len(stmt.INFiles)+len(inputClosure))
	seen := make(map[VFS]struct{}, 1+len(auxTools)+len(stmt.INFiles)+len(inputClosure))
	appendUnique := func(p VFS) {
		if _, dup := seen[p]; dup {
			return
		}

		seen[p] = struct{}{}
		inputs = append(inputs, p)
	}
	appendUnique(toolBinPath)

	for _, tool := range auxTools {
		appendUnique(tool.bin)
	}

	for _, f := range stmt.INFiles {
		appendUnique(inVFSByToken[f])
	}

	for _, p := range inputClosure {
		appendUnique(p)
	}

	var outputs []VFS
	var stdoutPath string

	if stdoutVFS != nil {
		stdoutPath = stdoutVFS.String()
		outputs = append(outputs, *stdoutVFS)
	}

	for _, f := range stmt.OUTFiles {
		outputs = append(outputs, outVFSByToken[f])
	}

	for _, f := range stmt.OUTNoAutoFiles {
		outputs = append(outputs, outVFSByToken[f])
	}

	toolRefs := make([]NodeRef, 0, len(auxTools)+1)
	seenToolRefs := make(map[NodeRef]struct{}, len(auxTools)+1)
	appendToolRef := func(ref NodeRef) {
		if ref == (NodeRef(0)) {
			return
		}

		if _, dup := seenToolRefs[ref]; dup {
			return
		}

		seenToolRefs[ref] = struct{}{}
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

	if stdoutPath != "" {
		cmd.Stdout = stdoutPath
	}

	if stmt.CWD != nil {
		cmd.Cwd = expandRunProgramCWD(instance, *stmt.CWD)
	}

	tags := instance.Platform.Tags

	node := &Node{
		Cmds:             []Cmd{cmd},
		Env:              env,
		Inputs:           inputs,
		Outputs:          outputs,
		KV:               KV{P: pkPR, PC: pcYellow, ShowOut: "yes"},
		Tags:             tags,
		TargetProperties: TargetProperties{ModuleDir: instance.Path},
		Platform:         string(instance.Platform.Target),
		Requirements:     Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
		DepRefs:          depRefs,
		ForeignDepRefs:   foreignDepRefs,
	}

	return prEmitResult{
		Ref:    emit.Emit(bindNodePlatform(node, instance.Platform)),
		Inputs: append([]VFS(nil), inputs...),
	}
}
