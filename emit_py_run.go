package main

import (
	"strings"
)

var (
	pyRunKV     = KV{P: pkPY, PC: pcYellow, ShowOut: true}
	luaRunKV    = KV{P: pkLU, PC: pcYellow, ShowOut: true}
	argToolsLua = internArg("tools/lua")
)

func (e *EmitContext) emitRunPythonStmt(rp *RunPythonStmt) {
	instance := e.instance

	e.emitRunPython(rp)

	outs := make([]string, 0, len(rp.OUTFiles)+1)

	outs = append(outs, anyStrs(rp.OUTFiles)...)

	if rp.StdoutFile != nil && !rp.StdoutNoAuto {
		outs = append(outs, rp.StdoutFile.string())
	}

	for _, out := range outs {
		switch {
		case isCCSourceExt(out) || isAsmSourceExt(out):
			e.enqueueSrc(SrcMeta{Source: copyFileOutputVFS(instance.Path.relString(), out).any(), Prio: stmtPrioDefault, Generated: true, Bucket: bkRunPython})
		case isCodegenProducingSrc(out):
			e.enqueueSrc(SrcMeta{Source: pathAny(internStr(out)), Prio: stmtPrioDefault, Generated: true, Bucket: bkRunPython})
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

	var pyGeneratedFromSources []VFS

	reg := e.codegen

	for _, v := range inVFSs {
		if v.isSource() {
			continue
		}

		if info := reg.lookup(v); info != nil {
			pyGeneratedFromSources = append(pyGeneratedFromSources, info.SourceInputs...)
		}
	}

	pySourceInputs := ctx.na.vfs.alloc(1 + len(inVFSs) + len(pyGeneratedFromSources))[:0]

	if scriptVFS.isSource() {
		pySourceInputs = append(pySourceInputs, scriptVFS)
	}

	for _, v := range inVFSs {
		if v.isSource() {
			pySourceInputs = append(pySourceInputs, v)
		}
	}

	pySourceInputs = append(pySourceInputs, pyGeneratedFromSources...)
	ctx.na.vfs.commit(len(pySourceInputs))

	pySourceInputs = pySourceInputs[:len(pySourceInputs):len(pySourceInputs)]

	pyRef := ctx.emit.reserve()

	var pyInfos []*GeneratedFileInfo

	registerPYOutput := func(out VFS, parsed []IncludeDirective) {
		pyInfos = append(pyInfos, reg.register(GeneratedFileInfo{
			OutputPath:     out,
			ProducerRef:    pyRef,
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed},
			SourceInputs:   pySourceInputs,
			ClosureLeaves:  pyGeneratedFromSources,
		}))
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

	interp := d.cc.TC.Python3.any()
	kv := &pyRunKV
	resources := usesPython3

	var interpInput *VFS
	var toolRefs []NodeRef

	if stmt.Lua {
		luaLDRef, luaBinVFS := ctx.tool(argToolsLua)

		interp = luaBinVFS.any()
		interpInput = &luaBinVFS
		toolRefs = depRefs(luaLDRef)
		kv = &luaRunKV
		resources = nil
	}

	outIncludeVFSs := make([]VFS, 0, len(stmt.OutputIncludes))

	for _, f := range stmt.OutputIncludes {
		outIncludeVFSs = append(outIncludeVFSs, e.runProgramInputVFS(f.string()))
	}

	inSnapVFSs := make([]VFS, 0, len(stmt.INFiles))

	for _, f := range stmt.INFiles {
		inSnapVFSs = append(inSnapVFSs, inVFSByToken[f.string()])
	}

	snap := &pyRunSnap{
		ctx:            ctx,
		instance:       instance,
		scanner:        e.scanner,
		scanCfg:        newScanContext(ctx.parsers, ctx.na.vfsList(d.cc.AddIncl...), ctx.na.vfsList(d.cc.PeerAddInclGlobal...), includeScannerBasePaths(), instance.Path.relString()),
		inVFSs:         ctx.na.vfsList(inSnapVFSs...),
		outIncludeVFSs: ctx.na.vfsList(outIncludeVFSs...),
	}

	pe := &PendingEmit{owner: ctx.instanceKey(instance), fn: func() {
		inputClosure := pyInputClosure(snap, stmt)
		extraDepRefs := resolveCodegenDepRefsIncl(ctx, instance, ctx.na, inputClosure)

		emitPYRun(instance, stmt, scriptVFS, inVFSByToken, outVFSByToken, stdoutVFS, inputClosure, extraDepRefs, pyRef, interp, interpInput, toolRefs, kv, resources, ctx.emit)
	}}

	for _, info := range pyInfos {
		info.pending = pe
	}

	e.noteOwn(pe)

	return pyRef
}

type pyRunSnap struct {
	ctx            *GenCtx
	instance       ModuleInstance
	scanner        *IncludeScanner
	scanCfg        ScanContext
	inVFSs         []VFS
	outIncludeVFSs []VFS
}

func pyInputClosure(s *pyRunSnap, stmt *RunPythonStmt) []VFS {
	ctx, instance := s.ctx, s.instance
	na := ctx.na
	scanCfg := s.scanCfg

	var groups [][][]VFS
	var selves []VFS

	walkOne := func(rel string) {
		buildRootPath := copyFileOutputVFS(instance.Path.relString(), rel)
		cv := walkClosure(s.scanner, buildRootPath, scanCfg)

		groups = append(groups, cv.buckets)
	}

	hasCCShard, _ := splitCodegenDetect(stmt)

	if hasCCShard {
		for i := range stmt.INFiles {
			cv := walkClosure(s.scanner, s.inVFSs[i], scanCfg)

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

		for i := range stmt.OutputIncludes {
			cv := walkClosure(s.scanner, s.outIncludeVFSs[i], scanCfg)

			selves = append(selves, cv.self)
			groups = append(groups, cv.buckets)
		}
	}

	return dedupClosure(na, selves, groups...)
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
				if v.isSource() && ctx.fs.isFile(srcRootRel, v.relString()) {
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
			runPending(info)

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

			includes := e.ctx.na.dirs.alloc(capacity)[:0]

			if isNonFirst && firstShardVFS != 0 {
				includes = append(includes, IncludeDirective{kind: includeQuoted, target: includeTarget(firstShardVFS.rel().any())})
			}

			for _, src := range splitSrcs {
				includes = append(includes, IncludeDirective{kind: includeQuoted, target: includeTarget(src.rel().any())})
			}

			e.ctx.na.dirs.commit(len(includes))

			return includes[:len(includes):len(includes)]
		}

		if isHeaderSource(outFile) {
			includes := e.ctx.na.dirs.alloc(1 + len(splitSrcs))[:0]

			if firstShardVFS != 0 {
				includes = append(includes, IncludeDirective{kind: includeQuoted, target: includeTarget(firstShardVFS.rel().any())})
			}

			for _, src := range splitSrcs {
				includes = append(includes, IncludeDirective{kind: includeQuoted, target: includeTarget(src.rel().any())})
			}

			e.ctx.na.dirs.commit(len(includes))

			return includes[:len(includes):len(includes)]
		}
	}

	includes := e.ctx.na.dirs.alloc(1 + len(stmt.INFiles) + len(stmt.OutputIncludes))[:0]

	includes = append(includes, IncludeDirective{kind: includeQuoted, target: includeTarget(scriptVFS.rel().any())})

	for _, f := range stmt.INFiles {
		includes = append(includes, IncludeDirective{kind: includeQuoted, target: includeTarget(e.runProgramInputVFS(f.string()).rel().any())})
	}

	for _, f := range stmt.OutputIncludes {
		if vfsHasPrefix(f.string()) {
			f = f.vfs().rel().any()
		}

		includes = append(includes, IncludeDirective{kind: includeQuoted, target: includeTarget(f)})
	}

	e.ctx.na.dirs.commit(len(includes))

	return includes[:len(includes):len(includes)]
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
	interp ANY,
	interpInput *VFS,
	toolRefs []NodeRef,
	kv *KV,
	resources []STR,
	emit *StreamingEmitter,
) NodeRef {
	na := emit.nodeArenas()
	env := envVarsVCS

	if len(stmt.EnvPairs) > 0 {
		block := na.envs.alloc(1 + len(stmt.EnvPairs))[:0]

		block = append(block, envVarsVCS...)

		for _, kv := range stmt.EnvPairs {
			parts := strings.SplitN(kv.string(), "=", 2)

			if len(parts) == 2 {
				block = append(block, EnvVar{Name: internEnv(parts[0]), Value: internStr(parts[1]).any()})
			} else {
				block = append(block, EnvVar{Name: internEnv(kv.string()), Value: strEmpty.any()})
			}
		}

		na.envs.commit(len(block))

		env = EnvVars(block[:len(block):len(block)])
	}

	cmdArgs := na.anys.alloc(2 + len(stmt.Args))[:0]

	cmdArgs = append(cmdArgs, interp, (scriptVFS).any())

	for _, aTok := range stmt.Args {
		a := aTok.string()

		if a == "${ARCADIA_ROOT}" {
			cmdArgs = append(cmdArgs, argS.any())

			continue
		}

		if vfs, ok := inVFSByToken[a]; ok && !strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
			cmdArgs = append(cmdArgs, vfs.any())

			continue
		} else if vfs, ok := outVFSByToken[a]; ok && !strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
			cmdArgs = append(cmdArgs, vfs.any())

			continue
		} else if k, val, ok := strings.Cut(a, "="); ok && !strings.HasPrefix(a, "-") {
			if vfs, ok := inVFSByToken[val]; ok {
				cmdArgs = append(cmdArgs, internV(k, "=", vfs.prefix(), vfs.relString()).any())

				continue
			}
		}

		cmdArgs = append(cmdArgs, aTok)
	}

	na.anys.commit(len(cmdArgs))

	cmdArgs = cmdArgs[:len(cmdArgs):len(cmdArgs)]

	head := na.vfs.alloc(2 + len(stmt.INFiles))[:0]

	if interpInput != nil {
		head = append(head, *interpInput)
	}

	head = append(head, scriptVFS)

	for _, f := range stmt.INFiles {
		head = append(head, inVFSByToken[f.string()])
	}

	na.vfs.commit(len(head))

	head = head[:len(head):len(head)]

	inputs := na.inputList(head, inputClosure)
	outputs := na.vfs.alloc(1 + len(stmt.OUTFiles) + len(stmt.OUTNoAutoFiles))[:0]

	var stdoutPath VFS

	if stdoutVFS != nil {
		stdoutPath = *stdoutVFS
		outputs = append(outputs, *stdoutVFS)
	}

	for _, f := range stmt.OUTFiles {
		outputs = append(outputs, outVFSByToken[f.string()])
	}

	for _, f := range stmt.OUTNoAutoFiles {
		outputs = append(outputs, outVFSByToken[f.string()])
	}

	na.vfs.commit(len(outputs))

	outputs = outputs[:len(outputs):len(outputs)]

	cmd := Cmd{CmdArgs: na.chunkList(cmdArgs), Env: env}

	if stdoutPath != 0 {
		cmd.Stdout = stdoutPath
	}

	if stmt.CWD != nil {
		cmd.Cwd = cwdVFS((*stmt.CWD).string())
	}

	node := Node{
		Platform:       instance.Platform,
		Cmds:           na.cmdList(cmd),
		Env:            env,
		Inputs:         inputs,
		KV:             kv,
		Outputs:        outputs,
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:        na.noderefs.list(extraDepRefs...),
		ForeignDepRefs: na.noderefs.list(toolRefs...),
		Resources:      resources,
	}

	emit.emitReservedNode(node, id)

	return id
}
