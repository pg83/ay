package main

import (
	"path/filepath"
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

func copyFileParsedIncludes(scanner *IncludeScanner, fs FS, moduleDir VFS, entry CopyFileEntry) []IncludeDirective {
	out := make([]IncludeDirective, 0, len(entry.OutputIncludes)+1)

	if entry.Text {
		srcVFS := copyFileInputVFS(fs, moduleDir, entry.Src)

		out = append(out, scanner.parsedIncludes(srcVFS, nil)...)
	} else if entry.WithContext {
		srcVFS := copyFileInputVFS(fs, moduleDir, entry.Src)

		out = append(out, IncludeDirective{kind: includeQuoted, target: internStr(srcVFS.rel())})
	}

	for _, include := range entry.OutputIncludes {
		out = append(out, IncludeDirective{
			kind:   includeQuoted,
			target: internStr(copyFileIncludeTarget(moduleDir.rel(), include)),
		})
	}

	return out
}

func emitCopyFiles(ctx *GenCtx, instance ModuleInstance, d *ModuleData) (memberRefs []NodeRef, memberOuts []VFS, memberSrcs []VFS) {
	scanner := ctx.scannerFor(instance)
	reg := ctx.codegenFor(instance)

	type entryReg struct {
		srcVFS         VFS
		dstVFS         VFS
		parsed         []IncludeDirective
		ref            NodeRef
		producerSource []VFS
	}

	entries := make([]entryReg, 0, len(d.copyFiles))

	for _, entry := range d.copyFiles {
		srcVFS := copyFileInputVFS(ctx.fs, instance.Path, entry.Src)
		dstVFS := copyFileOutputVFS(instance.Path.rel(), entry.Dst)
		parsed := copyFileParsedIncludes(scanner, ctx.fs, instance.Path, entry)
		ref := ctx.emit.reserve()

		var producerSource []VFS

		if srcInfo := reg.lookup(srcVFS); srcInfo != nil {
			producerSource = srcInfo.SourceInputs
		}

		entries = append(entries, entryReg{srcVFS, dstVFS, parsed, ref, producerSource})

		if existing := reg.lookup(dstVFS); existing != nil {
			existing.ParsedIncludes = parsed
		} else {
			info := &GeneratedFileInfo{
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
					info.SourceInputs = append([]VFS{srcVFS}, ctx.scripts[copyFsToolsVFS]...)
				}
			}

			if len(producerSource) > 0 {
				dstClosure := make([]VFS, 0, len(producerSource)+len(ctx.scripts[copyFsToolsVFS]))

				dstClosure = append(dstClosure, producerSource...)
				dstClosure = append(dstClosure, ctx.scripts[copyFsToolsVFS]...)

				info.SourceInputs = dstClosure
			}

			reg.register(info)
		}
	}

	for i, entry := range d.copyFiles {
		srcVFS := entries[i].srcVFS
		dstVFS := entries[i].dstVFS
		deps := resolveCodegenDepRefsIncl(ctx, instance, ctx.na, []VFS{srcVFS})

		var closure []VFS

		if entry.WithContext || len(entry.OutputIncludes) > 0 {
			closure = walkClosure(ctx.scannerFor(instance), dstVFS, d.cc.ScanCfg)
			closure = rewriteClosureCPSource(scanner, closure)
			closure = keepOnlySourceVFS(closure)
			closure = dedup(closure)
		}

		if len(entries[i].producerSource) > 0 {
			closure = append(closure, entries[i].producerSource...)
		}

		emitCPWithDeps(instance, srcVFS, dstVFS, deps, closure, entries[i].ref, d.cc.ModuleTag, d.tc, ctx.scripts, ctx.emit)

		if dst := entry.Dst; extIsArchiveMember(dst) {
			memberRefs = append(memberRefs, entries[i].ref)
			memberOuts = append(memberOuts, dstVFS)
			memberSrcs = append(memberSrcs, srcVFS)
		}
	}

	return memberRefs, memberOuts, memberSrcs
}

func generatedModuleSourceVFS(ctx *GenCtx, instance ModuleInstance, srcRel string) *VFS {
	reg := ctx.codegenFor(instance)

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
