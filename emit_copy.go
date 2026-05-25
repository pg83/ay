package main

import (
	"path/filepath"
	"strings"
)

func copyFileAutoSourceVFS(modulePath string, d *moduleData, srcRel string) *VFS {
	if d == nil || d.copyFileAutoOutputs == nil {
		return nil
	}

	entry, ok := d.copyFileAutoOutputs[srcRel]
	if !ok {
		return nil
	}

	return vfsPtr(copyFileOutputVFS(modulePath, entry.Dst))
}

func copyFileParsedIncludes(fs *FS, modulePath string, entry copyFileEntry) []includeDirective {
	out := make([]includeDirective, 0, len(entry.OutputIncludes)+1)
	if entry.WithContext {
		srcVFS := copyFileInputVFS(fs, modulePath, entry.Src)
		out = append(out, includeDirective{kind: includeQuoted, target: srcVFS.Rel()})
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
		srcVFS := copyFileInputVFS(ctx.fs, instance.Path, entry.Src)
		dstVFS := copyFileOutputVFS(instance.Path, entry.Dst)
		depRefs := resolveCodegenDepRefsExt(ctx, instance, nil, []VFS{srcVFS})
		ref := EmitCPWithDeps(instance, srcVFS, dstVFS, depRefs, ctx.emit)

		parsed := copyFileParsedIncludes(ctx.fs, instance.Path, entry)
		registerBoundGeneratedParsedOutput(ctx, instance, "CP", dstVFS, parsed, ref)
	}
}

func generatedModuleSourceVFS(ctx *genCtx, instance ModuleInstance, srcRel string) *VFS {
	reg := codegenRegForInstance(ctx, instance)
	if reg == nil {
		return nil
	}

	buildVFS := Build(filepath.ToSlash(filepath.Clean(instance.Path + "/" + srcRel)))
	if _, found := reg.Lookup(buildVFS); found {
		return vfsPtr(buildVFS)
	}

	return nil
}

func resolveModuleSourceVFS(ctx *genCtx, instance ModuleInstance, d *moduleData, srcRel string, srcDir *string) VFS {
	if buildVFS := copyFileAutoSourceVFS(instance.Path, d, srcRel); buildVFS != nil {
		return *buildVFS
	}
	if buildVFS := generatedModuleSourceVFS(ctx, instance, srcRel); buildVFS != nil {
		return *buildVFS
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
