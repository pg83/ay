package main

import "strings"

func copyFileAutoSourceVFS(modulePath string, d *moduleData, srcRel string) (VFS, bool) {
	if d == nil || d.copyFileAutoOutputs == nil {
		return VFS{}, false
	}

	entry, ok := d.copyFileAutoOutputs[srcRel]
	if !ok {
		return VFS{}, false
	}

	return copyFileOutputVFS(modulePath, entry.Dst), true
}

func copyFileParsedIncludes(modulePath string, entry copyFileEntry) []includeDirective {
	out := make([]includeDirective, 0, len(entry.OutputIncludes)+1)
	if entry.WithContext {
		srcVFS := copyFileInputVFS(modulePath, entry.Src)
		out = append(out, includeDirective{kind: includeQuoted, target: srcVFS.Rel})
	}
	for _, include := range entry.OutputIncludes {
		out = append(out, includeDirective{
			kind:   includeQuoted,
			target: copyFileIncludeTarget(modulePath, include),
		})
	}
	return out
}

func emitCopyFiles(ctx *genCtx, instance ModuleInstance, d *moduleData) {
	for _, entry := range d.copyFiles {
		srcVFS := copyFileInputVFS(instance.Path, entry.Src)
		dstVFS := copyFileOutputVFS(instance.Path, entry.Dst)
		depRefs := resolveCodegenDepRefsExt(ctx, instance, nil, []VFS{srcVFS})
		ref := EmitCPWithDeps(instance, srcVFS, dstVFS, depRefs, ctx.emit)

		parsed := copyFileParsedIncludes(instance.Path, entry)
		registerBoundGeneratedParsedOutput(ctx, instance, "CP", dstVFS, parsed, ref)
	}
}

func resolveModuleSourceVFS(ctx *genCtx, instance ModuleInstance, d *moduleData, srcRel string, srcDir *string) VFS {
	if buildVFS, ok := copyFileAutoSourceVFS(instance.Path, d, srcRel); ok {
		return buildVFS
	}

	return resolveSourceVFS(ctx, instance, srcRel, srcDir)
}

func isSourceEligibleForCopyAuto(srcRel string) bool {
	return isHeaderSource(srcRel) ||
		strings.HasSuffix(srcRel, ".c") ||
		strings.HasSuffix(srcRel, ".cpp") ||
		strings.HasSuffix(srcRel, ".cc") ||
		strings.HasSuffix(srcRel, ".cxx") ||
		strings.HasSuffix(srcRel, ".proto") ||
		strings.HasSuffix(srcRel, ".ev") ||
		strings.HasSuffix(srcRel, ".g4") ||
		strings.HasSuffix(srcRel, ".y") ||
		strings.HasSuffix(srcRel, ".rl") ||
		strings.HasSuffix(srcRel, ".rl6") ||
		strings.HasSuffix(srcRel, ".h.in") ||
		strings.HasSuffix(srcRel, ".c.in") ||
		strings.HasSuffix(srcRel, ".cpp.in")
}
