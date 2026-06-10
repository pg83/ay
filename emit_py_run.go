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
	scriptVFS := copyFileInputVFS(ctx.fs, instance.Path.Rel(), stmt.ScriptPath)
	inVFSByToken := make(map[string]VFS, len(stmt.INFiles))
	inVFSs := make([]VFS, 0, len(stmt.INFiles))

	for _, f := range stmt.INFiles {
		vfs := runProgramInputVFS(ctx, instance, d, f)
		inVFSByToken[f] = vfs
		inVFSs = append(inVFSs, vfs)
	}

	outVFSByToken := make(map[string]VFS, len(stmt.OUTFiles)+len(stmt.OUTNoAutoFiles)+1)

	for _, f := range stmt.OUTFiles {
		outVFSByToken[f] = copyFileOutputVFS(instance.Path.Rel(), f)
	}

	for _, f := range stmt.OUTNoAutoFiles {
		outVFSByToken[f] = copyFileOutputVFS(instance.Path.Rel(), f)
	}

	var stdoutVFS *VFS

	if stmt.StdoutFile != nil {
		vfs := copyFileOutputVFS(instance.Path.Rel(), *stmt.StdoutFile)
		stdoutVFS = &vfs
		outVFSByToken[*stmt.StdoutFile] = vfs
	}

	// Detect split-codegen pattern; precompute source inputs when matched.
	hasCCShard, _ := splitCodegenDetect(stmt)
	var splitSrcs []VFS

	if hasCCShard {
		splitSrcs = splitCodegenSrcs(ctx, instance, d, stmt, scriptVFS)
	}

	if reg != nil {
		for _, f := range stmt.OUTFiles {
			registerGeneratedParsedOutput(ctx, instance, pkPY, outVFSByToken[f], pyEmitsIncludes(ctx, instance, d, stmt, f, scriptVFS, splitSrcs, hasCCShard), nil)
		}

		for _, f := range stmt.OUTNoAutoFiles {
			registerGeneratedParsedOutput(ctx, instance, pkPY, outVFSByToken[f], pyEmitsIncludes(ctx, instance, d, stmt, f, scriptVFS, splitSrcs, hasCCShard), nil)
		}

		if stmt.StdoutFile != nil {
			registerGeneratedParsedOutput(ctx, instance, pkPY, *stdoutVFS, pyEmitsIncludes(ctx, instance, d, stmt, *stmt.StdoutFile, scriptVFS, splitSrcs, hasCCShard), nil)
		}
	}

	inputClosure := pyInputClosure(ctx, instance, stmt, d, moduleInputs)
	codegenInputs := append([]VFS{scriptVFS}, inVFSs...)
	extraDepRefs := resolveCodegenDepRefsExt(ctx, instance, inputClosure, codegenInputs)
	result := EmitPYRun(instance, stmt, scriptVFS, inVFSByToken, outVFSByToken, stdoutVFS, inputClosure, extraDepRefs, moduleInputs.TC, ctx.emit)

	if d.prOutputInputs == nil {
		d.prOutputInputs = map[string][]VFS{}
	}

	// result.Inputs is a fresh, never-mutated slice; the reader
	// (prResourceExtraInputs) copies out, so sharing it across keys is safe.
	for _, f := range stmt.OUTFiles {
		d.prOutputInputs[f] = result.Inputs
	}

	for _, f := range stmt.OUTNoAutoFiles {
		d.prOutputInputs[f] = result.Inputs
	}

	if stmt.StdoutFile != nil {
		d.prOutputInputs[*stmt.StdoutFile] = result.Inputs
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

func pyInputClosure(ctx *genCtx, instance ModuleInstance, stmt *RunPythonStmt, d *moduleData, moduleInputs ModuleCCInputs) []VFS {
	scanIn := ModuleCCInputs{
		TC:                d.tc,
		InclArgs:          ctx.inclArgs,
		Flags:             moduleInputs.Flags,
		AddIncl:           moduleInputs.AddIncl,
		PeerAddInclGlobal: moduleInputs.PeerAddInclGlobal,
		SrcDirs:           moduleInputs.SrcDirs,
		SourceRoot:        ctx.sourceRoot,
		FS:                ctx.fs,
	}

	var out []VFS
	walkOne := func(rel string) {
		buildRootPath := copyFileOutputVFS(instance.Path.Rel(), rel)
		out = append(out, walkClosure(ctx, instance, buildRootPath, scanIn)...)
	}

	hasCCShard, _ := splitCodegenDetect(stmt)

	if hasCCShard {
		// Split-codegen: the CC shard outputs are registered with splitSrcs
		// (antlr chain + induced-dep source headers like arena.h) rather than
		// the monolithic build-generated IN files (pb.h, pb.cc).  Walking the
		// shard outputs here would cause forEachResolvedChildID to compute and
		// CACHE closureOf(arena.h) with the current (potentially incomplete)
		// scan context, breaking later consumers that reuse the cached closure.
		// Instead, walk the IN files directly — the same include closure they
		// contribute, without the caching side-effect.  pb.h and pb.cc are both
		// build-generated IN files; walking them here (via their registered
		// parsedIncludes) produces the correct PY-node input set.
		for _, f := range stmt.INFiles {
			vfs := runProgramInputVFS(ctx, instance, d, f)
			out = append(out, walkClosure(ctx, instance, vfs, scanIn)...)
		}
	} else {
		// Walk every output that carries includes — the same predicate under which
		// pyEmitsIncludes registered its parsed includes (script + IN +
		// OUTPUT_INCLUDES). For a header output with OUTPUT_INCLUDES (e.g.
		// feature.gen.h listing feature.h) this resolves the registered feature.h
		// and folds its transitive header closure into the producing node's inputs,
		// matching upstream. CC-only would miss header outputs.
		for _, f := range stmt.OUTFiles {
			if generatedOutputCarriesIncludes(f) {
				walkOne(f)
			}
		}

		for _, f := range stmt.OUTNoAutoFiles {
			if generatedOutputCarriesIncludes(f) {
				walkOne(f)
			}
		}

		if stmt.StdoutFile != nil && generatedOutputCarriesIncludes(*stmt.StdoutFile) {
			walkOne(*stmt.StdoutFile)
		}
	}

	out = dropTransitiveGeneratedProto(out)

	if len(out) == 0 {
		return nil
	}

	out = dedupVFS(out, nil)
	return out
}

// splitCodegenDetect reports whether stmt matches the split-codegen pattern:
// OUT_NOAUTO contains BOTH CC source shards (*.pb.codeN.cc, *.pb.data.cc)
// AND header outputs (*.pb.main.h, *.pb.classes.h).
func splitCodegenDetect(stmt *RunPythonStmt) (hasCCShard bool, hasHeader bool) {
	for _, f := range stmt.OUTNoAutoFiles {
		if isCCSourceExt(f) {
			hasCCShard = true
		}

		if isHeaderSource(f) {
			hasHeader = true
		}
	}

	return
}

// splitCodegenSrcs computes the source-level include directives for
// split-codegen shard CC outputs.  It expands exactly ONE level of the
// registered parsedIncludes for each build-generated IN file:
//   - Source entries (toolInducedDeps like arena.h): added directly.
//   - Build-generated entries (like $(B)/proto from RUN_ANTLR):
//     their SourceInputs (antlr/configure chain sources) are used.
//
// We do NOT call walkClosure here.  walkClosure computes and caches
// closures in the global subgraphCache with the current module's scan
// context.  The pyInputClosure function (for the PY node) is now
// responsible for walking pb.h directly so closureOf(arena.h) etc. are
// cached with the full scan context BEFORE emitOneSource uses them.
func splitCodegenSrcs(ctx *genCtx, instance ModuleInstance, d *moduleData, stmt *RunPythonStmt, scriptVFS VFS) []VFS {
	reg := codegenRegForInstance(ctx, instance)
	scanner := ctx.scannerFor(instance)
	seen := make(map[VFS]struct{}, 32)
	var sources []VFS
	addSource := func(v VFS) {
		if !v.IsSource() {
			return
		}

		if _, dup := seen[v]; dup {
			return
		}

		seen[v] = struct{}{}
		sources = append(sources, v)
	}

	addInducedSources := func(deps []includeDirective) {
		for _, d := range deps {
			// INDUCED_DEPS targets arrive rooted ($(S)/... — the reserved
			// ${ARCADIA_ROOT}-family spellings); the STR already backs the
			// full path, so the binding is a shift.
			if v := d.target.vfs(); v != 0 {
				if v.IsSource() && ctx.fs.IsFile(srcRootVFS, v.Rel()) {
					addSource(v)
				}

				continue
			}

			target := d.target.String()

			if ctx.fs.IsFile(srcRootVFS, target) {
				addSource(Source(target))
			}
		}
	}

	addSource(scriptVFS)

	if scanner == nil {
		return sources
	}

	for _, f := range stmt.INFiles {
		vfs := runProgramInputVFS(ctx, instance, d, f)

		if vfs.IsSource() {
			addSource(vfs)
			continue
		}

		for _, pd := range scanner.parsers.parsedIncludes(vfs) {
			target := pd.target.String()

			if vfsHasPrefix(target) {
				bvfs := Intern(target)

				if bvfs.IsBuild() && reg != nil {
					if info := reg.Lookup(bvfs); info != nil {
						for _, si := range info.SourceInputs {
							addSource(si)
						}
					}
				}

				continue
			}

			if ctx.fs.IsFile(srcRootVFS, target) {
				addSource(Source(target))
			} else if reg != nil {
				if info := reg.LookupRel(target); info != nil {
					for _, si := range info.SourceInputs {
						addSource(si)
					}
				}
			}
		}

		// The IN file's producing tools' INDUCED_DEPS (e.g. protoc's runtime set for
		// a generated .pb.cc) are no longer woven into its parsedIncludes; pull the
		// source-rooted ones in directly here, mirroring the scanner's
		// resolveInducedDeps. Shards are translation units, so take the Cpp bucket
		// (which holds both the cpp-only and the h+cpp induced groups).
		if reg != nil {
			if info := reg.Lookup(vfs); info != nil {
				for _, gref := range info.GeneratorRefs {
					if tool, ok := ctx.moduleByRef.Get(gref); ok {
						addInducedSources(tool.InducedDeps.bucket(parsedIncludesCpp))
					}
				}
			}
		}
	}

	return sources
}

// pyEmitsIncludes returns the include directives to register for a generated
// output file in a RUN_PYTHON3 statement.
//
// For the split-codegen pattern (OUT_NOAUTO has both CC shards and headers):
//
//   - Shard CC outputs (pb.codeN.cc, pb.data.cc): use splitSrcs (source-level
//     generator inputs from the IN files' parsedIncludes).  Upstream CC compile
//     nodes for shards carry only source-level inputs, not the monolithic pb.h
//     or pb.cc.  pyInputClosure walks pb.h directly (before emitOneSource), so
//     closureOf(arena.h) etc. are cached with the full scan context before the
//     shard CC emitter encounters them.
//
//   - Header outputs (pb.main.h, pb.classes.h): register the FIRST CC shard
//     as a meta-include so consumers carry it in their input closure; then
//     splitSrcs for the actual include chain.  Only the first shard is
//     registered — the others are linked via the AR dep edge.
func pyEmitsIncludes(ctx *genCtx, instance ModuleInstance, d *moduleData, stmt *RunPythonStmt, outFile string, scriptVFS VFS, splitSrcs []VFS, splitHasCCShard bool) []includeDirective {
	if !generatedOutputCarriesIncludes(outFile) {
		return nil
	}

	if splitHasCCShard && len(splitSrcs) > 0 {
		// Find the first shard CC file (code0.cc or equivalent) once.
		var firstShardFile string
		var firstShardVFS VFS

		for _, f := range stmt.OUTNoAutoFiles {
			if isCCSourceExt(f) {
				firstShardFile = f
				firstShardVFS = copyFileOutputVFS(instance.Path.Rel(), f)
				break
			}
		}

		if isCCSourceExt(outFile) {
			// Non-first shards register code0.cc as their first parsedInclude so
			// the scanner's closure walk adds code0.cc to their input set.
			// Upstream shard CC nodes for code1..codeN and data carry code0.cc as
			// an input; code0.cc itself carries only splitSrcs.
			isNonFirst := outFile != firstShardFile
			capacity := len(splitSrcs)

			if isNonFirst {
				capacity++
			}

			includes := make([]includeDirective, 0, capacity)

			if isNonFirst && firstShardVFS != 0 {
				includes = append(includes, includeDirective{kind: includeQuoted, target: internStr(firstShardVFS.Rel())})
			}

			for _, src := range splitSrcs {
				includes = append(includes, includeDirective{kind: includeQuoted, target: internStr(src.Rel())})
			}

			return includes
		}

		if isHeaderSource(outFile) {
			// First shard CC as meta-include so downstream consumers of pb.main.h
			// carry code0.cc in their include-input closure.
			includes := make([]includeDirective, 0, 1+len(splitSrcs))

			if firstShardVFS != 0 {
				includes = append(includes, includeDirective{kind: includeQuoted, target: internStr(firstShardVFS.Rel())})
			}

			for _, src := range splitSrcs {
				includes = append(includes, includeDirective{kind: includeQuoted, target: internStr(src.Rel())})
			}

			return includes
		}
	}

	includes := []includeDirective{{kind: includeQuoted, target: internStr(scriptVFS.Rel())}}

	for _, f := range stmt.INFiles {
		includes = append(includes, includeDirective{kind: includeQuoted, target: internStr(runProgramInputVFS(ctx, instance, d, f).Rel())})
	}

	for _, f := range stmt.OutputIncludes {
		if vfsHasPrefix(f) {
			f = Intern(f).Rel()
		}

		includes = append(includes, includeDirective{kind: includeQuoted, target: internStr(f)})
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
	tc moduleToolchain,
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

	cmdArgs := []STR{tc.Python3, (scriptVFS).str()}

	for _, a := range stmt.Args {
		a = strings.ReplaceAll(a, "${ARCADIA_ROOT}", "$(S)")
		a = strings.ReplaceAll(a, "${ARCADIA_BUILD_ROOT}", "$(B)")
		a = strings.ReplaceAll(a, "${CURDIR}", instance.Path.String())
		a = strings.ReplaceAll(a, "${BINDIR}", Build(instance.Path.Rel()).String())
		a = strings.ReplaceAll(a, "${MODDIR}", instance.Path.Rel())
		a = strings.ReplaceAll(a, "$CURDIR", instance.Path.String())
		a = strings.ReplaceAll(a, "$BINDIR", Build(instance.Path.Rel()).String())

		if vfs, ok := inVFSByToken[a]; ok && !strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
			a = vfs.String()
		} else if vfs, ok := outVFSByToken[a]; ok && !strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
			a = vfs.String()
		}

		cmdArgs = append(cmdArgs, internStr(a))
	}

	inputs := make([]VFS, 0, 1+len(stmt.INFiles)+len(inputClosure))
	deduper.reset()
	appendUnique := func(vfs VFS) {
		if !deduper.add(vfs) {
			return
		}

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

	cmd := Cmd{CmdArgs: cmdArgs, Env: env}

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
		Inputs:           inputChunks{inputs},
		KV:               KV{P: pkPY, PC: pcYellow, ShowOut: true},
		Outputs:          outputs,
		TargetProperties: TargetProperties{ModuleDir: instance.Path.Rel()},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:          extraDepRefs,
		usesResources:    []string{resourcePatternYMakePython3},
	}

	return prEmitResult{
		Ref:    emit.Emit(node),
		Inputs: append([]VFS(nil), inputs...),
	}
}
