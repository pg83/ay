package main

import (
	"strings"
)

func emitRunPythonForAR(ctx *genCtx, instance ModuleInstance, d *moduleData, in ModuleCCInputs) *runProgramsForARResult {
	if len(d.runPython) == 0 {
		return nil
	}

	reg := codegenRegForInstance(ctx, instance)
	res := &runProgramsForARResult{}

	for _, rp := range d.runPython {
		pyRef := emitRunPython(ctx, instance, rp, d, reg, in)
		if d.prOutputProducer == nil {
			d.prOutputProducer = map[string]NodeRef{}
		}
		for _, f := range rp.OUTFiles {
			d.prOutputProducer[f] = pyRef
		}
		for _, f := range rp.OUTNoAutoFiles {
			d.prOutputProducer[f] = pyRef
		}
		if rp.StdoutFile != nil {
			d.prOutputProducer[*rp.StdoutFile] = pyRef
		}

		outs := make([]string, 0, len(rp.OUTFiles)+1)
		outs = append(outs, rp.OUTFiles...)
		if rp.StdoutFile != nil {
			outs = append(outs, *rp.StdoutFile)
		}

		for _, out := range outs {
			if !isCCSourceExt(out) {
				continue
			}
			ccRef, ccOut := emitPRDownstreamCC(ctx, instance, out, pyRef, in)
			res.CCRefs = append(res.CCRefs, ccRef)
			res.CCOutputs = append(res.CCOutputs, ccOut)
		}
	}

	return res
}

func emitRunPython(ctx *genCtx, instance ModuleInstance, stmt *RunPythonStmt, d *moduleData, reg *CodegenRegistry, moduleInputs ModuleCCInputs) NodeRef {
	scriptVFS := copyFileInputVFS(ctx.fs, instance.Path, stmt.ScriptPath)
	inVFSByToken := make(map[string]VFS, len(stmt.INFiles))
	inVFSs := make([]VFS, 0, len(stmt.INFiles))
	for _, f := range stmt.INFiles {
		vfs := runProgramInputVFS(ctx, instance, d, f)
		inVFSByToken[f] = vfs
		inVFSs = append(inVFSs, vfs)
	}
	outVFSByToken := make(map[string]VFS, len(stmt.OUTFiles)+len(stmt.OUTNoAutoFiles)+1)
	for _, f := range stmt.OUTFiles {
		outVFSByToken[f] = copyFileOutputVFS(instance.Path, f)
	}
	for _, f := range stmt.OUTNoAutoFiles {
		outVFSByToken[f] = copyFileOutputVFS(instance.Path, f)
	}
	var stdoutVFS *VFS
	if stmt.StdoutFile != nil {
		vfs := copyFileOutputVFS(instance.Path, *stmt.StdoutFile)
		stdoutVFS = &vfs
		outVFSByToken[*stmt.StdoutFile] = vfs
	}

	if reg != nil {
		for _, f := range stmt.OUTFiles {
			registerGeneratedParsedOutput(ctx, instance, "PY", outVFSByToken[f], pyEmitsIncludes(ctx, instance, d, stmt, f, scriptVFS))
		}
		for _, f := range stmt.OUTNoAutoFiles {
			registerGeneratedParsedOutput(ctx, instance, "PY", outVFSByToken[f], pyEmitsIncludes(ctx, instance, d, stmt, f, scriptVFS))
		}
		if stmt.StdoutFile != nil {
			registerGeneratedParsedOutput(ctx, instance, "PY", *stdoutVFS, pyEmitsIncludes(ctx, instance, d, stmt, *stmt.StdoutFile, scriptVFS))
		}
	}

	inputClosure := pyInputClosure(ctx, instance, stmt, moduleInputs)
	codegenInputs := append([]VFS{scriptVFS}, inVFSs...)
	extraDepRefs := resolveCodegenDepRefsExt(ctx, instance, inputClosure, codegenInputs)
	result := EmitPYRun(instance, stmt, scriptVFS, inVFSByToken, outVFSByToken, stdoutVFS, inputClosure, extraDepRefs, ctx.emit)
	if d.prOutputInputs == nil {
		d.prOutputInputs = map[string][]VFS{}
	}
	for _, f := range stmt.OUTFiles {
		d.prOutputInputs[f] = append([]VFS(nil), result.Inputs...)
	}
	for _, f := range stmt.OUTNoAutoFiles {
		d.prOutputInputs[f] = append([]VFS(nil), result.Inputs...)
	}
	if stmt.StdoutFile != nil {
		d.prOutputInputs[*stmt.StdoutFile] = append([]VFS(nil), result.Inputs...)
	}

	if reg != nil {
		for _, f := range stmt.OUTFiles {
			bindGeneratedOutput(ctx, instance, outVFSByToken[f], result.Ref)
		}
		for _, f := range stmt.OUTNoAutoFiles {
			bindGeneratedOutput(ctx, instance, outVFSByToken[f], result.Ref)
		}
		if stmt.StdoutFile != nil {
			bindGeneratedOutput(ctx, instance, *stdoutVFS, result.Ref)
		}
	}

	return result.Ref
}

