package main

import (
	"strings"
)

var pyRunKV = KV{P: pkPY, PC: pcYellow, ShowOut: true}

func (e *EmitContext) emitRunPythonForAR() {
	_, instance, d := e.ctx, e.instance, e.d
	if len(d.runPython) == 0 {
		return
	}

	for _, rp := range d.runPython {
		e.emitRunPython(rp)
		outs := make([]string, 0, len(rp.OUTFiles)+1)

		outs = append(outs, strStrings(rp.OUTFiles)...)

		if rp.StdoutFile != nil && !rp.StdoutNoAuto {
			outs = append(outs, rp.StdoutFile.string())
		}

		for _, out := range outs {
			if isCCSourceExt(out) || isAsmSourceExt(out) {
				e.enqueueSrc(copyFileOutputVFS(instance.Path.rel(), out).str(), SrcMeta{Prio: stmtPrioDefault, Generated: true, Bucket: bkRunPython})
			}
		}
	}
}

func (e *EmitContext) emitRunPython(stmt *RunPythonStmt) NodeRef {
	ctx, instance, d := e.ctx, e.instance, e.d
	scriptVFS := copyFileInputVFS(ctx.fs, instance.Path, stmt.ScriptPath.string())
	inVFSByToken := make(map[string]VFS, len(stmt.INFiles))
	inVFSs := make([]VFS, 0, len(stmt.INFiles))

	for _, f := range stmt.INFiles {
		vfs := e.runProgramInputVFS(f.string())

		inVFSByToken[f.string()] = vfs
		inVFSs = append(inVFSs, vfs)
	}

	outVFSByToken := make(map[string]VFS, len(stmt.OUTFiles)+len(stmt.OUTNoAutoFiles)+1)

	for _, f := range stmt.OUTFiles {
		outVFSByToken[f.string()] = copyFileOutputVFS(instance.Path.rel(), f.string())
	}

	for _, f := range stmt.OUTNoAutoFiles {
		outVFSByToken[f.string()] = copyFileOutputVFS(instance.Path.rel(), f.string())
	}

	var stdoutVFS *VFS

	if stmt.StdoutFile != nil {
		vfs := copyFileOutputVFS(instance.Path.rel(), stmt.StdoutFile.string())

		stdoutVFS = &vfs
		outVFSByToken[stmt.StdoutFile.string()] = vfs
	}

	hasCCShard, _ := splitCodegenDetect(stmt)

	var splitSrcs []VFS

	if hasCCShard {
		splitSrcs = e.splitCodegenSrcs(stmt, scriptVFS)
	}

	var pySourceInputs []VFS
	var pyGeneratedFromSources []VFS

	if scriptVFS.isSource() {
		pySourceInputs = append(pySourceInputs, scriptVFS)
	}

	reg := e.codegen

	for _, v := range inVFSs {
		if v.isSource() {
			pySourceInputs = append(pySourceInputs, v)

			continue
		}

		if info := reg.lookup(v); info != nil {
			pyGeneratedFromSources = append(pyGeneratedFromSources, info.SourceInputs...)
		}
	}

	pySourceInputs = append(pySourceInputs, pyGeneratedFromSources...)

	pyRef := ctx.emit.reserve()

	registerPYOutput := func(out VFS, parsed []IncludeDirective) {
		reg.register(&GeneratedFileInfo{
			OutputPath:     out,
			ProducerRef:    pyRef,
			ParsedIncludes: parsed,
			SourceInputs:   pySourceInputs,
			ClosureLeaves:  pyGeneratedFromSources,
		})
	}

	for _, f := range stmt.OUTFiles {
		registerPYOutput(outVFSByToken[f.string()], e.pyEmitsIncludes(stmt, f.string(), scriptVFS, splitSrcs, hasCCShard))
	}

	for _, f := range stmt.OUTNoAutoFiles {
		registerPYOutput(outVFSByToken[f.string()], e.pyEmitsIncludes(stmt, f.string(), scriptVFS, splitSrcs, hasCCShard))
	}

	if stmt.StdoutFile != nil {
		registerPYOutput(*stdoutVFS, e.pyEmitsIncludes(stmt, stmt.StdoutFile.string(), scriptVFS, splitSrcs, hasCCShard))
	}

	inputClosure := e.pyInputClosure(stmt)
	extraDepRefs := resolveCodegenDepRefsIncl(ctx, instance, ctx.na, inputClosure)

	return emitPYRun(instance, stmt, scriptVFS, inVFSByToken, outVFSByToken, stdoutVFS, inputClosure, extraDepRefs, pyRef, d.cc.TC, ctx.emit)
}

