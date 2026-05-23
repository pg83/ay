package main

import (
	"path/filepath"
	"sort"
	"strings"
)

// runProgramsForARResult carries per-CC (refs, outputs, memberInputs)
// for the caller's AR-member accumulators.
//
// A RUN_PROGRAM with `STDOUT/OUT foo.cpp` emits the .cpp under
// $(B)/<instance.Path>/foo.cpp and a downstream CC compiles it into
// foo.cpp.o for the AR/LD. Mirrors upstream ymake's auto-promote of
// compilable-extension RUN_PROGRAM outputs.
type runProgramsForARResult struct {
	CCRefs       []NodeRef
	CCOutputs    []VFS
	MemberInputs [][]VFS
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
		// Record (output filename → PR NodeRef) so ARCHIVE() in the
		// same module can wire the AR's dep set to the producing PR.
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

		// Classify outputs by extension. CC-compilable outputs
		// trigger a downstream CC; opaque outputs (.pyc etc.) stay
		// as registry-only entries.
		outs := make([]string, 0, len(rp.OUTFiles)+len(rp.OUTNoAutoFiles)+1)
		outs = append(outs, rp.OUTFiles...)
		// OUT_NOAUTO suppresses auto-promote-to-source upstream, so
		// rp.OUTNoAutoFiles is skipped for CC dispatch even when its
		// extension is .cpp/.c/...
		if rp.StdoutFile != nil {
			outs = append(outs, *rp.StdoutFile)
		}

		for _, out := range outs {
			if !isCCSourceExt(out) {
				continue
			}

			ccRef, ccOut, ccIns := emitPRDownstreamCC(ctx, instance, out, prRef, in)
			res.CCRefs = append(res.CCRefs, ccRef)
			res.CCOutputs = append(res.CCOutputs, ccOut)
			res.MemberInputs = append(res.MemberInputs, ccIns)
		}
	}

	return res
}

