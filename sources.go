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
	if vfs := moduleRootedVFS(srcInstance.Path.relString(), srcRel); vfs != nil {
		return *vfs
	}

	for i := len(srcDirs) - 1; i >= 1; i-- {
		if ctx.fs.isFile(srcDirs[i].rel(), srcRel) {
			if srcRel != "" && pathIsClean(srcRel) {
				return sourceJoined(srcDirs[i].relString(), srcRel)
			}

			return source(filepath.ToSlash(filepath.Clean(srcDirs[i].relString() + "/" + srcRel)))
		}
	}

	if srcRel != "" && pathIsClean(srcRel) &&
		!ctx.fs.isFile(srcInstance.Path.rel(), srcRel) &&
		ctx.fs.isFile(srcRootRel, srcRel) {
		return source(srcRel)
	}

	if srcRel != "" && pathIsClean(srcRel) {
		return sourceJoined(srcInstance.Path.relString(), srcRel)
	}

	srcRelOnDisk := filepath.ToSlash(filepath.Clean(srcInstance.Path.relString() + "/" + srcRel))

	return source(srcRelOnDisk)
}

func walkClosure(scanner *IncludeScanner, vfsPath VFS, cfg ScanContext) Closure {
	sc := scanner.getScanCtx(cfg, scanner.parsers.registry.registeredParserFor(vfsPath.relString()))

	defer scanner.putScanCtx(sc)

	return sc.closureOf(vfsPath)
}

func rewriteClosureCPSource(na *NodeArenas, scanner *IncludeScanner, cv Closure) []VFS {
	out := cv.collect(na, func(VFS) bool { return true })

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
		id = internedPrefixedJoined("", instance.Path.relString(), srcRel)
	} else {
		id = internedPrefixed("", filepath.ToSlash(filepath.Clean(instance.Path.relString()+"/"+srcRel)))
	}

	if id == 0 {
		return nil
	}

	buildVFS := id.build()

	if reg.lookup(buildVFS) != nil {
		return ptr(buildVFS)
	}

	return nil
}

func (e *EmitContext) resolveModuleSourceVFS(srcAny ANY, srcDirs []VFS) VFS {
	ctx, instance, d := e.ctx, e.instance, e.d

	if v := srcAny.vfs(); v != 0 {
		return v
	}

	src := srcAny.str()

	if buildVFS := copyFileAutoSourceVFS(instance.Path.relString(), d, src); buildVFS != nil {
		return *buildVFS
	}

	srcRel := src.string()

	if buildVFS := e.generatedModuleSourceVFS(srcRel); buildVFS != nil {
		return *buildVFS
	}

	return resolveSourceVFS(ctx, instance, srcRel, srcDirs)
}
