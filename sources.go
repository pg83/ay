package main

import (
	"path/filepath"
)

func resolveSourceVFS(ctx *GenCtx, srcInstance ModuleInstance, srcRel string, srcDirs []VFS) VFS {
	if vfs := moduleRootedVFS(srcInstance.Path.rel(), srcRel); vfs != nil {
		return *vfs
	}

	for i := len(srcDirs) - 1; i >= 1; i-- {
		if ctx.fs.isFile(srcDirs[i], srcRel) {
			if srcRel != "" && pathIsClean(srcRel) {
				return sourceJoined(srcDirs[i].rel(), srcRel)
			}

			return source(filepath.ToSlash(filepath.Clean(srcDirs[i].rel() + "/" + srcRel)))
		}
	}

	if srcRel != "" && pathIsClean(srcRel) &&
		!ctx.fs.isFile(srcInstance.Path, srcRel) &&
		ctx.fs.isFile(srcRootVFS, srcRel) {
		return source(srcRel)
	}

	if srcRel != "" && pathIsClean(srcRel) {
		return sourceJoined(srcInstance.Path.rel(), srcRel)
	}

	srcRelOnDisk := filepath.ToSlash(filepath.Clean(srcInstance.Path.rel() + "/" + srcRel))

	return source(srcRelOnDisk)
}

func walkClosure(scanner *IncludeScanner, vfsPath VFS, cfg ScanContext) ClosureView {
	sc := scanner.getScanCtx(cfg, scanner.parsers.registry.registeredParserFor(vfsPath.rel()))

	defer scanner.putScanCtx(sc)

	scanner.walkClosureCalls++

	return sc.closureOf(vfsPath)
}

func walkClosureTail(scanner *IncludeScanner, vfsPath VFS, cfg ScanContext) ClosureView {
	cv := walkClosure(scanner, vfsPath, cfg)
	cv.self = 0

	return cv
}

func rewriteClosureCPSource(scanner *IncludeScanner, cv ClosureView) []VFS {
	out := cv.collect(func(VFS) bool { return true })

	for i, v := range out {
		info := scanner.codegen.lookup(v)

		if info == nil || info.SourcePath == 0 {
			continue
		}

		out[i] = info.SourcePath
	}

	return out
}

func keepOnlySourceVFS(out []VFS) []VFS {
	var w []VFS

	for _, v := range out {
		if !v.isSource() {
			continue
		}

		w = append(w, v)
	}

	return w
}

func includeScannerBasePaths() []VFS {
	return []VFS{
		v,
		bld,
		contribLibsLinuxHeaders,
		contribLibsLinuxHeadersNf,
	}
}
