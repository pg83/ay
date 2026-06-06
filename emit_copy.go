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

			// The fs_tools.py copy tooling is a real input of every copy product, so
			// ride it as a non-expanded closure leaf of the dst — it then reaches the
			// dst's own compile (and every consumer) transitively through the dst's
			// cached window, instead of being re-attached per CC/BC source.
			// COPY_FILE(TEXT) and AUTO COPY additionally materialize the $(S) source,
			// which upstream lists alongside, so add it as a leaf too.
			if srcVFS != dstVFS {
				info.ClosureLeaves = append(info.ClosureLeaves, ctx.scripts[copyFsToolsVFS]...)

				if entry.Text || entry.Auto {
					info.ClosureLeaves = append(info.ClosureLeaves, srcVFS)
				}
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
