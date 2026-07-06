package main

import (
	"path/filepath"
)

var includeScannerBasePathsSlice = []VFS{
	v,
	bld,
	contribLibsLinuxHeaders,
	contribLibsLinuxHeadersNf,
}

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

func walkClosure(scanner *IncludeScanner, vfsPath VFS, cfg ScanContext) Closure {
	sc := scanner.getScanCtx(cfg, scanner.parsers.registry.registeredParserFor(vfsPath.rel()))

	defer scanner.putScanCtx(sc)

	return sc.closureOf(vfsPath)
}

func rewriteClosureCPSource(scanner *IncludeScanner, cv Closure) []VFS {
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

func includeScannerBasePaths() []VFS {
	return includeScannerBasePathsSlice
}

func (e *EmitContext) generatedModuleSourceVFS(srcRel string) *VFS {
	_, instance := e.ctx, e.instance
	reg := e.codegen

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

func (e *EmitContext) resolveModuleSourceVFS(src STR, srcDirs []VFS) VFS {
	ctx, instance, d := e.ctx, e.instance, e.d

	if buildVFS := copyFileAutoSourceVFS(instance.Path.rel(), d, src); buildVFS != nil {
		return *buildVFS
	}

	srcRel := src.string()

	if buildVFS := e.generatedModuleSourceVFS(srcRel); buildVFS != nil {
		return *buildVFS
	}

	return resolveSourceVFS(ctx, instance, srcRel, srcDirs)
}
