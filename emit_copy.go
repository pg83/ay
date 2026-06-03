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

func copyFileParsedIncludes(scanner *IncludeScanner, fs FS, modulePath string, entry copyFileEntry) []includeDirective {
	out := make([]includeDirective, 0, len(entry.OutputIncludes)+1)

	if entry.Text {
		// COPY_FILE(TEXT) substitutes the source's content into the dst and is
		// used for shared codegen templates (e.g. the minikql llvm16 *.h.txt
		// headers) that several sibling modules each copy into their own
		// staging. The dst must carry the source's *own raw #include directives*
		// so they resolve in THIS module's context — the per-module dst is the
		// unit of resolution (its absID is unique per module). Pointing the dst
		// at the shared $(S) source node instead would resolve it exactly once
		// (cached by absID in IncludeScanner.childrenCache): the first module to
		// reach the shared template fixed every consumer's <angle> includes to
		// that first module's staging copies — a cross-module include leak. The
		// source file rides as a non-expanded closure leaf of the dst instead
		// (registered in emitCopyFiles; see CodegenRegistry.closureLeaves).
		srcVFS := copyFileInputVFS(fs, modulePath, entry.Src)

		if scanner != nil {
			out = append(out, scanner.parsers.parsedIncludes(srcVFS)...)
		}
	} else if entry.WithContext {
		// Non-TEXT COPY(WITH_CONTEXT …) (e.g. a .cpp plus its sibling .h, copied
		// by a single module) cannot leak across modules, and its quoted
		// includes must resolve relative to the SOURCE dir — pointing at the
		// source node preserves that (e.g. a .cpp's `#include "foo.h"` resolves
		// to the $(S) sibling, not the flat $(B) staging copy).
		srcVFS := copyFileInputVFS(fs, modulePath, entry.Src)
		out = append(out, includeDirective{kind: includeQuoted, target: internString(srcVFS.Rel())})
	}

	for _, include := range entry.OutputIncludes {
		out = append(out, includeDirective{
			kind:   includeQuoted,
			target: internString(copyFileIncludeTarget(modulePath, include)),
		})
	}

	return out
}

func emitCopyFiles(ctx *genCtx, instance ModuleInstance, d *moduleData, moduleInputs *ModuleCCInputs) {
	scanner := ctx.scannerFor(instance)
	reg := codegenRegForInstance(ctx, instance)

	// Pre-pass: register every COPY entry's parsed includes AND its codegen
	// mapping (dst → src) before any closure walk runs. COPY entries reference
	// each other through OUTPUT_INCLUDES (e.g. mkql_builtins_impl.h has
	// mkql_builtins.h in OUTPUT_INCLUDES, mkql_builtins_decimal.h has
	// mkql_builtins_impl.h, and the impl.h-closure must transit through
	// decimal.h via header back-references). If we registered only when a
	// COPY entry is reached, an entry's closure walk would silently miss
	// any sibling defined later in the file.
	type entryReg struct {
		srcVFS VFS
		dstVFS VFS
		parsed []includeDirective
	}
	entries := make([]entryReg, 0, len(d.copyFiles))

	for _, entry := range d.copyFiles {
		srcVFS := copyFileInputVFS(ctx.fs, instance.Path, entry.Src)
		dstVFS := copyFileOutputVFS(instance.Path, entry.Dst)
		parsed := copyFileParsedIncludes(scanner, ctx.fs, instance.Path, entry)
		entries = append(entries, entryReg{srcVFS, dstVFS, parsed})

		if scanner != nil {
			scanner.parsers.RegisterBuildParsedIncludes(dstVFS.Rel(), parsed)
		}

		if reg != nil && reg.Lookup(dstVFS) == nil {
			info := &GeneratedFileInfo{
				ProducerKvP: "CP",
				OutputPath:  dstVFS,
				SourcePath:  srcVFS,
				IsText:      entry.Text,
			}

			// COPY_FILE(TEXT): the .txt source content is substituted verbatim into
			// the dst, so the source — and the fs_tools.py copy tooling — are real
			// inputs of every unit including the dst. Ride them as bare closure
			// leaves so they reach every consumer transitively through the dst's
			// window (scanner splices them in without expanding their own includes),
			// instead of being re-attached per CC source by a full closure re-walk.
			if entry.Text && srcVFS != dstVFS {
				info.ClosureLeaves = append([]VFS{srcVFS}, ctx.scripts[copyFsToolsVFS]...)
			}

			reg.Register(info)
		}
	}

	for i, entry := range d.copyFiles {
		srcVFS := entries[i].srcVFS
		dstVFS := entries[i].dstVFS
		depRefs := resolveCodegenDepRefsExt(ctx, instance, nil, []VFS{srcVFS})
		var closure []VFS

		// COPY_FILE with WITH_CONTEXT pulls the source file's #include closure;
		// COPY_FILE with OUTPUT_INCLUDES additionally pulls the closure of every
		// declared OUTPUT_INCLUDES target. Both fall out of a single walk from
		// dst because dst's registered parsedIncludes contain exactly those.
		// rewriteClosureCPSource swaps sibling-CP $(B) hits for their $(S)
		// sources (CP-specific — CC closures keep $(B)). keepOnlySourceVFS
		// then drops the remaining $(B) entries: upstream's CP closure is
		// source-only (tablegen .inc outputs etc. don't appear as direct CP
		// inputs). dedupVFS collapses repeated post-rewrite entries.
		if moduleInputs != nil && (entry.WithContext || len(entry.OutputIncludes) > 0) {
			closure = walkClosureRoot(ctx, instance, dstVFS, dstVFS.Rel(), *moduleInputs)
			closure = rewriteClosureCPSource(scanner, closure)

			// Before dropping $(B) entries, extract flatc wrapper + .fbs sources for
			// any $(B)/*.fbs.h entries in the closure — these are flatbuffers-generated
			// headers whose source .fbs files and the wrapper script must be inputs.
			if flatcExtras := flatcCCExtraInputs(ctx, closure); len(flatcExtras) > 0 {
				closure = append(closure, flatcExtras...)
			}

			closure = keepOnlySourceVFS(closure)
			closure = dedupVFS(closure)
		}

		ref := EmitCPWithDeps(instance, srcVFS, dstVFS, depRefs, closure, ctx.scripts, ctx.emit)

		// Promote the registration with the producer ref; SourcePath remains.
		if reg != nil {
			if info := reg.Lookup(dstVFS); info != nil {
				info.ProducerRef = ref
				info.HasProducerRef = true
			}
		}
	}
}

