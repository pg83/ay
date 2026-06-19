package main

import (
	"strings"
)

func emitRunPythonForAR(ctx *GenCtx, instance ModuleInstance, d *ModuleData, in ModuleCCInputs) *RunProgramsForARResult {
	if len(d.runPython) == 0 {
		return nil
	}

	res := &RunProgramsForARResult{}

	for _, rp := range d.runPython {
		pyRef := emitRunPython(ctx, instance, rp, d, in)

		outs := make([]string, 0, len(rp.OUTFiles)+1)
		outs = append(outs, strStrings(rp.OUTFiles)...)

		// Only auto STDOUT is a module source; STDOUT_NOAUTO carries upstream's
		// `noauto` modifier and is excluded, exactly like OUT_NOAUTO.
		if rp.StdoutFile != nil && !rp.StdoutNoAuto {
			outs = append(outs, rp.StdoutFile.string())
		}

		for _, out := range outs {
			switch {
			case isCCSourceExt(out):
				ccRef, ccOut := emitPRDownstreamCC(ctx, instance, out, pyRef, in)
				res.CCRefs = append(res.CCRefs, ccRef)
				res.CCOutputs = append(res.CCOutputs, ccOut)
			case isAsmSourceExt(out):
				asRef, asOut := emitCodegenDownstreamAS(ctx, instance, out, []NodeRef{pyRef}, in)
				res.CCRefs = append(res.CCRefs, asRef)
				res.CCOutputs = append(res.CCOutputs, asOut)
			}
		}
	}

	return res
}

