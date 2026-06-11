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

		if d.prOutputProducer == nil {
			d.prOutputProducer = map[string]NodeRef{}
		}

		for _, f := range rp.OUTFiles {
			d.prOutputProducer[f] = prRef
		}

		for _, f := range rp.OUTNoAutoFiles {
			d.prOutputProducer[f] = prRef
		}

		if rp.StdoutFile != nil {
			d.prOutputProducer[*rp.StdoutFile] = prRef
		}

		outs := make([]string, 0, len(rp.OUTFiles)+len(rp.OUTNoAutoFiles)+1)
		outs = append(outs, rp.OUTFiles...)

		if rp.StdoutFile != nil {
			outs = append(outs, *rp.StdoutFile)
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
	res := ctx.toolResult(internArg(filepath.Clean(stmt.ToolPath)))
	toolLDRef := res.LDRef
	toolBinPath := *res.LDPath
	auxTools := resolveRunProgramAuxTools(ctx, stmt.ToolPaths)
	inVFSByToken := make(map[string]VFS, len(stmt.INFiles))
	inVFSs := make([]VFS, 0, len(stmt.INFiles))

	for _, f := range stmt.INFiles {
		vfs := runProgramInputVFS(ctx, instance, d, f)
		inVFSByToken[f] = vfs
		inVFSs = append(inVFSs, vfs)
	}

	outVFSByToken := make(map[string]VFS, len(stmt.OUTFiles)+len(stmt.OUTNoAutoFiles)+1)

	for _, f := range stmt.OUTFiles {
		outVFSByToken[f] = copyFileOutputVFS(instance.Path.rel(), f)
	}

	for _, f := range stmt.OUTNoAutoFiles {
		outVFSByToken[f] = copyFileOutputVFS(instance.Path.rel(), f)
	}

	var stdoutVFS *VFS

	if stmt.StdoutFile != nil {
		vfs := copyFileOutputVFS(instance.Path.rel(), *stmt.StdoutFile)
		stdoutVFS = &vfs
		outVFSByToken[*stmt.StdoutFile] = vfs
	}

	// The run's $(S) source inputs are real inputs of any unit that transitively
	// consumes a generated output (directly, or after the output is archived into
	// an .inc that a CC unit #includes). Record them on each output so the archive
	// emit can propagate them as closure leaves (see emitArchive).
	var prSourceInputs []VFS

	for _, v := range inVFSs {
		if v.isSource() {
			prSourceInputs = append(prSourceInputs, v)
		}
	}

	if reg != nil {
		for _, f := range stmt.OUTFiles {
			registerGeneratedParsedOutput(ctx, instance, pkPR, outVFSByToken[f], prEmitsIncludes(ctx, instance, d, f, stmt), []NodeRef{toolLDRef})
			reg.setSourceInputs(outVFSByToken[f], prSourceInputs)
		}

		for _, f := range stmt.OUTNoAutoFiles {
			registerGeneratedParsedOutput(ctx, instance, pkPR, outVFSByToken[f], prEmitsIncludes(ctx, instance, d, f, stmt), []NodeRef{toolLDRef})
			reg.setSourceInputs(outVFSByToken[f], prSourceInputs)
		}

		if stmt.StdoutFile != nil {
			registerGeneratedParsedOutput(ctx, instance, pkPR, *stdoutVFS, prEmitsIncludes(ctx, instance, d, *stmt.StdoutFile, stmt), []NodeRef{toolLDRef})
			reg.setSourceInputs(*stdoutVFS, prSourceInputs)
		}
	}

	inputClosure := prInputClosure(ctx, instance, d, stmt, moduleInputs)

	prExtraDepRefs := resolveCodegenDepRefsExt(ctx, instance, inputClosure, inVFSs, toolLDRef)

	prResult := emitPR(instance, stmt, toolBinPath, toolLDRef, auxTools, inVFSByToken, outVFSByToken, stdoutVFS, inputClosure, prExtraDepRefs, ctx.emit)
	prRef := prResult.Ref

	if d.prOutputInputs == nil {
		d.prOutputInputs = map[string]InputChunks{}
	}

	// prResult.Inputs shares the PR node's chunk list; nothing mutates it after
	// Emit and the reader (prResourceExtraInputs) copies out, so sharing it
	// across keys is safe.
	for _, f := range stmt.OUTFiles {
		d.prOutputInputs[f] = prResult.Inputs
	}

	for _, f := range stmt.OUTNoAutoFiles {
		d.prOutputInputs[f] = prResult.Inputs
	}

	if stmt.StdoutFile != nil {
		d.prOutputInputs[*stmt.StdoutFile] = prResult.Inputs
	}

	if reg != nil {
		for _, f := range stmt.OUTFiles {
			bindGeneratedOutput(ctx, instance, outVFSByToken[f], prRef)
		}

		for _, f := range stmt.OUTNoAutoFiles {
			bindGeneratedOutput(ctx, instance, outVFSByToken[f], prRef)
		}

		if stmt.StdoutFile != nil {
			bindGeneratedOutput(ctx, instance, *stdoutVFS, prRef)
		}
	}

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
		SourceRoot:        ctx.sourceRoot,
		FS:                ctx.fs,
	}

	var out []VFS
	walkOne := func(rel string) {
		buildRootPath := copyFileOutputVFS(instance.Path.rel(), rel)
		sub := walkClosureTail(ctx, instance, buildRootPath, scanIn)
		out = append(out, sub...)
	}
	walkInput := func(rel string) {
		inputVFS := runProgramInputVFS(ctx, instance, d, rel)
		sub := walkClosureTail(ctx, instance, inputVFS, scanIn)
		out = append(out, sub...)
	}

	for _, f := range stmt.OUTFiles {
		if !isCCSourceExt(f) {
			continue
		}

		walkOne(f)
	}

	// OUT_NOAUTO outputs use upstream's `${hide;noauto;output:OUT_NOAUTO}`
	// modifier: registered as outputs but explicitly EXCLUDED from the
	// auto-input/scan chain — the PR node does not walk their closures.
	// (yql/.../v1_proto_split_antlr4 uses OUT_NOAUTO for .pb.h/.pb.cc, and
	// upstream tracks only IN + tools as PR inputs; walking the .pb.cc here
	// over-emits 1253 libcxx/protobuf headers via the parsed pb.h chain.)
	if stmt.StdoutFile != nil && isCCSourceExt(*stmt.StdoutFile) {
		walkOne(*stmt.StdoutFile)
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
		if !includeDirectiveParsers.hasRegisteredParser(f) {
			continue
		}

		walkInput(f)
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
	if reg := codegenRegForInstance(ctx, instance); reg != nil {
		for _, oi := range stmt.OutputIncludes {
			target := oi

			if vfsHasPrefix(target) {
				target = intern(target).rel()
			}

			candidate := build(target)
			info := reg.lookup(candidate)

			if info == nil {
				continue
			}

			sub := walkClosureTail(ctx, instance, info.OutputPath, scanIn)

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

	out = dropTransitiveGeneratedProto(out)

	if len(out) == 0 {
		return nil
	}

	out = dedupVFS(out, nil)

	return out
}

// dropTransitiveGeneratedProto removes a build-generated .proto a RUN_PROGRAM /
// RUN_PYTHON3 codegen node surfaces by walking its own generated .proto INFile
// (the protoc-split step takes the $(B) .proto as input, and walkClosure of that
// .proto returns the .proto itself — the scanner does not expand .proto imports).
// Such a $(B) .proto is a codegen intermediate, not a graph input of the walking
// node: upstream reaches it via the producer dep edge. Keeping it would also make
// resolveCodegenDepRefs attach a spurious dep on its RUN_ANTLR (JV) producer,
// diverging the node's deps (hence self_uid). $(S) proto imports stay.
//
// CC consumers no longer need this: a generated header used to fake-include its
// .proto, dragging the $(B) intermediate into every CC closure; that include is
// gone (the .proto rides as a closure leaf instead), so the only live callers are
// the two codegen-node sites above.
func dropTransitiveGeneratedProto(in []VFS) []VFS {
	out := in[:0]

	for _, v := range in {
		if v.isBuild() && strings.HasSuffix(v.rel(), ".proto") {
			continue
		}

		out = append(out, v)
	}

	return out
}

func prEmitsIncludes(ctx *GenCtx, instance ModuleInstance, d *ModuleData, outFile string, stmt *RunProgramStmt) []IncludeDirective {
	if !generatedOutputCarriesIncludes(outFile) {
		return nil
	}

	includes := make([]IncludeDirective, 0, len(stmt.INFiles)+len(stmt.OutputIncludes))

	for _, f := range stmt.INFiles {
		includes = append(includes, IncludeDirective{kind: includeQuoted, target: internStr(runProgramInputVFS(ctx, instance, d, f).rel())})
	}

	for _, f := range stmt.OutputIncludes {
		if vfsHasPrefix(f) {
			f = intern(f).rel()
		}

		includes = append(includes, IncludeDirective{kind: includeQuoted, target: internStr(f)})
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

	if reg := codegenRegForInstance(ctx, instance); reg != nil {
		if reg.lookup(buildVFS) != nil {
			return buildVFS
		}
	}

	if ctx.fs.isFile(srcRootVFS, rel) {
		return source(rel)
	}

	return resolveModuleSourceVFS(ctx, instance, d, rel, d.srcDirs)
}

func expandRunProgramCWD(instance ModuleInstance, cwd string) string {
	cwd = strings.ReplaceAll(cwd, "$BINDIR", build(instance.Path.rel()).string())
	cwd = strings.ReplaceAll(cwd, "$CURDIR", instance.Path.string())
	cwd = strings.ReplaceAll(cwd, "${ARCADIA_BUILD_ROOT}", "$(B)")
	cwd = strings.ReplaceAll(cwd, "${ARCADIA_ROOT}", "$(S)")

	return cwd
}

type PrEmitResult struct {
	Ref    NodeRef
	Inputs InputChunks
}

func emitPR(
	instance ModuleInstance,
	stmt *RunProgramStmt,
	toolBinPath VFS,
	toolLDRef NodeRef,
	auxTools []RunProgramAuxTool,
	inVFSByToken map[string]VFS,
	outVFSByToken map[string]VFS,
	stdoutVFS *VFS,
	inputClosure []VFS,
	extraDepRefs []NodeRef,
	emit Emitter,
) PrEmitResult {
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
		a = strings.ReplaceAll(a, "${CURDIR}", instance.Path.string())
		a = strings.ReplaceAll(a, "${BINDIR}", build(instance.Path.rel()).string())
		a = strings.ReplaceAll(a, "${MODDIR}", instance.Path.rel())
		a = strings.ReplaceAll(a, "$CURDIR", instance.Path.string())
		a = strings.ReplaceAll(a, "$BINDIR", build(instance.Path.rel()).string())

		for _, tool := range auxTools {
			if strings.Contains(a, tool.token) {
				a = strings.ReplaceAll(a, tool.token, tool.bin.string())
			}
		}

		if vfs, ok := inVFSByToken[a]; ok && !strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
			a = vfs.string()
		} else if vfs, ok := outVFSByToken[a]; ok && !strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
			a = vfs.string()
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
	inputs := InputChunks{head, deduper.filterSeen(inputClosure)}

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
		CmdArgs: ArgChunks{cmdArgs},
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
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel()},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:          depRefs,
		ForeignDepRefs:   foreignDepRefs,
	}

	// The node and the result share the same chunk list: nothing mutates a
	// node's Inputs after Emit, and prOutputInputs readers copy out.
	return PrEmitResult{
		Ref:    emit.emit(node),
		Inputs: inputs,
	}
}
