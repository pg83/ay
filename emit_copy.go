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
		out = append(out, scanner.parsers.parsedIncludes(srcVFS, nil)...)
	} else if entry.WithContext {
		// Non-TEXT COPY(WITH_CONTEXT …) (e.g. a .cpp plus its sibling .h, copied
		// by a single module) cannot leak across modules, and its quoted
		// includes must resolve relative to the SOURCE dir — pointing at the
		// source node preserves that (e.g. a .cpp's `#include "foo.h"` resolves
		// to the $(S) sibling, not the flat $(B) staging copy).
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
// of any copy whose destination is a linkable object (.a/.o) — upstream archives
// such a copied object into the module's library (e.g. a prebuilt Rust staticlib
// COPY_FILE'd into a PY3_LIBRARY: contrib/python/pydantic-core).
func emitCopyFiles(ctx *GenCtx, instance ModuleInstance, d *ModuleData, moduleInputs *ModuleCCInputs) (memberRefs []NodeRef, memberOuts []VFS, memberSrcs []VFS) {
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
		// Reserve the CP node's ref up front, per entry, so the second loop fills
		// the matching slot (emitCPWithDeps), every dst carries a valid ProducerRef
		// before any CP is built, and a CP whose source resolves to a sibling CP's
		// output sees that producer.
		ref := ctx.emit.reserve()

		// When the COPY source is itself a registered build-generated output carrying
		// a producer source closure (e.g. a parent module COPY_FILE of a child
		// RUN_PROGRAM's OUT_NOAUTO generated_consts.py), upstream's flat-input model
		// lists that producer's transitive $(S) closure on both the CP action and the
		// copied file. Peers resolve through genModule before this module's
		// emitCopyFiles runs, so the producer's ProducerSourceClosure is already
		// registered. An ordinary source-to-build copy resolves srcInfo==nil and keeps
		// current behavior.
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

				// A source-root packaging-stage copy ($(S) original staged into the
				// module) carries its own $(S) input set: the original source plus the
				// fs_tools.py copy tooling. Upstream's flat-input model lists this
				// transitive source closure on every node that consumes the staged
				// copy — so a PY_SRCS source staged through COPY_FILE carries the
				// original source (+ copy scripts) on its py3cc bytecode node, and the
				// original source on the resource objcopy. emit_py_codegen.go reads
				// ProducerSourceClosure for a generated PY_SRCS source unchanged.
				// A build-root source ($(B) generated output staged by COPY_FILE) has
				// no $(S) closure to lift here — its producer's source closure is
				// modeled by the generated-resource buckets, not this copy edge.
				if srcVFS.isSource() {
					info.ProducerSourceClosure = append([]VFS{srcVFS}, ctx.scripts[copyFsToolsVFS]...)
				}
			}

			// Carry a copied generated output's producer source closure onto the dst so
			// a downstream PY_SRCS bytecode (py3cc) node folds the full transitive
			// source set (upstream's flat-input model). The bytecode compiles the
			// $(B) copied file: its closure is the CP's own $(S) copy-tool scripts
			// (fs_tools.py / process_command_files.py) PLUS the $(B) source's producer
			// closure (the $(B) intermediate stays behind the producer node edge).
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
		// Exclude this CP's own reserved ref: an AUTO copy may have src == dst, and
		// without the exclude probe(srcVFS) would now resolve to this very node — a
		// self-dependency (and finalize cycle). The old two-phase code never saw it
		// because dst was not yet bound at this point.
		deps := resolveCodegenDepRefs(ctx, instance, []VFS{srcVFS}, entries[i].ref)

		// Upstream Node2Module first-DFS-leave ownership: when a COPY_FILE consumes
		// another module's generated build output (a ${ARCADIA_BUILD_ROOT}/<dir>/<f>
		// EDT_BuildFrom), this consuming module is a referencer of the producer node.
		// For an OUT_NOAUTO producer that its own module never re-references (e.g. a
		// gen_consts RUN_PROGRAM whose product is copied/consumed only by the parent),
		// the consuming COPY is the first leaver, so the producer node's module_dir AND
		// late ${BINDIR} cwd follow this module. Record the consumer first-claim (first
		// -wins; the override's self/same-module guard makes it a no-op when the
		// producer already owns the output). moduleCCTag is 0 for a plain/PY3 LIBRARY
		// (non-multimodule), so no spurious module_tag is added.
		if len(deps) > 0 && srcVFS.isBuild() && srcVFS != dstVFS {
			var ownerTag STR
			if d.moduleStmt != nil {
				ownerTag = moduleCCTag(d.moduleStmt.Name)
			}
			scanner.recordFirstClaim(srcVFS, instance.Path.rel(), ownerTag)
		}

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
			// The closure root is irrelevant here: rewriteClosureCPSource maps
			// the dst root to its SourcePath (== src) and EmitCPWithDeps filters
			// both src and dst from extraInputs, so the rootless walk is exact.
			closure = walkClosure(ctx.scannerFor(instance), dstVFS, moduleInputs.ScanCfg)
			closure = rewriteClosureCPSource(scanner, closure)
			closure = keepOnlySourceVFS(closure)
			closure = dedupVFS(closure)
		}

		// Copying a registered generated output lists its producer's transitive $(S)
		// closure on the CP action too (upstream's flat-input model). fs_tools.py /
		// process_command_files.py already ride as the CP node's own script inputs.
		if len(entries[i].producerSource) > 0 {
			closure = append(closure, entries[i].producerSource...)
		}

		// Upstream attributes the owning submodule's MODULE_TAG to every node it
		// produces (Node2Module); a PY23_LIBRARY .py copy node therefore carries
		// module_tag=py3. moduleInputs.ModuleTag is the already-computed
		// moduleCCTag(d.moduleStmt.Name) — 0 for an ordinary LIBRARY, so plain
		// copies stay untagged.
		var moduleTag STR
		if moduleInputs != nil {
			moduleTag = moduleInputs.ModuleTag
		}

		emitCPWithDeps(instance, srcVFS, dstVFS, deps, closure, entries[i].ref, moduleTag, d.tc, ctx.scripts, ctx.emit)

		if dst := entry.Dst; strings.HasSuffix(dst, ".a") || strings.HasSuffix(dst, ".o") {
			memberRefs = append(memberRefs, entries[i].ref)
			memberOuts = append(memberOuts, dstVFS)
			// Upstream archives a copied .a/.o alongside its copy source: the
			// prebuilt source rides as a direct input of the module's AR (e.g.
			// pydantic-core's Rust lib_pydantic_core.a).
			memberSrcs = append(memberSrcs, srcVFS)
		}
	}

	return memberRefs, memberOuts, memberSrcs
}

func generatedModuleSourceVFS(ctx *GenCtx, instance ModuleInstance, srcRel string) *VFS {
	reg := codegenRegForInstance(ctx, instance)

	// Lookup-only probe: the canonical "$(B)/<mod>/<src>" is assembled in the
	// scratch buffer and probed without inserting — a plain src (the common
	// case) used to intern a never-registered $(B) path per call.
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
