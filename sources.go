package main

import (
	"path/filepath"
)

func joinSrcsIncludeClosure(ctx *genCtx, scanPlatform *Platform, srcInstance ModuleInstance, sources []string, in ModuleCCInputs) []VFS {
	scanner := ctx.scannerForPlatform(scanPlatform)
	if scanner == nil {
		return nil
	}

	visited := scanner.visitedIDPool.Get().(*idSet)
	visited.reset(vfsBound())
	defer scanner.visitedIDPool.Put(visited)
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

		sc := scanner.NewScanCtx(cfg)
		sc.cfg.SourceRel = srcRelOnDisk

		srcAbs := Source(srcRelOnDisk)
		srcID := uint32(srcAbs)
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
		out = append(out, VFS(absID))
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

func jsCCIncludeInputs(srcInstance ModuleInstance, sources []string, closure []VFS) []VFS {
	out := make([]VFS, 0, 2+len(sources)+len(closure))
	out = append(out, Intern("$(S)/build/scripts/gen_join_srcs.py"))
	out = append(out, Intern("$(S)/build/scripts/process_command_files.py"))

	for _, s := range sources {
		out = append(out, Source(srcInstance.Path+"/"+s))
	}

	out = append(out, closure...)

	return out
}

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

func walkClosure(ctx *genCtx, srcInstance ModuleInstance, vfsPath VFS, in ModuleCCInputs) []VFS {
	return walkClosureWithSourceRel(ctx, srcInstance, vfsPath, vfsPath.Rel(), in)
}

func walkClosureWithSourceRel(ctx *genCtx, srcInstance ModuleInstance, vfsPath VFS, sourceRel string, in ModuleCCInputs) []VFS {
	full := walkClosureRoot(ctx, srcInstance, vfsPath, sourceRel, in)
	if full == nil {
		return nil
	}
	return full[1:]
}

func walkClosureRoot(ctx *genCtx, srcInstance ModuleInstance, vfsPath VFS, sourceRel string, in ModuleCCInputs) []VFS {
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

	return scanner.NewScanCtx(cfg).WalkClosure(vfsPath)
}

func includeScannerBasePaths() []VFS {
	return []VFS{
		Intern("$(S)/"),
		Intern("$(S)/contrib/libs/linux-headers"),
		Intern("$(S)/contrib/libs/linux-headers/_nf"),
	}
}
