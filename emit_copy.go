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

type CopyEmitState struct {
	srcVFS         VFS
	dstVFS         VFS
	ref            NodeRef
	producerSource []VFS
}

func (e *EmitContext) emitCopyFileStmt(entry CopyFileEntry) {
	st := e.registerCopyFile(entry)

	e.deferPass2(func() {
		e.emitCopyFileNode(entry, st)
	})
}

func (e *EmitContext) registerCopyFile(entry CopyFileEntry) CopyEmitState {
	ctx, instance := e.ctx, e.instance
	scanner := e.scanner
	reg := e.codegen
	srcVFS := copyFileInputVFS(ctx.fs, instance.Path, entry.Src)
	dstVFS := copyFileOutputVFS(instance.Path.rel(), entry.Dst)

	if srcVFS != dstVFS {
		e.requireProducedInput("COPY_FILE src", entry.Src, srcVFS)
	}

	parsed := copyFileParsedIncludes(scanner, ctx.fs, instance.Path, entry)
	ref := ctx.emit.reserve()

	var producerSource []VFS

	if srcInfo := reg.lookup(srcVFS); srcInfo != nil {
		producerSource = srcInfo.SourceInputs
	}

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
			info.SourceInputs = concat(producerSource, ctx.scripts[copyFsToolsVFS])
		}

		reg.register(info)
	}

	return CopyEmitState{srcVFS: srcVFS, dstVFS: dstVFS, ref: ref, producerSource: producerSource}
}

func (e *EmitContext) emitCopyFileNode(entry CopyFileEntry, st CopyEmitState) {
	ctx, instance, d := e.ctx, e.instance, e.d
	scanner := e.scanner
	deps := resolveCodegenDepRefsIncl(ctx, instance, ctx.na, []VFS{st.srcVFS})

	var closure []VFS

	if entry.WithContext || len(entry.OutputIncludes) > 0 {
		closure = rewriteClosureCPSource(scanner, walkClosure(e.scanner, st.dstVFS, d.cc.ScanCfg))
		closure = keepOnlySourceVFS(closure)
		closure = dedup(closure)
	}

	if len(st.producerSource) > 0 {
		closure = append(closure, st.producerSource...)
	}

	emitCPWithDeps(instance, st.srcVFS, st.dstVFS, deps, closure, st.ref, d.cc.ModuleTag, d.tc, ctx.scripts, ctx.emit)

	if extIsArchiveMember(entry.Dst) {
		e.collectObj(st.ref, st.dstVFS, SrcMeta{Prio: stmtPrioDefault})
	}
}

func (e *EmitContext) generatedModuleSourceVFS(srcRel string) *VFS {
	_, instance := e.ctx, e.instance
	reg := e.codegen

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

func (e *EmitContext) resolveModuleSourceVFS(src STR, srcDirs []VFS) VFS {
	ctx, instance, d := e.ctx, e.instance, e.d

	if buildVFS := copyFileAutoSourceVFS(instance.Path.rel(), d, src); buildVFS != nil {
		return *buildVFS
	}

	srcRel := src.string()

	if buildVFS := e.generatedModuleSourceVFS(srcRel); buildVFS != nil {
		return *buildVFS
	}

	return resolveSourceVFS(ctx, instance, srcRel, srcDirs)
}
