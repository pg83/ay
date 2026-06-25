package main

import (
	"path/filepath"
	"strings"
)

func copyFileAutoSourceVFS(modulePath string, d *ModuleData, src STR) *VFS {
	if d.copyFileAutoOutputs == nil {
		return nil
	}

	entry, ok := d.copyFileAutoOutputs[src]

	if !ok {
		return nil
	}

	return vfsPtr(copyFileOutputVFS(modulePath, entry.Dst))
}

func copyFileParsedIncludes(scanner *IncludeScanner, fs FS, modulePath string, entry CopyFileEntry) []IncludeDirective {
	out := make([]IncludeDirective, 0, len(entry.OutputIncludes)+1)

	if entry.Text {
		srcVFS := copyFileInputVFS(fs, modulePath, entry.Src)
		out = append(out, scanner.parsedIncludes(srcVFS, nil)...)
	} else if entry.WithContext {
		srcVFS := copyFileInputVFS(fs, modulePath, entry.Src)
		out = append(out, IncludeDirective{kind: includeQuoted, target: internStr(srcVFS.rel())})
	}

	for _, include := range entry.OutputIncludes {
		out = append(out, IncludeDirective{
			kind:   includeQuoted,
			target: internStr(copyFileIncludeTarget(modulePath, include)),
		})
	}

	return out
}

func emitCopyFiles(ctx *GenCtx, instance ModuleInstance, d *ModuleData, moduleInputs *ModuleCCInputs) (memberRefs []NodeRef, memberOuts []VFS, memberSrcs []VFS) {
	scanner := ctx.scannerFor(instance)
	reg := codegenRegForInstance(ctx, instance)

	type entryReg struct {
		srcVFS         VFS
		dstVFS         VFS
		parsed         []IncludeDirective
		ref            NodeRef
		producerSource []VFS
	}
	entries := make([]entryReg, 0, len(d.copyFiles))

	for _, entry := range d.copyFiles {
		srcVFS := copyFileInputVFS(ctx.fs, instance.Path.rel(), entry.Src)
		dstVFS := copyFileOutputVFS(instance.Path.rel(), entry.Dst)
		parsed := copyFileParsedIncludes(scanner, ctx.fs, instance.Path.rel(), entry)

		ref := ctx.emit.reserve()

		var producerSource []VFS

		if srcInfo := reg.lookup(srcVFS); srcInfo != nil {
			producerSource = srcInfo.ProducerSourceClosure
		}

		entries = append(entries, entryReg{srcVFS, dstVFS, parsed, ref, producerSource})

		if existing := reg.lookup(dstVFS); existing != nil {
			existing.ParsedIncludes = parsed
		} else {
			info := &GeneratedFileInfo{
				ProducerKvP:    pkCP,
				OutputPath:     dstVFS,
				SourcePath:     srcVFS,
				IsText:         entry.Text,
				ProducerRef:    ref,
				ParsedIncludes: parsed,
			}

			if srcVFS != dstVFS {
				info.ClosureLeaves = append(info.ClosureLeaves, ctx.scripts[copyFsToolsVFS]...)

				if entry.Text || entry.Auto {
					info.ClosureLeaves = append(info.ClosureLeaves, srcVFS)
				}

				if srcVFS.isSource() {
					info.ProducerSourceClosure = append([]VFS{srcVFS}, ctx.scripts[copyFsToolsVFS]...)
				}
			}

			if len(producerSource) > 0 {
				dstClosure := make([]VFS, 0, len(producerSource)+len(ctx.scripts[copyFsToolsVFS]))
				dstClosure = append(dstClosure, producerSource...)
				dstClosure = append(dstClosure, ctx.scripts[copyFsToolsVFS]...)
				info.ProducerSourceClosure = dstClosure
			}

			reg.register(info)
		}
	}

	for i, entry := range d.copyFiles {
		srcVFS := entries[i].srcVFS
		dstVFS := entries[i].dstVFS

		deps := resolveCodegenDepRefsIncl(ctx, instance, ctx.na, []VFS{srcVFS})

		var closure []VFS

		if moduleInputs != nil && (entry.WithContext || len(entry.OutputIncludes) > 0) {
			closure = walkClosure(ctx.scannerFor(instance), dstVFS, moduleInputs.ScanCfg)
			closure = rewriteClosureCPSource(scanner, closure)
			closure = keepOnlySourceVFS(closure)
			closure = dedupVFS(closure)
		}

		if len(entries[i].producerSource) > 0 {
			closure = append(closure, entries[i].producerSource...)
		}

		var moduleTag STR

		if moduleInputs != nil {
			moduleTag = moduleInputs.ModuleTag
		}

		emitCPWithDeps(instance, srcVFS, dstVFS, deps, closure, entries[i].ref, moduleTag, d.tc, ctx.scripts, ctx.emit)

		if dst := entry.Dst; strings.HasSuffix(dst, ".a") || strings.HasSuffix(dst, ".o") {
			memberRefs = append(memberRefs, entries[i].ref)
			memberOuts = append(memberOuts, dstVFS)

			memberSrcs = append(memberSrcs, srcVFS)
		}
	}

	return memberRefs, memberOuts, memberSrcs
}

func generatedModuleSourceVFS(ctx *GenCtx, instance ModuleInstance, srcRel string) *VFS {
	reg := codegenRegForInstance(ctx, instance)

	var id STR

	if srcRel != "" && pathIsClean(srcRel) {
		id = internedPrefixedJoined("$(B)/", instance.Path.rel(), srcRel)
	} else {
		id = internedPrefixed("$(B)/", filepath.ToSlash(filepath.Clean(instance.Path.rel()+"/"+srcRel)))
	}

	if id == 0 {
		return nil
	}

	buildVFS := id.vfs()

	if reg.lookup(buildVFS) != nil {
		return vfsPtr(buildVFS)
	}

	return nil
}

func resolveModuleSourceVFS(ctx *GenCtx, instance ModuleInstance, d *ModuleData, src STR, srcDirs []VFS) VFS {
	if buildVFS := copyFileAutoSourceVFS(instance.Path.rel(), d, src); buildVFS != nil {
		return *buildVFS
	}

	srcRel := src.string()

	if buildVFS := generatedModuleSourceVFS(ctx, instance, srcRel); buildVFS != nil {
		return *buildVFS
	}

	return resolveSourceVFS(ctx, instance, srcRel, srcDirs)
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
		strings.HasSuffix(srcRel, ".ypp") ||
		strings.HasSuffix(srcRel, ".rl") ||
		strings.HasSuffix(srcRel, ".rl6") ||
		strings.HasSuffix(srcRel, ".h.in") ||
		strings.HasSuffix(srcRel, ".c.in") ||
		strings.HasSuffix(srcRel, ".cpp.in")
}