func (e *EmitContext) pyInputClosure(stmt *RunPythonStmt) []VFS {
	ctx, instance, d := e.ctx, e.instance, e.d
	scanCfg := newScanContext(ctx.parsers, d.cc.AddIncl, d.cc.PeerAddInclGlobal, includeScannerBasePaths(), instance.Path.rel())

	var out []VFS

	walkOne := func(rel string) {
		buildRootPath := copyFileOutputVFS(instance.Path.rel(), rel)

		out = append(out, walkClosureTail(e.scanner, buildRootPath, scanCfg)...)
	}

	hasCCShard, _ := splitCodegenDetect(stmt)

	if hasCCShard {
		for _, f := range stmt.INFiles {
			vfs := e.runProgramInputVFS(f.string())

			out = append(out, walkClosure(e.scanner, vfs, scanCfg)...)
		}
	} else {
		for _, f := range stmt.OUTFiles {
			if generatedOutputCarriesIncludes(f.string()) {
				walkOne(f.string())
			}
		}

		for _, f := range stmt.OUTNoAutoFiles {
			if generatedOutputCarriesIncludes(f.string()) {
				walkOne(f.string())
			}
		}

		if stmt.StdoutFile != nil && generatedOutputCarriesIncludes(stmt.StdoutFile.string()) {
			walkOne(stmt.StdoutFile.string())
		}
	}

	if len(out) == 0 {
		return nil
	}

	out = dedup(out, nil)

	return out
}

func splitCodegenDetect(stmt *RunPythonStmt) (hasCCShard bool, hasHeader bool) {
	for _, f := range stmt.OUTNoAutoFiles {
		if isCCSourceExt(f.string()) {
			hasCCShard = true
		}

		if isHeaderSource(f.string()) {
			hasHeader = true
		}
	}

	return
}

func (e *EmitContext) splitCodegenSrcs(stmt *RunPythonStmt, scriptVFS VFS) []VFS {
	ctx, _, _ := e.ctx, e.instance, e.d
	reg := e.codegen
	seen := make(map[VFS]struct{}, 32)

	var sources []VFS

	addSource := func(v VFS) {
		if _, dup := seen[v]; dup {
			return
		}

		seen[v] = struct{}{}
		sources = append(sources, v)
	}

	addInducedSources := func(deps []IncludeDirective) {
		for _, d := range deps {
			if v := d.target.vfs(); v != 0 {
				if v.isSource() && ctx.fs.isFile(srcRootVFS, v.rel()) {
					addSource(v)
				}

				continue
			}
		}
	}

	addSource(scriptVFS)

	for _, f := range stmt.INFiles {
		vfs := e.runProgramInputVFS(f.string())

		if info := reg.lookup(vfs); info != nil {
			for _, si := range info.SourceInputs {
				addSource(si)
			}

			for _, gref := range info.GeneratorRefs {
				if tool, ok := ctx.moduleByRef.get(gref); ok {
					addInducedSources(tool.InducedDeps.bucket(parsedIncludesCpp))
				}
			}
		}
	}

	return sources
}