func generatedModuleSourceVFS(ctx *genCtx, instance ModuleInstance, srcRel string) *VFS {
	reg := codegenRegForInstance(ctx, instance)

	if reg == nil {
		return nil
	}

	buildVFS := Build(filepath.ToSlash(filepath.Clean(instance.Path + "/" + srcRel)))

	if reg.Lookup(buildVFS) != nil {
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

// autoCopyDstExtras returns AUTO COPY companion paths for entries hit by
// the include closure. Each AUTO copy leaves both the original $(S) source
// and the $(B) destination on disk; upstream's REF tracks both. The scanner
// resolves to whichever path satisfies the #include resolution:
//   - same-extension copies (e.g. .cpp → .cpp): the scanner finds the source
//     in $(S); we add the dst.
//   - extension-changing copies (e.g. .h.txt → .h, codegen_llvm_deps): the
//     scanner can only find the dst in $(B) (the #include uses the dst's
//     extension); we add the source.
//
// The rootDst arg is the CC compile's own input (the .cpp being compiled,
// which is itself an AUTO copy dst); we skip it to avoid double-listing it
// through the .cpp's own #include "X" chain pointing back at its source.
func autoCopyDstExtras(modulePath string, d *moduleData, closure []VFS, rootDst VFS) []VFS {
	if d == nil || len(d.copyFiles) == 0 || len(closure) == 0 {
		return nil
	}

	srcToDst := make(map[VFS]VFS, len(d.copyFiles))
	dstToSrc := make(map[VFS]VFS, len(d.copyFiles))

	for _, entry := range d.copyFiles {
		if !entry.Auto {
			continue
		}

		// entry.Src is normally an arcadia-root-relative path
		// (`yql/.../mkql_builtins.h.txt`). When it starts with `./` or `../`
		// it's relative to the module dir and needs normalising — otherwise
		// Source(entry.Src) yields `$(S)/../…` which can't satisfy a closure
		// match against the canonical source path (e.g. codegen_llvm_deps's
		// `../codegen_llvm_deps.h.txt` from .../codegen/llvm16 resolves to
		// yql/essentials/minikql/codegen/codegen_llvm_deps.h.txt).
		srcRel := entry.Src

		if strings.HasPrefix(srcRel, "./") || strings.HasPrefix(srcRel, "../") {
			srcRel = filepath.ToSlash(filepath.Clean(modulePath + "/" + srcRel))
		}

		srcVFS := Source(srcRel)
		dstVFS := copyFileOutputVFS(modulePath, entry.Dst)

		if dstVFS == srcVFS || dstVFS == rootDst {
			continue
		}

		srcToDst[srcVFS] = dstVFS
		dstToSrc[dstVFS] = srcVFS
	}

	if len(srcToDst) == 0 {
		return nil
	}

	var extras []VFS

	for _, v := range closure {
		if dst, ok := srcToDst[v]; ok {
			extras = append(extras, dst)
		} else if src, ok := dstToSrc[v]; ok {
			extras = append(extras, src)
		}
	}

	return extras
}

// copyProductToolingExtras returns the fs_tools.py copy tooling (fs_tools.py +
// its import closure) when the compiled unit rootDst is itself a COPY product.
//
// The two cross-module attachments this used to also do — re-adding a
// COPY_FILE(TEXT) dst's $(S) source, and the fs_tools tooling for a *consumed*
// TEXT-copied header — now ride transitively as non-expanded closure leaves
// (CodegenRegistry.closureLeaves, registered in emitCopyFiles and spliced into
// the dst's window by the scanner). So a unit that merely includes a TEXT header
// already carries both through its closure; the only case left here is the
// compiled unit's own producer tooling when it is itself a copy (TEXT or not),
// which has no dst-in-closure to ride from.
func copyProductToolingExtras(reg *CodegenRegistry, rootDst VFS, scripts scriptDeps) []VFS {
	if !isCopyProduct(reg, rootDst) {
		return nil
	}

	return scripts[copyFsToolsVFS]
}

// isCopyProduct reports whether v is the $(B) output of a CP (COPY_FILE) node.
func isCopyProduct(reg *CodegenRegistry, v VFS) bool {
	if reg == nil || v.IsSource() {
		return false
	}

	info := reg.Lookup(v)
	return info != nil && info.ProducerKvP == "CP"
}

var copyFsToolsVFS = Intern("$(S)/build/scripts/fs_tools.py")

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
