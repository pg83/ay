package main

import (
	"strings"
)

var pyRunKV = KV{P: pkPY, PC: pcYellow, ShowOut: true}

var luaRunKV = KV{P: pkLU, PC: pcYellow, ShowOut: true}

var argToolsLua = internArg("tools/lua")

func (e *EmitContext) emitRunPythonStmt(rp *RunPythonStmt) {
	instance := e.instance

	e.emitRunPython(rp)

	outs := make([]string, 0, len(rp.OUTFiles)+1)

	outs = append(outs, strStrings(rp.OUTFiles)...)

	if rp.StdoutFile != nil && !rp.StdoutNoAuto {
		outs = append(outs, rp.StdoutFile.string())
	}

	for _, out := range outs {
		switch {
		case isCCSourceExt(out) || isAsmSourceExt(out):
			e.enqueueSrc(SrcMeta{Source: copyFileOutputVFS(instance.Path.relString(), out).fullSTR(), Prio: stmtPrioDefault, Generated: true, Bucket: bkRunPython})
		case isCodegenProducingSrc(out):
			e.enqueueSrc(SrcMeta{Source: internStr(out), Prio: stmtPrioDefault, Generated: true, Bucket: bkRunPython})
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
		outVFSByToken[f.string()] = copyFileOutputVFS(instance.Path.relString(), f.string())
	}

	for _, f := range stmt.OUTNoAutoFiles {
		outVFSByToken[f.string()] = copyFileOutputVFS(instance.Path.relString(), f.string())
	}

	var stdoutVFS *VFS

	if stmt.StdoutFile != nil {
		vfs := copyFileOutputVFS(instance.Path.relString(), stmt.StdoutFile.string())

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
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed},
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

	interp := d.cc.TC.Python3
	kv := &pyRunKV
	resources := usesPython3

	var interpInput *VFS
	var toolRefs []NodeRef

	if stmt.Lua {
		luaLDRef, luaBinVFS := ctx.tool(argToolsLua)

		interp = luaBinVFS.fullSTR()
		interpInput = &luaBinVFS
		toolRefs = depRefs(luaLDRef)
		kv = &luaRunKV
		resources = nil
	}

	return emitPYRun(instance, stmt, scriptVFS, inVFSByToken, outVFSByToken, stdoutVFS, inputClosure, extraDepRefs, pyRef, interp, interpInput, toolRefs, kv, resources, ctx.emit)
}

func (e *EmitContext) pyInputClosure(stmt *RunPythonStmt) []VFS {
	ctx, instance, d := e.ctx, e.instance, e.d
	scanCfg := newScanContext(ctx.parsers, d.cc.AddIncl, d.cc.PeerAddInclGlobal, includeScannerBasePaths(), instance.Path.relString())

	var groups [][][]VFS
	var selves []VFS

	walkOne := func(rel string) {
		buildRootPath := copyFileOutputVFS(instance.Path.relString(), rel)
		cv := walkClosure(e.scanner, buildRootPath, scanCfg)

		groups = append(groups, cv.buckets)
	}

	hasCCShard, _ := splitCodegenDetect(stmt)

	if hasCCShard {
		for _, f := range stmt.INFiles {
			vfs := e.runProgramInputVFS(f.string())
			cv := walkClosure(e.scanner, vfs, scanCfg)

			selves = append(selves, cv.self)
			groups = append(groups, cv.buckets)
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

		for _, f := range stmt.OutputIncludes {
			cv := walkClosure(e.scanner, e.runProgramInputVFS(f.string()), scanCfg)

			selves = append(selves, cv.self)
			groups = append(groups, cv.buckets)
		}
	}

	return dedupClosure(selves, groups...)
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
				if v.isSource() && ctx.fs.isFile(srcRootVFS, v.relString()) {
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
				firstShardVFS = copyFileOutputVFS(instance.Path.relString(), f.string())

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
				includes = append(includes, IncludeDirective{kind: includeQuoted, target: internStr(firstShardVFS.relString())})
			}

			for _, src := range splitSrcs {
				includes = append(includes, IncludeDirective{kind: includeQuoted, target: internStr(src.relString())})
			}

			return includes
		}

		if isHeaderSource(outFile) {
			includes := make([]IncludeDirective, 0, 1+len(splitSrcs))

			if firstShardVFS != 0 {
				includes = append(includes, IncludeDirective{kind: includeQuoted, target: internStr(firstShardVFS.relString())})
			}

			for _, src := range splitSrcs {
				includes = append(includes, IncludeDirective{kind: includeQuoted, target: internStr(src.relString())})
			}

			return includes
		}
	}

	includes := []IncludeDirective{{kind: includeQuoted, target: internStr(scriptVFS.relString())}}

	for _, f := range stmt.INFiles {
		includes = append(includes, IncludeDirective{kind: includeQuoted, target: internStr(e.runProgramInputVFS(f.string()).relString())})
	}

	for _, f := range stmt.OutputIncludes {
		if vfsHasPrefix(f.string()) {
			f = internStr(intern(f.string()).relString())
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
	interp STR,
	interpInput *VFS,
	toolRefs []NodeRef,
	kv *KV,
	resources []STR,
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

	cmdArgs := []STR{interp, (scriptVFS).fullSTR()}

	for _, aTok := range stmt.Args {
		a := aTok.string()

		if a == "${ARCADIA_ROOT}" {
			a = "$(S)"
		}

		if vfs, ok := inVFSByToken[a]; ok && !strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
			a = vfs.string()
		} else if vfs, ok := outVFSByToken[a]; ok && !strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
			a = vfs.string()
		} else if k, val, ok := strings.Cut(a, "="); ok && !strings.HasPrefix(a, "-") {
			if vfs, ok := inVFSByToken[val]; ok {
				a = k + "=" + vfs.string()
			}
		}

		cmdArgs = append(cmdArgs, internStr(a))
	}

	head := make([]VFS, 0, 2+len(stmt.INFiles))

	if interpInput != nil {
		head = append(head, *interpInput)
	}

	head = append(head, scriptVFS)

	for _, f := range stmt.INFiles {
		head = append(head, inVFSByToken[f.string()])
	}

	inputs := na.inputList(head, inputClosure)

	var outputs []VFS
	var stdoutPath STR

	if stdoutVFS != nil {
		stdoutPath = stdoutVFS.fullSTR()
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

	node := Node{
		Platform:       instance.Platform,
		Cmds:           na.cmdList(cmd),
		Env:            env,
		Inputs:         inputs,
		KV:             kv,
		Outputs:        outputs,
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:        extraDepRefs,
		ForeignDepRefs: toolRefs,
		Resources:      resources,
	}

	emit.emitReservedNode(node, id)

	return id
}
