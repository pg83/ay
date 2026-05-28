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
	for _, f := range stmt.OUTFiles {
		d.prOutputInputs[f] = append([]VFS(nil), prResult.Inputs...)
	}
	for _, f := range stmt.OUTNoAutoFiles {
		d.prOutputInputs[f] = append([]VFS(nil), prResult.Inputs...)
	}
	if stmt.StdoutFile != nil {
		d.prOutputInputs[*stmt.StdoutFile] = append([]VFS(nil), prResult.Inputs...)
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

	// Walk closure for every generated output that participates in the
	// include graph — CC sources AND headers (.h). Upstream tracks the
	// OUTPUT_INCLUDES list as the generated header's parsed includes and
	// threads their transitive include closure into the PR node's
	// AUTO_INPUT (e.g. runtime_feature_flags.h's `util/system/types.h,
	// atomic, tuple` pulls in the whole libcxx/glibc closure as inputs).
	// Restricting walkOne to CC sources drops that closure entirely.
	// .inc outputs are excluded: tablegen's PR'd .inc files reference
	// sibling .inc's generated by OTHER PRs in the same module, and those
	// siblings register only when their PR runs; walking .inc at this PR's
	// emit time would surface a transient unresolved-include error.
	carriesPRClosure := func(p string) bool {
		return isCCSourceExt(p) || isHeaderSource(p)
	}
	for _, f := range stmt.OUTFiles {
		if !carriesPRClosure(f) {
			continue
		}
		walkOne(f)
	}
	for _, f := range stmt.OUTNoAutoFiles {
		if !carriesPRClosure(f) {
			continue
		}
		walkOne(f)
	}
	if stmt.StdoutFile != nil && carriesPRClosure(*stmt.StdoutFile) {
		walkOne(*stmt.StdoutFile)
	}
	for _, f := range stmt.INFiles {
		if !generatedOutputCarriesIncludes(f) {
			continue
		}
		walkInput(f)
	}

	if len(out) == 0 {
		return nil
	}

	out = mergeDedupVFS(out, nil)
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

	if ctx.fs.IsFile(rel) {
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
