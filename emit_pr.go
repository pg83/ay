package main

import (
	"path/filepath"
	"strings"
)

type runProgramsForARResult struct {
	CCRefs    []NodeRef
	CCOutputs []VFS
}

type runProgramAuxTool struct {
	token string
	ref   NodeRef
	bin   VFS
}

func emitRunProgramsForAR(ctx *genCtx, instance ModuleInstance, d *moduleData, in ModuleCCInputs) *runProgramsForARResult {
	if len(d.runPrograms) == 0 {
		return nil
	}

	reg := codegenRegForInstance(ctx, instance)
	res := &runProgramsForARResult{}

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

func emitRunProgram(ctx *genCtx, instance ModuleInstance, stmt *RunProgramStmt, d *moduleData, reg *CodegenRegistry, moduleInputs ModuleCCInputs) NodeRef {
	res := ctx.toolResult(filepath.Clean(stmt.ToolPath))
	toolLDRef := res.LDRef
	toolBinPath := *res.LDPath
	toolInducedDeps := res.InducedDeps
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
			registerGeneratedParsedOutput(ctx, instance, "PR", outVFSByToken[f], prEmitsIncludes(ctx, instance, d, f, stmt, toolInducedDeps))
		}

		for _, f := range stmt.OUTNoAutoFiles {
			registerGeneratedParsedOutput(ctx, instance, "PR", outVFSByToken[f], prEmitsIncludes(ctx, instance, d, f, stmt, toolInducedDeps))
		}

		if stmt.StdoutFile != nil {
			registerGeneratedParsedOutput(ctx, instance, "PR", *stdoutVFS, prEmitsIncludes(ctx, instance, d, *stmt.StdoutFile, stmt, toolInducedDeps))
		}
	}

	inputClosure := prInputClosure(ctx, instance, d, stmt, moduleInputs)

	prExtraDepRefs := resolveCodegenDepRefsExt(ctx, instance, inputClosure, inVFSs, toolLDRef)

	prResult := EmitPR(instance, stmt, toolBinPath, toolLDRef, auxTools, inVFSByToken, outVFSByToken, stdoutVFS, inputClosure, prExtraDepRefs, ctx.emit)
	prRef := prResult.Ref

	if d.prOutputInputs == nil {
		d.prOutputInputs = map[string][]VFS{}
	}

	// prResult.Inputs is a fresh, never-mutated slice; the reader
	// (prResourceExtraInputs) copies out, so sharing it across keys is safe.
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

func prInputClosure(ctx *genCtx, instance ModuleInstance, d *moduleData, stmt *RunProgramStmt, moduleInputs ModuleCCInputs) []VFS {
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
		sub := walkClosure(ctx, instance, buildRootPath, scanIn)
		out = append(out, sub...)
	}
	walkInput := func(rel string) {
		inputVFS := runProgramInputVFS(ctx, instance, d, rel)
		sub := walkClosure(ctx, instance, inputVFS, scanIn)
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
				target = Intern(target).Rel()
			}

			candidate := Build(target)
			info := reg.Lookup(candidate)

			if info == nil {
				continue
			}

			sub := walkClosure(ctx, instance, info.OutputPath, scanIn)

			for _, v := range sub {
				if !strings.HasSuffix(v.Rel(), ".proto") {
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
				if v.IsSource() {
					sibling := strings.TrimSuffix(v.Rel(), ".proto") + ".pb.h"
					sibDir, sibBase := splitDirName(sibling)

					if ctx.fs.IsFile(dirKey(sibDir), sibBase) {
						out = append(out, Source(sibling))
					}
				}
			}
		}
	}

	out = dropTransitiveGeneratedProto(out)

	if len(out) == 0 {
		return nil
	}

	out = ctx.deduper.dedupVFS(out, nil)
	return out
}

// dropTransitiveGeneratedProto removes build-generated .proto files reached
// transitively in a RUN_PROGRAM/RUN_PYTHON3 closure (e.g. via a .pb.h/.pb.cc
// whose protoc step lists the .proto as its IN). Such a .proto is a codegen
// intermediate, not a real input of the walking node: upstream reaches it via
// the producer dep edge and lists only the generator's $(S) sources — which the
// closure already gathered by walking *through* the .proto. Keeping the $(B)
// .proto would also make resolveCodegenDepRefs attach a spurious dep on its
// RUN_ANTLR (JV) producer, diverging the node's deps (hence self_uid). A .proto
// that is the node's own direct IN enters via inVFSs, not the closure, so it
// survives. $(S) proto imports (the proto-import graph) are kept.
// dropTransitiveGeneratedCPP removes build-generated C/C++ source files from a
// CC include-input closure. Build-generated .cpp/.cc/.c files (from ANTLR, proto
// codegen, etc.) that appear transitively in a DIFFERENT module's CC closure are
// not real inputs of that compilation — they're compiled by their own producer
// module. Upstream does not list them as CC inputs when they come through via a
// generated header's parsedIncludes chain. The primary source file (srcVFS) is
// excluded from filtering via the caller not including it in includeInputs.
func dropTransitiveGeneratedCPP(in []VFS) []VFS {
	out := in[:0]

	for _, v := range in {
		if v.IsBuild() && isCCSourceRel(v.Rel()) {
			continue
		}

		out = append(out, v)
	}

	return out
}

