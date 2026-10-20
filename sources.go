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
	if vfs, ok := moduleRootedVFS(srcInstance.Path.relString(), srcRel); ok {
		return vfs
	}

	srcRelClean := srcRel != "" && pathIsClean(srcRel)

	for i := len(srcDirs) - 1; i >= 1; i-- {
		if srcDirs[i].isBuild() {
			candidate := buildJoinClean(srcDirs[i].relString(), srcRel)

			if ctx.scannerForPlatform(srcInstance.Platform).codegen.lookup(candidate) != nil {
				return candidate
			}

			continue
		}

		var present bool

		if srcRelClean {
			present = ctx.fs.isFileClean(srcDirs[i].rel(), srcRel)
		} else {
			present = ctx.fs.isFile(srcDirs[i].rel(), srcRel)
		}

		if present {
			if srcRelClean {
				return sourceJoined(srcDirs[i].relString(), srcRel)
			}

			return source(filepath.ToSlash(filepath.Clean(srcDirs[i].relString() + "/" + srcRel)))
		}
	}

	if srcRelClean &&
		!ctx.fs.isFileClean(srcInstance.Path.rel(), srcRel) &&
		ctx.fs.isFilePathClean(srcRel) {
		return source(srcRel)
	}

	if srcRelClean {
		return sourceJoined(srcInstance.Path.relString(), srcRel)
	}

	srcRelOnDisk := filepath.ToSlash(filepath.Clean(srcInstance.Path.relString() + "/" + srcRel))

	return source(srcRelOnDisk)
}

func (scanner *IncludeScanner) walkClosure(vfsPath VFS, ctx *ScanContext, domain ScanDomain) Closure {
	parser := scanner.parsers.registry.registeredParserFor(vfsPath.relString())

	if domain == scanDomainCC && parser == IncludeDirectiveParser(swigParser) {
		domain = scanDomainSwig
	}

	sc := scanner.getScanCtx(ctx, domain, parser)

	defer scanner.putScanCtx(sc)

	return sc.closureOf(vfsPath)
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

func (e *EmitContext) moduleSourceRel(src ANY) string {
	if v := src.vfs(); v != 0 {
		return trimModulePrefix(v.relString(), e.instance.Path.relString())
	}

	return src.string()
}

func (e *EmitContext) moduleSourceName(src ANY) string {
	if v := src.vfs(); v != 0 && v.isBuild() {
		return e.moduleSourceRel(src)
	}

	return src.string()
}