func emitRunPython(ctx *GenCtx, instance ModuleInstance, stmt *RunPythonStmt, d *ModuleData, moduleInputs ModuleCCInputs) NodeRef {
	scriptVFS := copyFileInputVFS(ctx.fs, instance.Path.rel(), stmt.ScriptPath.string())
	inVFSByToken := make(map[string]VFS, len(stmt.INFiles))
	inVFSs := make([]VFS, 0, len(stmt.INFiles))

	for _, f := range stmt.INFiles {
		vfs := runProgramInputVFS(ctx, instance, d, f.string())
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

	// Detect split-codegen pattern; precompute source inputs when matched.
	hasCCShard, _ := splitCodegenDetect(stmt)
	var splitSrcs []VFS

	if hasCCShard {
		splitSrcs = splitCodegenSrcs(ctx, instance, d, stmt, scriptVFS)
	}

	// The run's $(S) source inputs (the python script + $(S) IN files) are real
	// inputs of any unit that transitively consumes a generated output — directly,
	// or after the output is archived (ARCHIVE_ASM's .rodata embeds out.dict.bin,
	// whose RD compile must carry the script + IN leaves). A $(B) IN that is itself
	// a codegen intermediate folds in its own $(S) generator sources, transitively;
	// the $(B)-derived subset additionally rides as a non-expanded closure leaf so
	// walkClosure carries it to consumers. Mirrors emitRunProgram.
	var pySourceInputs []VFS
	var pyGeneratedFromSources []VFS

	if scriptVFS.isSource() {
		pySourceInputs = append(pySourceInputs, scriptVFS)
	}

	reg := codegenRegForInstance(ctx, instance)

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

	// Reserve the PY producer's ref before registering its outputs; see emit_pr.go.
	pyRef := ctx.emit.reserve()

	registerPYOutput := func(out VFS, parsed []IncludeDirective) {
		registerBoundGeneratedParsedOutput(ctx, instance, pkPY, out, parsed, pyRef, nil)
		reg.setSourceInputs(out, pySourceInputs)

		for _, s := range pyGeneratedFromSources {
			reg.addClosureLeaf(out, s)
		}
	}

	for _, f := range stmt.OUTFiles {
		registerPYOutput(outVFSByToken[f.string()], pyEmitsIncludes(ctx, instance, d, stmt, f.string(), scriptVFS, splitSrcs, hasCCShard))
	}

	for _, f := range stmt.OUTNoAutoFiles {
		registerPYOutput(outVFSByToken[f.string()], pyEmitsIncludes(ctx, instance, d, stmt, f.string(), scriptVFS, splitSrcs, hasCCShard))
	}

	if stmt.StdoutFile != nil {
		registerPYOutput(*stdoutVFS, pyEmitsIncludes(ctx, instance, d, stmt, stmt.StdoutFile.string(), scriptVFS, splitSrcs, hasCCShard))
	}

	inputClosure := pyInputClosure(ctx, instance, stmt, d, moduleInputs)
	// Exclude pyRef: outputs are now registered against it; see emit_pr.go.
	extraDepRefs := resolveCodegenDepRefs(ctx, instance, inputClosure, pyRef)

	return emitPYRun(instance, stmt, scriptVFS, inVFSByToken, outVFSByToken, stdoutVFS, inputClosure, extraDepRefs, pyRef, moduleInputs.TC, ctx.emit)
}

func pyInputClosure(ctx *GenCtx, instance ModuleInstance, stmt *RunPythonStmt, d *ModuleData, moduleInputs ModuleCCInputs) []VFS {
	scanIn := ModuleCCInputs{
		TC:                d.tc,
		InclArgs:          ctx.inclArgs,
		Flags:             moduleInputs.Flags,
		AddIncl:           moduleInputs.AddIncl,
		PeerAddInclGlobal: moduleInputs.PeerAddInclGlobal,
		SrcDirs:           moduleInputs.SrcDirs,
		FS:                ctx.fs,
		ScanCfg:           newScanContext(ctx.parsers, moduleInputs.AddIncl, moduleInputs.PeerAddInclGlobal, includeScannerBasePaths(), instance.Path.rel()),
	}

	var out []VFS
	walkOne := func(rel string) {
		buildRootPath := copyFileOutputVFS(instance.Path.rel(), rel)
		out = append(out, walkClosureTail(ctx.scannerFor(instance), buildRootPath, scanIn.ScanCfg)...)
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
			vfs := runProgramInputVFS(ctx, instance, d, f.string())
			out = append(out, walkClosure(ctx.scannerFor(instance), vfs, scanIn.ScanCfg)...)
		}
	} else {
		// Walk every output that carries includes — the same predicate under which
		// pyEmitsIncludes registered its parsed includes (script + IN +
		// OUTPUT_INCLUDES). For a header output with OUTPUT_INCLUDES (e.g.
		// feature.gen.h listing feature.h) this resolves the registered feature.h
		// and folds its transitive header closure into the producing node's inputs,
		// matching upstream. CC-only would miss header outputs.
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

	out = dedupVFS(out, nil)

	return out
}

// splitCodegenDetect reports whether stmt matches the split-codegen pattern:
// OUT_NOAUTO contains BOTH CC source shards (*.pb.codeN.cc, *.pb.data.cc)
// AND header outputs (*.pb.main.h, *.pb.classes.h).
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
func splitCodegenSrcs(ctx *GenCtx, instance ModuleInstance, d *ModuleData, stmt *RunPythonStmt, scriptVFS VFS) []VFS {
	reg := codegenRegForInstance(ctx, instance)
	seen := make(map[VFS]struct{}, 32)
	var sources []VFS
	addSource := func(v VFS) {
		if !v.isSource() {
			return
		}

		if _, dup := seen[v]; dup {
			return
		}

		seen[v] = struct{}{}
		sources = append(sources, v)
	}

	addInducedSources := func(deps []IncludeDirective) {
		for _, d := range deps {
			// INDUCED_DEPS targets arrive rooted ($(S)/... — the reserved
			// ${ARCADIA_ROOT}-family spellings); the STR already backs the
			// full path, so the binding is a shift.
			if v := d.target.vfs(); v != 0 {
				if v.isSource() && ctx.fs.isFile(srcRootVFS, v.rel()) {
					addSource(v)
				}

				continue
			}

			target := d.target.string()

			if ctx.fs.isFile(srcRootVFS, target) {
				addSource(source(target))
			}
		}
	}

	addSource(scriptVFS)

	for _, f := range stmt.INFiles {
		vfs := runProgramInputVFS(ctx, instance, d, f.string())

		if vfs.isSource() {
			addSource(vfs)

			continue
		}

		if info := reg.lookup(vfs); info != nil {
			// The IN file's own transitive source inputs — e.g. the antlr
			// grammar/template/jar/scripts behind a generated .pb.h/.pb.cc, folded
			// in by its producer (emitRunProgram). Read them directly from the
			// registry rather than chasing parsedIncludes.
			for _, si := range info.SourceInputs {
				addSource(si)
			}

			// The IN file's producing tools' INDUCED_DEPS (e.g. protoc's runtime
			// set for a generated .pb.cc): pull the source-rooted ones in,
			// mirroring the scanner's resolveInducedDeps. Shards are translation
			// units, so take the Cpp bucket (cpp-only + h+cpp induced groups).
			for _, gref := range info.GeneratorRefs {
				if tool, ok := ctx.moduleByRef.get(gref); ok {
					addInducedSources(tool.InducedDeps.bucket(parsedIncludesCpp))
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
func pyEmitsIncludes(ctx *GenCtx, instance ModuleInstance, d *ModuleData, stmt *RunPythonStmt, outFile string, scriptVFS VFS, splitSrcs []VFS, splitHasCCShard bool) []IncludeDirective {
	if !generatedOutputCarriesIncludes(outFile) {
		return nil
	}

	if splitHasCCShard && len(splitSrcs) > 0 {
		// Find the first shard CC file (code0.cc or equivalent) once.
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
			// Non-first shards register code0.cc as their first parsedInclude so
			// the scanner's closure walk adds code0.cc to their input set.
			// Upstream shard CC nodes for code1..codeN and data carry code0.cc as
			// an input; code0.cc itself carries only splitSrcs.
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
			// First shard CC as meta-include so downstream consumers of pb.main.h
			// carry code0.cc in their include-input closure.
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
		includes = append(includes, IncludeDirective{kind: includeQuoted, target: internStr(runProgramInputVFS(ctx, instance, d, f.string()).rel())})
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
	emit Emitter,
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
	deduper.reset()
	appendUnique := func(vfs VFS) {
		if !deduper.add(vfs) {
			return
		}

		head = append(head, vfs)
	}
	appendUnique(scriptVFS)

	for _, f := range stmt.INFiles {
		appendUnique(inVFSByToken[f.string()])
	}

	// The closure tail is filtered against the head set; filterSeen returns
	// inputClosure itself when nothing collides, so the closure is referenced,
	// not copied, into the chunk list.
	inputs := na.inputList(head, deduper.filterSeen(inputClosure))

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
		Platform:         instance.Platform,
		Cmds:             na.cmdList(cmd),
		Env:              env,
		Inputs:           inputs,
		KV:               KV{P: pkPY, PC: pcYellow, ShowOut: true},
		Outputs:          outputs,
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel()},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:          extraDepRefs,
		Resources:        usesPython3,
	}

	emit.emitReserved(node, id)

	return id
}
