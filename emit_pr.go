package main

import (
	"path/filepath"
	"strings"
)

type RunProgramsForARResult struct {
	CCRefs    []NodeRef
	CCOutputs []VFS
}

type RunProgramAuxTool struct {
	token string
	ref   NodeRef
	bin   VFS
}

func emitRunProgramsForAR(ctx *GenCtx, instance ModuleInstance, d *ModuleData, in ModuleCCInputs) *RunProgramsForARResult {
	if len(d.runPrograms) == 0 {
		return nil
	}

	reg := codegenRegForInstance(ctx, instance)
	res := &RunProgramsForARResult{}

	for _, rp := range d.runPrograms {
		prRef := emitRunProgram(ctx, instance, rp, d, reg, in)

		outs := make([]string, 0, len(rp.OUTFiles)+len(rp.OUTNoAutoFiles)+1)
		outs = append(outs, strStrings(rp.OUTFiles)...)

		if rp.StdoutFile != nil {
			outs = append(outs, rp.StdoutFile.string())
		}

		for _, out := range outs {
			if !isCCSourceExt(out) {
				continue
			}

			ccRef, ccOut := emitPRDownstreamCC(ctx, instance, out, prRef, in)
			res.CCRefs = append(res.CCRefs, ccRef)
			res.CCOutputs = append(res.CCOutputs, ccOut)
		}
	}

	return res
}

func emitRunProgram(ctx *GenCtx, instance ModuleInstance, stmt *RunProgramStmt, d *ModuleData, reg *CodegenRegistry, moduleInputs ModuleCCInputs) NodeRef {
	res := ctx.toolResult(internArg(filepath.Clean(stmt.ToolPath.string())))
	toolLDRef := res.LDRef
	toolBinPath := *res.LDPath
	auxTools := resolveRunProgramAuxTools(ctx, strStrings(stmt.ToolPaths))
	inVFSByToken := make(map[STR]VFS, len(stmt.INFiles))
	inVFSs := make([]VFS, 0, len(stmt.INFiles))

	for _, f := range stmt.INFiles {
		vfs := runProgramInputVFS(ctx, instance, d, f.string())
		inVFSByToken[f] = vfs
		inVFSs = append(inVFSs, vfs)
	}

	outVFSByToken := make(map[STR]VFS, len(stmt.OUTFiles)+len(stmt.OUTNoAutoFiles)+1)

	for _, f := range stmt.OUTFiles {
		outVFSByToken[f] = copyFileOutputVFS(instance.Path.rel(), f.string())
	}

	for _, f := range stmt.OUTNoAutoFiles {
		outVFSByToken[f] = copyFileOutputVFS(instance.Path.rel(), f.string())
	}

	var stdoutVFS *VFS

	if stmt.StdoutFile != nil {
		vfs := copyFileOutputVFS(instance.Path.rel(), stmt.StdoutFile.string())
		stdoutVFS = &vfs
		outVFSByToken[*stmt.StdoutFile] = vfs
	}

	// The run's $(S) source inputs are real inputs of any unit that transitively
	// consumes a generated output (directly, or after the output is archived into
	// an .inc that a CC unit #includes). Record them on each output so the archive
	// emit can propagate them as closure leaves (see emitArchive).
	//
	// A $(B) input is itself a codegen intermediate (e.g. a RUN_ANTLR-generated
	// .proto). Its own $(S) generator sources (grammar/template/jar/scripts) are
	// real inputs of every consumer of this run's outputs too, so fold them in:
	// SourceInputs is transitive through the producer chain. prGeneratedFromSources
	// (the $(B)-derived subset) additionally rides as a non-expanded ClosureLeaf of
	// each output, so walkClosure carries it to consumers — the vehicle
	// emit_proto.go uses, replacing the old fake `#include "X.proto"` bridge.
	var prSourceInputs []VFS
	var prGeneratedFromSources []VFS

	for _, v := range inVFSs {
		if v.isSource() {
			prSourceInputs = append(prSourceInputs, v)

			continue
		}

		if info := reg.lookup(v); info != nil {
			prGeneratedFromSources = append(prGeneratedFromSources, info.SourceInputs...)
		}
	}

	prSourceInputs = append(prSourceInputs, prGeneratedFromSources...)

	// Reserve the PR producer's ref before registering its outputs: the input
	// closure walk below resolves sibling codegen deps that may include these
	// outputs, and registration records the producer ref.
	prRef := ctx.emit.reserve()

	// A RUN_PROGRAM may name the same file in more than one output role —
	// e.g. STDOUT and OUT_NOAUTO pointing at the same generated artifact (the
	// program's stdout *is* that declared output). They denote one physical
	// output, so register each distinct output VFS exactly once; a second
	// registration would trip the codegen registry's duplicate-producer guard.
	registeredPROut := map[VFS]bool{}

	registerPROutput := func(out VFS, parsed []IncludeDirective) {
		if registeredPROut[out] {
			return
		}

		registeredPROut[out] = true

		registerBoundGeneratedParsedOutput(ctx, instance, pkPR, out, parsed, prRef, []NodeRef{toolLDRef})
		reg.setSourceInputs(out, prSourceInputs)

		for _, s := range prGeneratedFromSources {
			reg.addClosureLeaf(out, s)
		}
	}

	for _, f := range stmt.OUTFiles {
		registerPROutput(outVFSByToken[f], prEmitsIncludes(f, stmt, inVFSs))
	}

	for _, f := range stmt.OUTNoAutoFiles {
		registerPROutput(outVFSByToken[f], prEmitsIncludes(f, stmt, inVFSs))
	}

	if stmt.StdoutFile != nil {
		registerPROutput(*stdoutVFS, prEmitsIncludes(*stmt.StdoutFile, stmt, inVFSs))
	}

	inputClosure := prInputClosure(ctx, instance, d, stmt, moduleInputs)

	// Exclude prRef as well as the tool: the outputs are now registered against
	// prRef, so a PR output appearing in another output's closure must not become a
	// self-dependency (the old two-phase code bound the ref only after this resolve).
	prExtraDepRefs := resolveCodegenDepRefs(ctx, instance, inputClosure, toolLDRef, prRef)

	emitPR(instance, stmt, toolBinPath, toolLDRef, auxTools, inVFSByToken, inVFSs, outVFSByToken, stdoutVFS, inputClosure, prExtraDepRefs, prRef, ctx.emit)

	return prRef
}

