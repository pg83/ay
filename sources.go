package main

// sources.go — helper layer for per-source dispatch and include-closure
// composition used by emit_sources.go.

import (
	"path/filepath"
)

// joinSrcsIncludeClosure walks the include graph for a JOIN_SRCS
// member set with a SHARED visited set across members, mirroring the
// joined compile so total work is O(union closure).
//
// `scanPlatform` chooses scanner + sysincl ISA independently of
// srcInstance.Platform so a JS-target override can resolve against
// target-arch peer ADDINCL during a host-axis walk.
func joinSrcsIncludeClosure(ctx *genCtx, scanPlatform *Platform, srcInstance ModuleInstance, sources []string, in ModuleCCInputs) []VFS {
	scanner := ctx.scannerForPlatform(scanPlatform)
	if scanner == nil {
		return nil
	}

	visited := &idSet{}
	visited.reset(scanner.interner.relIDBound())
	order := make([]uint32, 0, 1024)
	srcAbsSet := make(map[uint32]struct{}, len(sources))

	for _, src := range sources {
		srcRelOnDisk := srcInstance.Path + "/" + src

		if in.SrcDir != nil && *in.SrcDir != srcInstance.Path {
			if !ctx.fs.IsFile(srcInstance.Path + "/" + src) {
				srcRelOnDisk = *in.SrcDir + "/" + src
			}
		}

		cfg := ScanContext{
			SourceRel:       srcRelOnDisk,
			OwnAddIncl:      in.AddIncl,
			PeerAddInclSet:  in.PeerAddInclGlobal,
			BaseSearchPaths: includeScannerBasePaths(),
		}

		sc := scanner.NewScanCtx(cfg, in.SearchTier)
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
// for the JS-derived CC's include-inputs slot.
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
// source with SRCDIR-aware fallback: when SRCDIR is set and no local
// file exists at instance.Path/<srcRel>, resolve under SRCDIR.
// Registration-time resolution — feeds path composition, not scanner
// dispatch. The joined path is path-cleaned so `crypto/../asm/...`
// collapses to `asm/...` (openssl AS sources use this form).
func resolveSourceVFS(ctx *genCtx, srcInstance ModuleInstance, srcRel string, srcDir *string) VFS {
	srcRelOnDisk := srcInstance.Path + "/" + srcRel

	if srcDir != nil && filepath.Clean(*srcDir) != "." && filepath.Clean(*srcDir) != srcInstance.Path {
		cleanSrcDir := filepath.Clean(*srcDir)
		if !ctx.fs.IsFile(srcInstance.Path + "/" + srcRel) {
			srcRelOnDisk = filepath.ToSlash(filepath.Clean(cleanSrcDir + "/" + srcRel))
		}
	}

	return Source(srcRelOnDisk)
}

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
		BaseSearchPaths: includeScannerBasePaths(),
	}

	sc := scanner.NewScanCtx(cfg, in.SearchTier)

	return sc.WalkClosure(vfsPath)
}

// includeScannerBasePaths returns the scanner baseline NOT expected to
// arrive via module/peer ADDINCL: a repo-root fallback (empty prefix,
// mirrors `-I$(S)`) plus bundled linux-headers.
//
// Musl include roots are intentionally absent — upstream models them
// through ordinary module/peer ADDINCL, so musl-self-only paths
// (`src/include`, `src/internal`) never leak into arbitrary consumers.
func includeScannerBasePaths() []VFS {
	return []VFS{
		Source(""),
		Source("contrib/libs/linux-headers"),
		Source("contrib/libs/linux-headers/_nf"),
	}
}
