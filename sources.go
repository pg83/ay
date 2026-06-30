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
		!ctx.fs.isFile(dirKey(srcInstance.Path.rel()), srcRel) &&
		ctx.fs.isFile(srcRootVFS, srcRel) {
		return source(srcRel)
	}

	if srcRel != "" && pathIsClean(srcRel) {
		return sourceJoined(srcInstance.Path.rel(), srcRel)
	}

	srcRelOnDisk := filepath.ToSlash(filepath.Clean(srcInstance.Path.rel() + "/" + srcRel))

	return source(srcRelOnDisk)
}

func walkClosure(scanner *IncludeScanner, vfsPath VFS, cfg ScanContext) []VFS {
	sc := scanner.getScanCtx(cfg, scanner.parsers.registry.registeredParserFor(vfsPath.rel()))

	defer scanner.putScanCtx(sc)

	scanner.walkClosureCalls++

	return sc.closureOf(vfsPath)
}

func walkClosureTail(scanner *IncludeScanner, vfsPath VFS, cfg ScanContext) []VFS {
	full := walkClosure(scanner, vfsPath, cfg)

	if len(full) == 0 {
		return nil
	}

	return full[1:]
}

func rewriteClosureCPSource(scanner *IncludeScanner, out []VFS) []VFS {
	var result []VFS

	for i, v := range out {
		info := scanner.codegen.lookup(v)

		if info == nil || info.SourcePath == 0 {
			continue
		}

		if result == nil {
			result = append(result, out...)
		}

		result[i] = info.SourcePath
	}

	if result == nil {
		return out
	}

	return result
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
