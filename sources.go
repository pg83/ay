package main

// sources.go — helper layer for per-source dispatch and include-closure
// composition used by emit_sources.go.

import (
	"os"
	"path/filepath"
)

// emittedSourceInputPath mirrors composeCCPaths' inputPath logic so
// the walker composes the AR/LD inputs aggregator without round-
// tripping through the emitted node. Returns `$(S)/...` (or
// `$(B)/...` for IsGenerated).
func emittedSourceInputPath(instance ModuleInstance, srcRel string, in ModuleCCInputs, sourceRoot string) VFS {
	if in.IsGenerated {
		return Build(instance.Path + "/" + srcRel)
	}

	if in.SrcDir != nil && *in.SrcDir != instance.Path {
		localCandidate := filepath.Join(sourceRoot, instance.Path, srcRel)
		info, err := os.Stat(localCandidate)

		if err != nil || info.IsDir() {
			return Source(*in.SrcDir + "/" + srcRel)
		}
	}

	return Source(instance.Path + "/" + srcRel)
}

// joinSrcsIncludeClosure walks the include graph for a JOIN_SRCS
// member set. DFS runs over all members with a SHARED visited set
// (mirroring the joined compile — headers reached once stay deduped),
// so total work is O(union closure) not O(sum per-source).
//
// `scanPlatform` chooses scanner + sysincl ISA: callers pass
// `srcInstance.Platform` normally; the JS-target override passes
// `ctx.target` so the closure resolves against target-arch musl peer
// ADDINCL even when the surrounding walk is host-axis.
// instance.Platform is read for module-level facts (Path,
// Flags.NoStdInc), NOT mutated.
func joinSrcsIncludeClosure(ctx *genCtx, scanPlatform *Platform, srcInstance ModuleInstance, sources []string, in ModuleCCInputs) []VFS {
	scanner := ctx.scannerForPlatform(scanPlatform)
	if scanner == nil {
		return nil
	}

	visited := make(idSet, 1024)
	order := make([]uint32, 0, 1024)
	srcAbsSet := make(map[uint32]struct{}, len(sources))

	for _, src := range sources {
		srcRelOnDisk := srcInstance.Path + "/" + src

		if in.SrcDir != nil && *in.SrcDir != srcInstance.Path {
			localCandidate := filepath.Join(ctx.sourceRoot, srcInstance.Path, src)
			info, err := os.Stat(localCandidate)

			if err != nil || info.IsDir() {
				srcRelOnDisk = *in.SrcDir + "/" + src
			}
		}

		cfg := ScanContext{
			SourceRel:       srcRelOnDisk,
			OwnAddIncl:      in.AddIncl,
			PeerAddInclSet:  in.PeerAddInclGlobal,
			BaseSearchPaths: includeScannerBasePaths(srcInstance.Flags.NoStdInc),
		}

		sc := ctx.getScanCtx(scanner, cfg)
		sc.cfg.SourceRel = srcRelOnDisk

		srcAbs := Source(srcRelOnDisk)
		srcID := scanner.interner.internVFS(srcAbs)
		srcAbsSet[srcID] = struct{}{}
		sc.dfsID(srcID, visited, &order)
	}

	if len(order) == 0 {
		return nil
	}

	out := make([]VFS, 0, len(order))
	for _, absID := range order {
		if _, isSrc := srcAbsSet[absID]; isSrc {
			continue
		}
		out = append(out, scanner.interner.vfsByID(absID))
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

func appendVFSUnique(dst []VFS, src []VFS) []VFS {
	seen := make(map[VFS]struct{}, len(dst)+len(src))

	for _, v := range dst {
		seen[v] = struct{}{}
	}

	for _, v := range src {
		if _, dup := seen[v]; dup {
			continue
		}

		seen[v] = struct{}{}
		dst = append(dst, v)
	}

	return dst
}

// jsCCIncludeInputs assembles `[scripts..., sources..., closure...]`
// for the JS-derived CC's IncludeInputs slot (PR-35d).
func jsCCIncludeInputs(srcInstance ModuleInstance, sources []string, closure []VFS) []VFS {
	out := make([]VFS, 0, 2+len(sources)+len(closure))
	out = append(out, Source("build/scripts/gen_join_srcs.py"))
	out = append(out, Source("build/scripts/process_command_files.py"))

	for _, s := range sources {
		out = append(out, Source(srcInstance.Path+"/"+s))
	}

	out = append(out, closure...)

	return out
}

// resolveSourceVFS composes the `$(S)/...` VFS path of a SRCS-declared
// source with composeCCPaths' SRCDIR-aware fallback: when SRCDIR is set
// and no local file exists at instance.Path/<srcRel>, resolve under
// SRCDIR. Registration-time resolution; os.Stat is legitimate here
// because it feeds path composition, not scanner-internal dispatch.
func resolveSourceVFS(ctx *genCtx, srcInstance ModuleInstance, srcRel string, srcDir *string) VFS {
	srcRelOnDisk := srcInstance.Path + "/" + srcRel

	if srcDir != nil && filepath.Clean(*srcDir) != "." && filepath.Clean(*srcDir) != srcInstance.Path {
		cleanSrcDir := filepath.Clean(*srcDir)
		localCandidate := filepath.Join(ctx.sourceRoot, srcInstance.Path, srcRel)
		info, err := os.Stat(localCandidate)

		if err != nil || info.IsDir() {
			srcRelOnDisk = cleanSrcDir + "/" + srcRel
		}
	}

	return Source(srcRelOnDisk)
}

// resolveCodegenDepRefs replaced by the EN/PB/EV-aware version at line 344
// (PR-M3-L0-codegen-deps-EV-PB).

// walkClosure resolves the transitive include closure of a source
// rooted at any VFS path — `$(S)/...` for FS-resident sources or
// `$(B)/...` for codegen outputs registered in the CodegenRegistry.
// Scanner's locator dispatches FS-vs-codegen internally. ScanContext
// mirrors cmd_args -I: own AddIncl + peer GLOBAL AddIncl + the small
// scanner-only baseline for bundled fallbacks (repo-root + linux-headers).
func walkClosure(ctx *genCtx, srcInstance ModuleInstance, vfsPath VFS, in ModuleCCInputs) []VFS {
	return walkClosureWithSourceRel(ctx, srcInstance, vfsPath, vfsPath.Rel, in)
}

func walkClosureWithSourceRel(ctx *genCtx, srcInstance ModuleInstance, vfsPath VFS, sourceRel string, in ModuleCCInputs) []VFS {
	scanner := ctx.scannerFor(srcInstance)
	if scanner == nil {
		return nil
	}

	cfg := ScanContext{
		SourceRel:       sourceRel,
		OwnAddIncl:      in.AddIncl,
		PeerAddInclSet:  in.PeerAddInclGlobal,
		BaseSearchPaths: includeScannerBasePaths(srcInstance.Flags.NoStdInc),
	}

	sc := ctx.getScanCtx(scanner, cfg)

	return sc.WalkClosure(vfsPath)
}

// includeScannerBasePaths returns the scanner baseline that is NOT
// expected to arrive via module/peer ADDINCL propagation:
//   - repo-root fallback (`-I$(S)` shape) for non-no-stdinc consumers;
//   - bundled linux-headers fallback for all C/C++ scanner contexts.
//
// Musl include roots are intentionally NOT part of this baseline.
// Upstream models them through ordinary module ADDINCL:
//   - musl-self gets `arch/<isa>`, `arch/generic`, `src/include`,
//     `src/internal`, `include`, `extra` from contrib/libs/musl/ya.make;
//   - consumers get `arch/<isa>`, `arch/generic`, `include`, `extra`
//     from contrib/libs/musl/include's GLOBAL ADDINCL.
//
// Keeping musl out of the baseline avoids smuggling musl-self-only
// paths (`src/include`, `src/internal`) into arbitrary consumers'
// resolution context.
//
// Non-no-stdinc flavours prepend an empty-string entry representing
// SOURCE_ROOT itself (mirrors `-I$(S)`); `<util/foo.h>` tries
// $(S)/util/foo.h before linux-headers.
//
// No-stdinc flavours MUST NOT get the empty prefix — `-nostdinc` plus a
// fully explicit muslCcIncludes search path. Adding SOURCE_ROOT
// would cause false resolution of system-form includes against the
// repo root, silently expanding musl CC input sets.
func includeScannerBasePaths(noStdInc bool) []VFS {
	base := []VFS{
		Source("contrib/libs/linux-headers"),
		Source("contrib/libs/linux-headers/_nf"),
	}

	if noStdInc {
		return base
	}

	out := make([]VFS, 0, 1+len(base))
	out = append(out, Source(""))
	out = append(out, base...)

	return out
}
