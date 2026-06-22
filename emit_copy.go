package main

import (
	"path/filepath"
	"strings"
)

func copyFileAutoSourceVFS(modulePath string, d *ModuleData, srcRel string) *VFS {
	if d.copyFileAutoOutputs == nil {
		return nil
	}

	entry, ok := d.copyFileAutoOutputs[internStr(srcRel)]

	if !ok {
		return nil
	}

	return vfsPtr(copyFileOutputVFS(modulePath, entry.Dst))
}

func copyFileParsedIncludes(scanner *IncludeScanner, fs FS, modulePath string, entry CopyFileEntry) []IncludeDirective {
	out := make([]IncludeDirective, 0, len(entry.OutputIncludes)+1)

	if entry.Text {
		// COPY_FILE(TEXT) substitutes the source's content into the dst. The dst
		// must carry the source's own raw #include directives so they resolve in
		// THIS module's context — the per-module dst is the unit of resolution.
		// Pointing the dst at the shared $(S) source node would resolve it once
		// and leak one module's <angle> includes to every consumer.
		srcVFS := copyFileInputVFS(fs, modulePath, entry.Src)
		out = append(out, scanner.parsers.parsedIncludes(srcVFS, nil)...)
	} else if entry.WithContext {
		// Non-TEXT COPY(WITH_CONTEXT …) quoted includes must resolve relative to
		// the SOURCE dir — pointing at the source node preserves that.
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

// emitCopyFiles emits the module's COPY_FILE nodes and returns the (ref, output)
// of any copy whose destination is a linkable object (.a/.o), archived into the
// module's library.
func emitCopyFiles(ctx *GenCtx, instance ModuleInstance, d *ModuleData, moduleInputs *ModuleCCInputs) (memberRefs []NodeRef, memberOuts []VFS, memberSrcs []VFS) {
	scanner := ctx.scannerFor(instance)
	reg := codegenRegForInstance(ctx, instance)

	// Pre-pass: register every COPY entry's parsed includes and codegen mapping
	// (dst → src) before any closure walk runs. COPY entries reference each other
	// through OUTPUT_INCLUDES, so a closure may transit through a sibling defined
	// later in the file.
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
		// Reserve the CP node's ref up front so every dst carries a valid
		// ProducerRef before any CP is built.
		ref := ctx.emit.reserve()

		// When the COPY source is itself a registered build-generated output, its
		// producer's transitive $(S) closure rides on both the CP action and the
		// copied file. An ordinary source-to-build copy resolves srcInfo==nil.
		var producerSource []VFS

		if srcInfo := reg.lookup(srcVFS); srcInfo != nil {
			producerSource = srcInfo.ProducerSourceClosure
		}

		entries = append(entries, entryReg{srcVFS, dstVFS, parsed, ref, producerSource})

		scanner.parsers.registerBuildParsedIncludes(dstVFS, parsed)

		if reg.lookup(dstVFS) == nil {
			info := &GeneratedFileInfo{
				ProducerKvP: pkCP,
				OutputPath:  dstVFS,
				SourcePath:  srcVFS,
				IsText:      entry.Text,
				ProducerRef: ref,
			}

			// The copy tooling is a real input of every copy product, so ride
			// it as a closure leaf of the dst — reaching every consumer
			// transitively instead of being re-attached per source. TEXT and
			// AUTO copies additionally materialize the $(S) source, so add it.
			if srcVFS != dstVFS {
				info.ClosureLeaves = append(info.ClosureLeaves, ctx.scripts[copyFsToolsVFS]...)

				if entry.Text || entry.Auto {
					info.ClosureLeaves = append(info.ClosureLeaves, srcVFS)
				}

				// A source-root packaging-stage copy carries its own $(S) input
				// set: the original source plus the copy tooling, listed on
				// every consumer. A $(B) generated source has no $(S) closure
				// to lift here.
				if srcVFS.isSource() {
					info.ProducerSourceClosure = append([]VFS{srcVFS}, ctx.scripts[copyFsToolsVFS]...)
				}
			}

			// Carry a copied generated output's producer source closure onto
			// the dst so a downstream PY_SRCS bytecode node folds the full
			// transitive source set: the CP's own copy-tool scripts plus the
			// $(B) source's producer closure.
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
		// Exclude this CP's own reserved ref: an AUTO copy may have src == dst,
		// so without the exclude srcVFS would resolve to this node — a
		// self-dependency (and finalize cycle).
		deps := resolveCodegenDepRefs(ctx, instance, []VFS{srcVFS}, entries[i].ref)

		// First-DFS-leave ownership: when a COPY_FILE consumes another module's
		// OUT_NOAUTO build output whose own module never re-references it, the
		// consuming COPY is the first leaver, so the producer's module_dir and
		// late ${BINDIR} cwd follow this module. Record the consumer first-claim
		// (first-wins; a no-op when the producer already owns the output).
		if len(deps) > 0 && srcVFS.isBuild() && srcVFS != dstVFS {
			var ownerTag STR

			if d.moduleStmt != nil {
				ownerTag = moduleCCTag(d.moduleStmt.Name)
			}

			scanner.recordFirstClaim(srcVFS, instance.Path.rel(), ownerTag)
		}

		var closure []VFS

		// WITH_CONTEXT pulls the source file's #include closure; OUTPUT_INCLUDES
		// each declared target's. Both fall out of a single walk from dst.
		// rewriteClosureCPSource swaps sibling-CP $(B) hits for their $(S) sources;
		// keepOnlySourceVFS drops the rest (CP closures are source-only).
		if moduleInputs != nil && (entry.WithContext || len(entry.OutputIncludes) > 0) {
			// Closure root is irrelevant: rewriteClosureCPSource maps the dst
			// root to its SourcePath and EmitCPWithDeps filters src and dst from
			// extraInputs, so the rootless walk is exact.
			closure = walkClosure(ctx.scannerFor(instance), dstVFS, moduleInputs.ScanCfg)
			closure = rewriteClosureCPSource(scanner, closure)
			closure = keepOnlySourceVFS(closure)
			closure = dedupVFS(closure)
		}

		// Copying a registered generated output lists its producer's transitive
		// $(S) closure on the CP action too.
		if len(entries[i].producerSource) > 0 {
			closure = append(closure, entries[i].producerSource...)
		}

		// Every node carries the owning submodule's MODULE_TAG. ModuleTag is the
		// already-computed moduleCCTag — 0 for an ordinary LIBRARY, so plain
		// copies stay untagged.
		var moduleTag STR

		if moduleInputs != nil {
			moduleTag = moduleInputs.ModuleTag
		}

		emitCPWithDeps(instance, srcVFS, dstVFS, deps, closure, entries[i].ref, moduleTag, d.tc, ctx.scripts, ctx.emit)

		if dst := entry.Dst; strings.HasSuffix(dst, ".a") || strings.HasSuffix(dst, ".o") {
			memberRefs = append(memberRefs, entries[i].ref)
			memberOuts = append(memberOuts, dstVFS)
			// A copied .a/.o is archived alongside its copy source: the prebuilt
			// source rides as a direct input of the module's AR.
			memberSrcs = append(memberSrcs, srcVFS)
		}
	}

	return memberRefs, memberOuts, memberSrcs
}

func generatedModuleSourceVFS(ctx *GenCtx, instance ModuleInstance, srcRel string) *VFS {
	reg := codegenRegForInstance(ctx, instance)

	// Lookup-only probe: the canonical "$(B)/<mod>/<src>" is assembled and probed
	// without inserting — a plain src otherwise interns a never-registered $(B)
	// path per call.
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

func resolveModuleSourceVFS(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcRel string, srcDirs []VFS) VFS {
	if buildVFS := copyFileAutoSourceVFS(instance.Path.rel(), d, srcRel); buildVFS != nil {
		return *buildVFS
	}

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