func isCCSourceExt(p string) bool {
	return strings.HasSuffix(p, ".cpp") ||
		strings.HasSuffix(p, ".cc") ||
		strings.HasSuffix(p, ".cxx") ||
		strings.HasSuffix(p, ".c")
}

func generatedOutputCarriesIncludes(p string) bool {
	return isCCSourceExt(p) || isHeaderSource(p) || strings.HasSuffix(p, ".inc")
}

func prInputClosure(ctx *GenCtx, instance ModuleInstance, d *ModuleData, stmt *RunProgramStmt, moduleInputs ModuleCCInputs) []VFS {
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
		sub := walkClosureTail(ctx.scannerFor(instance), buildRootPath, scanIn.ScanCfg)
		out = append(out, sub...)
	}
	walkInput := func(rel string) {
		inputVFS := runProgramInputVFS(ctx, instance, d, rel)
		sub := walkClosure(ctx.scannerFor(instance), inputVFS, scanIn.ScanCfg)
		out = append(out, sub...)
	}

	for _, f := range stmt.OUTFiles {
		if !isCCSourceExt(f.string()) {
			continue
		}

		walkOne(f.string())
	}

	// OUT_NOAUTO outputs use upstream's `${hide;noauto;output:OUT_NOAUTO}`
	// modifier: registered as outputs but explicitly EXCLUDED from the
	// auto-input/scan chain — the PR node does not walk their closures.
	// (yql/.../v1_proto_split_antlr4 uses OUT_NOAUTO for .pb.h/.pb.cc, and
	// upstream tracks only IN + tools as PR inputs; walking the .pb.cc here
	// over-emits 1253 libcxx/protobuf headers via the parsed pb.h chain.)
	if stmt.StdoutFile != nil && isCCSourceExt(stmt.StdoutFile.string()) {
		walkOne(stmt.StdoutFile.string())
	}

	// Upstream's RUN_PROGRAM macro registers every IN as an input with
	// scan-on-include (`${hide;input:IN}`); the scanner walks each IN's
	// parsed-include closure when the file's extension is explicitly mapped
	// to an include parser (cpp/h/.h.in/etc.). Files outside that map —
	// Jinja templates (.jnj), JSON, libmagic Magdir entries without an
	// extension — must not be parsed: our default parser is the C parser,
	// and it would surface spurious `#include "/mach-o/fat.h"` matches on
	// random binary data. Gate IN-walk on hasRegisteredParser so unknown
	// extensions contribute zero closure entries (matches REF's
	// yql_*_expr_nodes.gen.h PR nodes, which list only the tool + IN
	// files).
	for _, f := range stmt.INFiles {
		if rel := f.string(); includeDirectiveParsers.hasRegisteredParser(rel) {
			walkInput(rel)
		}
	}

	// OUTPUT_INCLUDES contribute to the PR node's AUTO_INPUT only when the
	// target resolves to a codegen output (.pb.h from a peer PROTO_LIBRARY,
	// .h registered by another PR, etc.). Source-tree headers in
	// OUTPUT_INCLUDES — yql_*_expr_nodes_gen.h, util/generic/hash_set.h, any
	// path that already lives in the C include graph — do NOT contribute
	// here in upstream: the PR node's own include graph is rooted at IN, and
	// pulling source-tree closures via OUTPUT_INCLUDES would massively
	// over-emit (yql_*_expr_nodes.gen.h would gain libcxx).
	//
	// For .pb.h OUTPUT_INCLUDES, upstream tracks the listed .pb.h itself
	// plus the TRANSITIVE .proto SOURCES of the proto-import graph rooted at
	// that .pb.h's proto — but NOT the intermediate .pb.h headers along the
	// chain (control_board_proto.h's OUTPUT_INCLUDES tablet.pb.h + config.pb.h
	// lands tablet.pb.h, config.pb.h, and the 153 transitive .proto sources,
	// without the 148 deep .pb.h headers our closure walk otherwise gathers).
	// Filter the walk: keep the OUTPUT_INCLUDES VFS itself and every .proto
	// reached through it; drop transitive .pb.h.
	{
		reg := codegenRegForInstance(ctx, instance)

		for _, oi := range stmt.OutputIncludes {
			target := oi

			if vfsHasPrefix(target.string()) {
				target = internStr(intern(target.string()).rel())
			}

			candidate := build(target.string())
			info := reg.lookup(candidate)

			if info == nil {
				continue
			}

			sub := walkClosureTail(ctx.scannerFor(instance), info.OutputPath, scanIn.ScanCfg)

			for _, v := range sub {
				if !strings.HasSuffix(v.rel(), ".proto") {
					continue
				}

				out = append(out, v)

				// Protobuf WKTs (google/protobuf/{any,duration,empty,struct,
				// timestamp,...}.proto) ship pre-built `.pb.h` headers checked
				// in alongside the .proto. Upstream lists both the .proto and
				// the pre-built .pb.h as PR inputs when the chain transits
				// through one. For purely-generated .pb.h's (no source-tree
				// .pb.h sibling) the IsFile probe returns false, so this is a
				// no-op outside the WKT path.
				if v.isSource() {
					sibling := strings.TrimSuffix(v.rel(), ".proto") + ".pb.h"
					sibDir, sibBase := splitDirName(sibling)

					if ctx.fs.isFile(dirKey(sibDir), sibBase) {
						out = append(out, source(sibling))
					}
				}
			}
		}
	}

	if len(out) == 0 {
		return nil
	}

	out = dedupVFS(out, nil)

	return out
}

