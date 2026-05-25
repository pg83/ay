package main

import (
	"path/filepath"
	"strings"
)

func copyFileAutoSourceVFS(modulePath string, d *moduleData, srcRel string) (VFS, bool) {
	if d == nil || d.copyFileAutoOutputs == nil {
		return vfsNone, false
	}

	entry, ok := d.copyFileAutoOutputs[srcRel]
	if !ok {
		return vfsNone, false
	}

	return copyFileOutputVFS(modulePath, entry.Dst), true
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

func generatedModuleSourceVFS(ctx *genCtx, instance ModuleInstance, srcRel string) (VFS, bool) {
	reg := codegenRegForInstance(ctx, instance)
	if reg == nil {
		return vfsNone, false
	}

	buildVFS := Build(filepath.ToSlash(filepath.Clean(instance.Path + "/" + srcRel)))
	if _, found := reg.Lookup(buildVFS); found {
		return buildVFS, true
	}

	return vfsNone, false
}

func resolveModuleSourceVFS(ctx *genCtx, instance ModuleInstance, d *moduleData, srcRel string, srcDir *string) VFS {
	if buildVFS, ok := copyFileAutoSourceVFS(instance.Path, d, srcRel); ok {
		return buildVFS
	}
	if buildVFS, ok := generatedModuleSourceVFS(ctx, instance, srcRel); ok {
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