func isCCSourceRel(rel string) bool {
	return strings.HasSuffix(rel, ".cpp") ||
		strings.HasSuffix(rel, ".cc") ||
		strings.HasSuffix(rel, ".cxx") ||
		strings.HasSuffix(rel, ".c")
}

func dropTransitiveGeneratedProto(in []VFS) []VFS {
	out := in[:0]

	for _, v := range in {
		if v.IsBuild() && strings.HasSuffix(v.Rel(), ".proto") {
			continue
		}

		out = append(out, v)
	}

	return out
}

func prEmitsIncludes(ctx *genCtx, instance ModuleInstance, d *moduleData, outFile string, stmt *RunProgramStmt, toolInducedDeps []string) []includeDirective {
	if !generatedOutputCarriesIncludes(outFile) {
		return nil
	}

	includes := make([]includeDirective, 0, len(stmt.INFiles)+len(stmt.OutputIncludes)+len(toolInducedDeps))

	for _, f := range stmt.INFiles {
		includes = append(includes, includeDirective{kind: includeQuoted, target: internString(runProgramInputVFS(ctx, instance, d, f).Rel())})
	}

	for _, f := range stmt.OutputIncludes {
		if vfsHasPrefix(f) {
			f = Intern(f).Rel()
		}

		includes = append(includes, includeDirective{kind: includeQuoted, target: internString(f)})
	}

	for _, f := range toolInducedDeps {
		includes = append(includes, includeDirective{kind: includeQuoted, target: internString(f)})
	}

	return includes
}

func resolveRunProgramAuxTools(ctx *genCtx, toolPaths []string) []runProgramAuxTool {
	if len(toolPaths) == 0 {
		return nil
	}

	out := make([]runProgramAuxTool, 0, len(toolPaths))
	seen := make(map[string]struct{}, len(toolPaths))

	for _, toolPath := range toolPaths {
		if _, dup := seen[toolPath]; dup {
			continue
		}

		seen[toolPath] = struct{}{}
		res := ctx.toolResult(filepath.Clean(toolPath))
		out = append(out, runProgramAuxTool{
			token: toolPath,
			ref:   res.LDRef,
			bin:   *res.LDPath,
		})
	}

	return out
}

func runProgramInputVFS(ctx *genCtx, instance ModuleInstance, d *moduleData, rel string) VFS {
	switch {
	case strings.HasPrefix(rel, "$(S)/"),
		strings.HasPrefix(rel, "$(B)/"),
		strings.HasPrefix(rel, "${ARCADIA_ROOT}/"),
		strings.HasPrefix(rel, "${CURDIR}/"),
		strings.HasPrefix(rel, "${ARCADIA_BUILD_ROOT}/"),
		strings.HasPrefix(rel, "${BINDIR}/"):
		return copyFileInputVFS(ctx.fs, instance.Path, rel)
	}

	buildVFS := Build(filepath.ToSlash(filepath.Clean(instance.Path + "/" + rel)))

	if reg := codegenRegForInstance(ctx, instance); reg != nil {
		if reg.Lookup(buildVFS) != nil {
			return buildVFS
		}
	}

	if ctx.fs.IsFile(srcRootVFS, rel) {
		return Source(rel)
	}

	return resolveModuleSourceVFS(ctx, instance, d, rel, d.srcDir)
}

func expandRunProgramCWD(instance ModuleInstance, cwd string) string {
	cwd = strings.ReplaceAll(cwd, "$BINDIR", Build(instance.Path).String())
	cwd = strings.ReplaceAll(cwd, "$CURDIR", Source(instance.Path).String())
	cwd = strings.ReplaceAll(cwd, "${ARCADIA_BUILD_ROOT}", "$(B)")
	cwd = strings.ReplaceAll(cwd, "${ARCADIA_ROOT}", "$(S)")

	return cwd
}