func pyInputClosure(ctx *genCtx, instance ModuleInstance, stmt *RunPythonStmt, moduleInputs ModuleCCInputs) []VFS {
	scanIn := ModuleCCInputs{
		InclArgs:          ctx.inclArgs,
		Flags:             moduleInputs.Flags,
		AddIncl:           moduleInputs.AddIncl,
		PeerAddInclGlobal: moduleInputs.PeerAddInclGlobal,
		SrcDir:            moduleInputs.SrcDir,
		SourceRoot:        ctx.sourceRoot,
		FS:                ctx.fs,
	}

	var out []VFS
	walkOne := func(rel string) {
		buildRootPath := copyFileOutputVFS(instance.Path, rel)
		out = append(out, walkClosure(ctx, instance, buildRootPath, scanIn)...)
	}

	for _, f := range stmt.OUTFiles {
		if isCCSourceExt(f) {
			walkOne(f)
		}
	}
	for _, f := range stmt.OUTNoAutoFiles {
		if isCCSourceExt(f) {
			walkOne(f)
		}
	}
	if stmt.StdoutFile != nil && isCCSourceExt(*stmt.StdoutFile) {
		walkOne(*stmt.StdoutFile)
	}

	out = dropTransitiveGeneratedProto(out)

	if len(out) == 0 {
		return nil
	}
	out = mergeDedupVFS(out, nil)
	return out
}

func pyEmitsIncludes(ctx *genCtx, instance ModuleInstance, d *moduleData, stmt *RunPythonStmt, outFile string, scriptVFS VFS) []includeDirective {
	if !generatedOutputCarriesIncludes(outFile) {
		return nil
	}

	includes := []includeDirective{{kind: includeQuoted, target: internString(scriptVFS.Rel())}}
	for _, f := range stmt.INFiles {
		includes = append(includes, includeDirective{kind: includeQuoted, target: internString(runProgramInputVFS(ctx, instance, d, f).Rel())})
	}
	for _, f := range stmt.OutputIncludes {
		if vfsHasPrefix(f) {
			f = Intern(f).Rel()
		}
		includes = append(includes, includeDirective{kind: includeQuoted, target: internString(f)})
	}
	return includes
}

func EmitPYRun(
	instance ModuleInstance,
	stmt *RunPythonStmt,
	scriptVFS VFS,
	inVFSByToken map[string]VFS,
	outVFSByToken map[string]VFS,
	stdoutVFS *VFS,
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

	cmdArgs := []string{instance.Platform.Tools.Python3, scriptVFS.String()}
	for _, a := range stmt.Args {
		a = strings.ReplaceAll(a, "${ARCADIA_ROOT}", "$(S)")
		a = strings.ReplaceAll(a, "${ARCADIA_BUILD_ROOT}", "$(B)")
		a = strings.ReplaceAll(a, "${CURDIR}", Source(instance.Path).String())
		a = strings.ReplaceAll(a, "${BINDIR}", Build(instance.Path).String())
		a = strings.ReplaceAll(a, "${MODDIR}", instance.Path)
		a = strings.ReplaceAll(a, "$CURDIR", Source(instance.Path).String())
		a = strings.ReplaceAll(a, "$BINDIR", Build(instance.Path).String())
		if vfs, ok := inVFSByToken[a]; ok && !strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
			a = vfs.String()
		} else if vfs, ok := outVFSByToken[a]; ok && !strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
			a = vfs.String()
		}
		cmdArgs = append(cmdArgs, a)
	}

	inputs := make([]VFS, 0, 1+len(stmt.INFiles)+len(inputClosure))
	seen := make(map[VFS]struct{}, 1+len(stmt.INFiles)+len(inputClosure))
	appendUnique := func(vfs VFS) {
		if _, ok := seen[vfs]; ok {
			return
		}
		seen[vfs] = struct{}{}
		inputs = append(inputs, vfs)
	}
	appendUnique(scriptVFS)
	for _, f := range stmt.INFiles {
		appendUnique(inVFSByToken[f])
	}
	for _, vfs := range inputClosure {
		appendUnique(vfs)
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

	cmd := Cmd{CmdArgs: cmdArgs, Env: env}
	if stdoutPath != "" {
		cmd.Stdout = stdoutPath
	}
	if stmt.CWD != nil {
		cmd.Cwd = expandRunProgramCWD(instance, *stmt.CWD)
	}

	node := &Node{
		Cmds:   []Cmd{cmd},
		Env:    env,
		Inputs: inputs,
		KV: map[string]interface{}{
			"p":        "PY",
			"pc":       "yellow",
			"show_out": "yes",
		},
		Outputs: outputs,
		Tags:    instance.Platform.Tags,
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		Platform: string(instance.Platform.Target),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		DepRefs: extraDepRefs,
	}

	return prEmitResult{
		Ref:    emit.Emit(bindNodePlatform(node, instance.Platform)),
		Inputs: append([]VFS(nil), inputs...),
	}
}