func (e *EmitContext) pyEmitsIncludes(stmt *RunPythonStmt, outFile string, scriptVFS VFS, splitSrcs []VFS, splitHasCCShard bool) []IncludeDirective {
	_, instance, _ := e.ctx, e.instance, e.d
	if !generatedOutputCarriesIncludes(outFile) {
		return nil
	}

	if splitHasCCShard && len(splitSrcs) > 0 {
		var firstShardFile string
		var firstShardVFS VFS

		for _, f := range stmt.OUTNoAutoFiles {
			if isCCSourceExt(f.string()) {
				firstShardFile = f.string()
				firstShardVFS = copyFileOutputVFS(instance.Path.rel(), f.string())

				break
			}
		}

		if isCCSourceExt(outFile) {
			isNonFirst := outFile != firstShardFile
			capacity := len(splitSrcs)

			if isNonFirst {
				capacity++
			}

			includes := make([]IncludeDirective, 0, capacity)

			if isNonFirst && firstShardVFS != 0 {
				includes = append(includes, IncludeDirective{kind: includeQuoted, target: internStr(firstShardVFS.rel())})
			}

			for _, src := range splitSrcs {
				includes = append(includes, IncludeDirective{kind: includeQuoted, target: internStr(src.rel())})
			}

			return includes
		}

		if isHeaderSource(outFile) {
			includes := make([]IncludeDirective, 0, 1+len(splitSrcs))

			if firstShardVFS != 0 {
				includes = append(includes, IncludeDirective{kind: includeQuoted, target: internStr(firstShardVFS.rel())})
			}

			for _, src := range splitSrcs {
				includes = append(includes, IncludeDirective{kind: includeQuoted, target: internStr(src.rel())})
			}

			return includes
		}
	}

	includes := []IncludeDirective{{kind: includeQuoted, target: internStr(scriptVFS.rel())}}

	for _, f := range stmt.INFiles {
		includes = append(includes, IncludeDirective{kind: includeQuoted, target: internStr(e.runProgramInputVFS(f.string()).rel())})
	}

	for _, f := range stmt.OutputIncludes {
		if vfsHasPrefix(f.string()) {
			f = internStr(intern(f.string()).rel())
		}

		includes = append(includes, IncludeDirective{kind: includeQuoted, target: internStr(f.string())})
	}

	return includes
}

func emitPYRun(
	instance ModuleInstance,
	stmt *RunPythonStmt,
	scriptVFS VFS,
	inVFSByToken map[string]VFS,
	outVFSByToken map[string]VFS,
	stdoutVFS *VFS,
	inputClosure []VFS,
	extraDepRefs []NodeRef,
	id NodeRef,
	tc ModuleToolchain,
	emit *StreamingEmitter,
) NodeRef {
	na := emit.nodeArenas()
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	for _, kv := range stmt.EnvPairs {
		parts := strings.SplitN(kv.string(), "=", 2)

		if len(parts) == 2 {
			env = append(env, EnvVar{Name: internEnv(parts[0]), Value: internStr(parts[1])})
		} else {
			env = append(env, EnvVar{Name: internEnv(kv.string()), Value: strEmpty})
		}
	}

	cmdArgs := []STR{tc.Python3, (scriptVFS).str()}

	for _, aTok := range stmt.Args {
		a := aTok.string()

		if vfs, ok := inVFSByToken[a]; ok && !strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
			a = vfs.string()
		} else if vfs, ok := outVFSByToken[a]; ok && !strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
			a = vfs.string()
		}

		cmdArgs = append(cmdArgs, internStr(a))
	}

	head := make([]VFS, 0, 1+len(stmt.INFiles))

	head = append(head, scriptVFS)

	for _, f := range stmt.INFiles {
		head = append(head, inVFSByToken[f.string()])
	}

	inputs := na.inputList(head, inputClosure)

	var outputs []VFS
	var stdoutPath STR

	if stdoutVFS != nil {
		stdoutPath = stdoutVFS.str()
		outputs = append(outputs, *stdoutVFS)
	}

	for _, f := range stmt.OUTFiles {
		outputs = append(outputs, outVFSByToken[f.string()])
	}

	for _, f := range stmt.OUTNoAutoFiles {
		outputs = append(outputs, outVFSByToken[f.string()])
	}

	cmd := Cmd{CmdArgs: na.chunkList(cmdArgs), Env: env}

	if stdoutPath != 0 {
		cmd.Stdout = stdoutPath
	}

	if stmt.CWD != nil {
		cmd.Cwd = *stmt.CWD
	}

	node := &Node{
		Platform:     instance.Platform,
		Cmds:         na.cmdList(cmd),
		Env:          env,
		Inputs:       inputs,
		KV:           &pyRunKV,
		Outputs:      outputs,
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:      extraDepRefs,
		Resources:    usesPython3,
	}

	emit.emitReserved(node, id)

	return id
}