// prEmitsIncludes builds the parsed-include set registered on one PR output.
// inVFSs mirrors stmt.INFiles in order (computed once by emitRunProgram), so the
// per-output call needn't re-resolve every IN file.
func prEmitsIncludes(outFile STR, stmt *RunProgramStmt, inVFSs []VFS) []IncludeDirective {
	if !generatedOutputCarriesIncludes(outFile.string()) {
		return nil
	}

	includes := make([]IncludeDirective, 0, len(inVFSs)+len(stmt.OutputIncludes))

	for _, v := range inVFSs {
		// A generated output never #includes its $(B) inputs — those are codegen
		// intermediates (e.g. a RUN_ANTLR-generated .proto) reached via the
		// producer dep edge, not C++ includes. Their $(S) generator sources ride
		// to consumers as this output's ClosureLeaves (see emitRunProgram),
		// matching emit_proto.go; fake-including the intermediate here dragged the
		// $(B) file into every consumer's closure.
		if v.isBuild() {
			continue
		}

		includes = append(includes, IncludeDirective{kind: includeQuoted, target: internStr(v.rel())})
	}

	for _, f := range stmt.OutputIncludes {
		if v := f.vfs(); v != 0 {
			f = internStr(v.rel())
		}

		includes = append(includes, IncludeDirective{kind: includeQuoted, target: f})
	}

	return includes
}

func resolveRunProgramAuxTools(ctx *GenCtx, toolPaths []string) []RunProgramAuxTool {
	if len(toolPaths) == 0 {
		return nil
	}

	out := make([]RunProgramAuxTool, 0, len(toolPaths))
	seen := make(map[string]struct{}, len(toolPaths))

	for _, toolPath := range toolPaths {
		if _, dup := seen[toolPath]; dup {
			continue
		}

		seen[toolPath] = struct{}{}
		res := ctx.toolResult(internArg(filepath.Clean(toolPath)))
		out = append(out, RunProgramAuxTool{
			token: toolPath,
			ref:   res.LDRef,
			bin:   *res.LDPath,
		})
	}

	return out
}