// emitRunProgram emits a PR node for a RUN_PROGRAM declaration.
// It walks the tool PROGRAM as a host instance to get its LD ref/path.
func emitRunProgram(ctx *genCtx, instance ModuleInstance, stmt *RunProgramStmt, d *moduleData, reg *CodegenRegistry, moduleInputs ModuleCCInputs) NodeRef {
	// Walk the tool as a host program.
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

	// Register PR outputs FIRST so the closure walk below resolves
	// each output's $(B) path through the codegen registry.
	//
	// CC-compilable outputs and generated headers get EmitsIncludes =
	// SOURCE_ROOT-rooted IN + OUTPUT_INCLUDES + tool INDUCED_DEPS, so the
	// include scanner reaches headers the tool injects into every emitted
	// translation unit/header. Opaque outputs (.pyc, binary blobs) still
	// leave EmitsIncludes nil.
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

	// Fold the transitive include closure of each CC-compilable
	// output into THIS PR node's inputs[] — REF's PR carries the full
	// closure (dep_types.h_dumper.cpp PR holds 1500 entries). Driven
	// by the registry: each output's EmitsIncludes is the SOURCE_ROOT
	// IN/OUTPUT_INCLUDES set; scanner follows real `#include`s from
	// there. Non-CC outputs contribute nothing.
	inputClosure := prInputClosure(ctx, instance, stmt, moduleInputs)

	// Resolve codegen-producer refs reached via the PR's inputClosure.
	// PR's deps[] must include any cross-module EN/PB/EV producer
	// whose generated header appears in the PR's transitive input
	// set (dep_types.h_dumper.cpp PR depends on diag's EN
	// stats_enums.h_serialized.cpp via dep_types.h → stats_enums.h).
	prExtraDepRefs := resolveCodegenDepRefsExt(ctx, instance, inputClosure, inVFSs, toolLDRef)

	prResult := EmitPR(instance, stmt, toolBinPath, toolLDRef, auxTools, inVFSByToken, outVFSByToken, stdoutVFS, inputClosure, prExtraDepRefs, ctx.emit)
	prRef := prResult.Ref
	if d.prOutputInputs == nil {
		d.prOutputInputs = map[string][]VFS{}
	}
	for _, f := range stmt.OUTFiles {
		d.prOutputInputs[f] = append([]VFS(nil), prResult.Inputs...)
	}
	for _, f := range stmt.OUTNoAutoFiles {
		d.prOutputInputs[f] = append([]VFS(nil), prResult.Inputs...)
	}
	if stmt.StdoutFile != nil {
		d.prOutputInputs[*stmt.StdoutFile] = append([]VFS(nil), prResult.Inputs...)
	}

	// Backfill PR ProducerRef so the downstream CC's
	// resolveCodegenDepRefs threads PR into deps[]. Registry entries
	// above were created with HasProducerRef=false (ref not yet
	// known); SetProducerRef fills it atomically.
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

// isCCSourceExt reports whether path names a CC-compilable source.
// PR outputs with these extensions become implicit module sources
// (.c/.cpp/.cc/.cxx). .S/.s/.asm are excluded — PR currently produces
// no assembly outputs and the AS path has its own toolchain
// prerequisites (yasm walk).
func isCCSourceExt(p string) bool {
	return strings.HasSuffix(p, ".cpp") ||
		strings.HasSuffix(p, ".cc") ||
		strings.HasSuffix(p, ".cxx") ||
		strings.HasSuffix(p, ".c")
}

func generatedOutputCarriesIncludes(p string) bool {
	return isCCSourceExt(p) || isHeaderSource(p) || strings.HasSuffix(p, ".inc")
}

// prInputClosure returns the union of transitive include closures
// over every CC-compilable PR output (OUT / OUT_NOAUTO / STDOUT).
// Scanner walks registered EmitsIncludes (SOURCE_ROOT IN +
// OUTPUT_INCLUDES) and follows `#include`s from there. Non-CC outputs
// have nil EmitsIncludes and contribute nothing.
func prInputClosure(ctx *genCtx, instance ModuleInstance, stmt *RunProgramStmt, moduleInputs ModuleCCInputs) []VFS {
	// Use the consuming module's full scan-input bag so peer headers
	// reachable from the PR output's EmitsIncludes chain resolve.
	// Mirrors emitEnumSrcs (gen.go).
	scanIn := ModuleCCInputs{
		Flags:             moduleInputs.Flags,
		AddIncl:           moduleInputs.AddIncl,
		PeerAddInclGlobal: moduleInputs.PeerAddInclGlobal,
		SrcDir:            moduleInputs.SrcDir,
		SourceRoot:        ctx.sourceRoot,
		FS:                ctx.fs,
	}

	var out []VFS
	walkOne := func(rel string) {
		buildRootPath := Build(instance.Path + "/" + rel)
		sub := walkClosure(ctx, instance, buildRootPath, scanIn)
		out = append(out, sub...)
	}

	for _, f := range stmt.OUTFiles {
		if !isCCSourceExt(f) {
			continue
		}
		walkOne(f)
	}
	for _, f := range stmt.OUTNoAutoFiles {
		if !isCCSourceExt(f) {
			continue
		}
		walkOne(f)
	}
	if stmt.StdoutFile != nil && isCCSourceExt(*stmt.StdoutFile) {
		walkOne(*stmt.StdoutFile)
	}

	if len(out) == 0 {
		return nil
	}

	out = mergeDedupVFS(out, nil)
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// prEmitsIncludes returns the EmitsIncludes set to register for a PR
// output named `outFile`. Generated C/C++ sources and headers are assumed to
// textually `#include` their IN files, OUTPUT_INCLUDES headers, and the
// tool's module-level INDUCED_DEPS list; opaque outputs return nil.
func prEmitsIncludes(ctx *genCtx, instance ModuleInstance, d *moduleData, outFile string, stmt *RunProgramStmt, toolInducedDeps []string) []includeDirective {
	if !generatedOutputCarriesIncludes(outFile) {
		return nil
	}

	includes := make([]includeDirective, 0, len(stmt.INFiles)+len(stmt.OutputIncludes)+len(toolInducedDeps))

	// IN files are module-relative; rebase to SOURCE_ROOT.
	for _, f := range stmt.INFiles {
		includes = append(includes, includeDirective{kind: includeQuoted, target: runProgramInputVFS(ctx, instance, d, f).Rel})
	}

	// OUTPUT_INCLUDES entries are repo-relative; expandStmtTokens may have
	// expanded ${ARCADIA_ROOT} → $(S)/ — strip the VFS prefix if present.
	for _, f := range stmt.OutputIncludes {
		if v, ok := ParseVFS(f); ok {
			f = v.Rel
		}
		includes = append(includes, includeDirective{kind: includeQuoted, target: f})
	}

	// Tool-declared INDUCED_DEPS (repo-relative).
	for _, f := range toolInducedDeps {
		includes = append(includes, includeDirective{kind: includeQuoted, target: f})
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
		return copyFileInputVFS(instance.Path, rel)
	}

	buildVFS := Build(filepath.ToSlash(filepath.Clean(instance.Path + "/" + rel)))
	if reg := codegenRegForInstance(ctx, instance); reg != nil {
		if _, found := reg.Lookup(buildVFS); found {
			return buildVFS
		}
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
