package main

import "strings"

// EmitPR emits a PR node for a RUN_PROGRAM invocation.
//
// toolBinPath: BUILD_ROOT-absolute path to the tool binary (from
// walking the tool PROGRAM's LDPath). toolLDRef: the tool's LD node.
//
// cmd_args: [toolBinPath, <args with ${ARCADIA_ROOT}→$(S)>]. Bare
// filenames matching IN/OUT/OUT_NOAUTO/STDOUT are expanded to
// $(S)/.../ or $(B)/.../ respectively. inputs: tool + IN abs paths +
// closure. outputs: STDOUT or OUT/OUT_NOAUTO abs paths.
// deps/foreign_deps.tool carry toolLDRef.
type prEmitResult struct {
	Ref    NodeRef
	Inputs []VFS
}

func EmitPR(
	instance ModuleInstance,
	srcDir *string,
	stmt *RunProgramStmt,
	toolBinPath VFS,
	toolLDRef NodeRef,
	inputClosure []VFS,
	extraDepRefs []NodeRef,
	emit Emitter,
) prEmitResult {
	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
	}
	for _, kv := range stmt.EnvPairs {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			env[parts[0]] = parts[1]
		} else {
			env[kv] = ""
		}
	}

	inSet := make(map[string]bool, len(stmt.INFiles))
	for _, f := range stmt.INFiles {
		inSet[f] = true
	}
	outSet := make(map[string]bool, len(stmt.OUTFiles)+len(stmt.OUTNoAutoFiles)+1)
	for _, f := range stmt.OUTFiles {
		outSet[f] = true
	}
	for _, f := range stmt.OUTNoAutoFiles {
		outSet[f] = true
	}
	if stmt.StdoutFile != nil {
		outSet[*stmt.StdoutFile] = true
	}

	cmdArgs := make([]string, 0, 1+len(stmt.Args))
	cmdArgs = append(cmdArgs, toolBinPath.String())
	for _, a := range stmt.Args {
		a = strings.ReplaceAll(a, "${ARCADIA_ROOT}", "$(S)")
		a = strings.ReplaceAll(a, "${MODDIR}", instance.Path)
		a = strings.ReplaceAll(a, "$CURDIR", Source(instance.Path).String())
		if inSet[a] && !strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
			a = Source(runProgramSourceRel(instance, srcDir, a)).String()
		} else if outSet[a] && !strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
			a = Build(instance.Path + "/" + a).String()
		}
		cmdArgs = append(cmdArgs, a)
	}

	inAbsPaths := make([]VFS, 0, len(stmt.INFiles))
	for _, f := range stmt.INFiles {
		inAbsPaths = append(inAbsPaths, Source(runProgramSourceRel(instance, srcDir, f)))
	}

	inputs := make([]VFS, 0, 1+len(inAbsPaths)+len(inputClosure))
	seen := make(map[VFS]struct{}, 1+len(inAbsPaths)+len(inputClosure))
	appendUnique := func(p VFS) {
		if _, dup := seen[p]; dup {
			return
		}
		seen[p] = struct{}{}
		inputs = append(inputs, p)
	}
	appendUnique(toolBinPath)
	for _, p := range inAbsPaths {
		appendUnique(p)
	}
	for _, p := range inputClosure {
		appendUnique(p)
	}

	var outputs []VFS
	var stdoutPath string
	if stmt.StdoutFile != nil {
		stdoutVFS := Build(instance.Path + "/" + *stmt.StdoutFile)
		stdoutPath = stdoutVFS.String()
		outputs = append(outputs, stdoutVFS)
	}
	for _, f := range stmt.OUTFiles {
		outputs = append(outputs, Build(instance.Path+"/"+f))
	}
	for _, f := range stmt.OUTNoAutoFiles {
		outputs = append(outputs, Build(instance.Path+"/"+f))
	}

	depRefs := make([]NodeRef, 0, 1+len(extraDepRefs))
	if toolLDRef != (NodeRef{}) {
		depRefs = append(depRefs, toolLDRef)
	}
	depRefs = append(depRefs, extraDepRefs...)

	foreignDepTool := make([]NodeRef, 0, 1)
	if toolLDRef != (NodeRef{}) {
		foreignDepTool = append(foreignDepTool, toolLDRef)
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
		Cmds:    []Cmd{cmd},
		Env:     env,
		Inputs:  inputs,
		Outputs: outputs,
		KV: map[string]string{
			"p":        "PR",
			"pc":       "yellow",
			"show_out": "yes",
		},
		Tags:         tags,
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		Platform: string(instance.Platform.Target),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		DepRefs:        depRefs,
		ForeignDepRefs: map[string][]NodeRef{"tool": foreignDepTool},
	}

	return prEmitResult{
		Ref:    emit.Emit(node),
		Inputs: append([]VFS(nil), inputs...),
	}
}
