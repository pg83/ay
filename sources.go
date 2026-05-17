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
// `scanPlatform` chooses scanner + arch search-paths: callers pass
// `srcInstance.Platform` normally; the JS-target override passes
// `ctx.target` so the closure resolves against target-arch musl even
// when the surrounding walk is host-axis. instance.Platform is read
// for module-level facts (Path, Flags.NoStdInc), NOT mutated.
func joinSrcsIncludeClosure(ctx *genCtx, scanPlatform *Platform, srcInstance ModuleInstance, sources []string, in ModuleCCInputs) []VFS {
	scanner := ctx.scannerForPlatform(scanPlatform)
	if scanner == nil {
		return nil
	}

	visited := NewVFSSet(1024)
	order := make([]VFS, 0, 1024)
	srcAbsSet := make(map[VFS]struct{}, len(sources))

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
			BaseSearchPaths: includeScannerBasePaths(srcInstance.Flags.NoStdInc, scanPlatform),
		}

		sc := ctx.getScanCtx(scanner, cfg)
		sc.cfg.SourceRel = srcRelOnDisk

		srcAbs := Source(srcRelOnDisk)
		srcAbsSet[srcAbs] = struct{}{}
		sc.dfs(srcAbs, visited, &order)
	}

	if len(order) == 0 {
		return nil
	}

	out := make([]VFS, 0, len(order))
	for _, abs := range order {
		if _, isSrc := srcAbsSet[abs]; isSrc {
			continue
		}
		out = append(out, abs)
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

// jsTargetPeerAddIncl rebases a host-axis PeerAddInclGlobal slice to
// the target-axis musl-arch layout for the JS-node closure scan. JS
// nodes are anchored to the target platform axis, so their include
// closure reflects the target's musl-arch paths.
//
// Narrow shim — only the musl-arch entry is rewritten; other entries
// pass through. TODO: replace with general target-addincl propagation.
func jsTargetPeerAddIncl(hostPeerAddIncl []VFS, from, to ISA) []VFS {
	fromMuslArch := Source("contrib/libs/musl/arch/" + string(from))
	toMuslArch := Source("contrib/libs/musl/arch/" + string(to))

	out := make([]VFS, len(hostPeerAddIncl))

	for i, p := range hostPeerAddIncl {
		if p == fromMuslArch {
			out[i] = toMuslArch
		} else {
			out[i] = p
		}
	}

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
// mirrors cmd_args -I: own AddIncl + peer GLOBAL AddIncl + cc-bundle
// implicit baseline (linux-headers + active musl-arch).
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
		BaseSearchPaths: includeScannerBasePaths(srcInstance.Flags.NoStdInc, srcInstance.Platform),
	}

	sc := ctx.getScanCtx(scanner, cfg)

	return sc.WalkClosure(vfsPath)
}

// includeScannerBasePaths returns the implicit include search path
// the cc bundle adds via cmd_args (SOURCE_ROOT + linux-headers +
// musl-arch when applicable). Used as fallback resolution candidates
// so `<util/folder/path.h>` and `<linux/types.h>` resolve compiler-
// identically.
//
// Non-musl flavours prepend an empty-string entry representing
// SOURCE_ROOT itself (mirrors `-I$(S)`); `<util/foo.h>` tries
// $(S)/util/foo.h before linux-headers.
//
// Musl flavours MUST NOT get the empty prefix — `-nostdinc` plus a
// fully explicit muslCcIncludes search path. Adding SOURCE_ROOT
// would cause false resolution of system-form includes against the
// repo root, silently expanding musl CC input sets.
//
// `libcMusl` is the per-MODULE flag; `scanPlatform` is the platform
// to resolve against (typically instance.Platform, but JOIN_SRCS
// during a host walk passes ctx.target to force target-arch paths).
func includeScannerBasePaths(libcMusl bool, scanPlatform *Platform) []VFS {
	base := []VFS{
		Source("contrib/libs/linux-headers"),
		Source("contrib/libs/linux-headers/_nf"),
	}

	if libcMusl {
		muslPaths := []VFS{
			Source("contrib/libs/musl/arch/" + string(scanPlatform.ISA)),
			Source("contrib/libs/musl/arch/generic"),
			Source("contrib/libs/musl/src/include"),
			Source("contrib/libs/musl/src/internal"),
			Source("contrib/libs/musl/include"),
			Source("contrib/libs/musl/extra"),
		}

		out := make([]VFS, 0, len(muslPaths)+len(base))
		out = append(out, muslPaths...)
		out = append(out, base...)

		return out
	}

	out := make([]VFS, 0, 1+len(base))
	out = append(out, Source(""))
	out = append(out, base...)

	return out
}