func runProgramInputVFS(ctx *GenCtx, instance ModuleInstance, d *ModuleData, rel string) VFS {
	switch {
	case strings.HasPrefix(rel, "$(S)/"),
		strings.HasPrefix(rel, "$(B)/"),
		strings.HasPrefix(rel, "${ARCADIA_ROOT}/"),
		strings.HasPrefix(rel, "${CURDIR}/"),
		strings.HasPrefix(rel, "${ARCADIA_BUILD_ROOT}/"),
		strings.HasPrefix(rel, "${BINDIR}/"):
		return copyFileInputVFS(ctx.fs, instance.Path.rel(), rel)
	}

	buildVFS := build(filepath.ToSlash(filepath.Clean(instance.Path.rel() + "/" + rel)))

	if codegenRegForInstance(ctx, instance).lookup(buildVFS) != nil {
		return buildVFS
	}

	if ctx.fs.isFile(srcRootVFS, rel) {
		return source(rel)
	}

	resolved := resolveModuleSourceVFS(ctx, instance, d, rel, d.srcDirs)

	// A resolved $(S) source that does not exist on disk is not a real source:
	// it is the output of an unmodelled codegen (e.g. a FROM_SANDBOX OUT_NOAUTO
	// like `trie`), which lives in $(B). Treat it as the generated build
	// artifact so it is not content-hashed as a missing source. The producing
	// node is a node-count gap to close once that codegen is modelled.
	if resolved.isSource() && !ctx.fs.isFile(srcRootVFS, resolved.rel()) {
		return buildVFS
	}

	return resolved
}

func emitPR(
	instance ModuleInstance,
	stmt *RunProgramStmt,
	toolBinPath VFS,
	toolLDRef NodeRef,
	auxTools []RunProgramAuxTool,
	inVFSByToken map[STR]VFS,
	inVFSs []VFS,
	outVFSByToken map[STR]VFS,
	stdoutVFS *VFS,
	inputClosure []VFS,
	extraDepRefs []NodeRef,
	id NodeRef,
	emit Emitter,
) {
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

	cmdArgs := make([]STR, 0, 1+len(stmt.Args))
	cmdArgs = append(cmdArgs, (toolBinPath).str())

	for _, aTok := range stmt.Args {
		a := aTok.string()
		key := aTok

		for _, tool := range auxTools {
			if strings.Contains(a, tool.token) {
				a = strings.ReplaceAll(a, tool.token, tool.bin.string())
				key = internStr(a)
			}
		}

		if !strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
			if vfs, ok := inVFSByToken[key]; ok {
				cmdArgs = append(cmdArgs, vfs.str())

				continue
			}

			if vfs, ok := outVFSByToken[key]; ok {
				cmdArgs = append(cmdArgs, vfs.str())

				continue
			}

			// A path with a trailing modifier (e.g. CFFI's `build.py:ffi`): the head
			// before ':' is the relative path declared in IN/OUT, which "becomes
			// absolute" per RUN_PROGRAM semantics; the modifier rides along.
			if i := strings.IndexByte(a, ':'); i > 0 {
				head := internStr(a[:i])

				if vfs, ok := inVFSByToken[head]; ok {
					cmdArgs = append(cmdArgs, internStr(vfs.string()+a[i:]))

					continue
				}

				if vfs, ok := outVFSByToken[head]; ok {
					cmdArgs = append(cmdArgs, internStr(vfs.string()+a[i:]))

					continue
				}
			}
		}

		cmdArgs = append(cmdArgs, key)
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

	// inVFSs mirrors stmt.INFiles in order — same values the token map holds,
	// without re-probing it.
	for _, v := range inVFSs {
		appendUnique(v)
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
		outputs = append(outputs, outVFSByToken[f])
	}

	for _, f := range stmt.OUTNoAutoFiles {
		outputs = append(outputs, outVFSByToken[f])
	}

	var toolRefs []NodeRef

	for _, tool := range auxTools {
		toolRefs = append(toolRefs, depRefs(tool.ref)...)
	}

	toolRefs = append(toolRefs, depRefs(toolLDRef)...)

	deps := append([]NodeRef(nil), extraDepRefs...)

	// toolRefs is a fresh local, not mutated after this; the node owns it as its
	// foreign (tool) deps. The graph's "deps" array is DepRefs ∪ ForeignDepRefs
	// (Node.buildDeps), so the tools are not duplicated here.
	foreignDepRefs := toolRefs

	cmd := Cmd{
		CmdArgs: na.chunkList(cmdArgs),
		Env:     env,
	}

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
		Outputs:          outputs,
		KV:               KV{P: pkPR, PC: pcYellow, ShowOut: true},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel()},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:          deps,
		ForeignDepRefs:   foreignDepRefs,
	}

	emit.emitReserved(node, id)
}
