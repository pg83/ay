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

func resolveSourceOrGeneratedVFS(ctx *GenCtx, instance ModuleInstance, src string, srcDirs []VFS) VFS {
	vfs := resolveSourceVFS(ctx, instance, src, srcDirs)

	if _, rooted := moduleRootedVFS(instance.Path.relString(), src); !rooted && vfs.isSource() {
		candidate := vfs.rel().build()

		if ctx.scannerForPlatform(instance.Platform).codegen.lookup(candidate) != nil && !ctx.fs.isFile(srcRootRel, vfs.relString()) {
			return candidate
		}
	}

	return vfs
}

func (scanner *IncludeScanner) walkClosure(vfsPath VFS, ctx *ScanContext, domain ScanDomain) Closure {
	return scanner.walkClosureWithDefaultParser(vfsPath, ctx, domain, nil)
}

func (scanner *IncludeScanner) walkClosureWithDefaultParser(vfsPath VFS, ctx *ScanContext, domain ScanDomain, defaultParser IncludeDirectiveParser) Closure {
	cl := scanner.scanClosureWithDefaultParser(vfsPath, ctx, domain, defaultParser)

	scanner.reportClosureDiagnostics(vfsPath, cl)

	return cl
}

func (scanner *IncludeScanner) scanClosure(vfsPath VFS, ctx *ScanContext, domain ScanDomain) Closure {
	return scanner.scanClosureWithDefaultParser(vfsPath, ctx, domain, nil)
}

func (scanner *IncludeScanner) scanClosureWithDefaultParser(vfsPath VFS, ctx *ScanContext, domain ScanDomain, defaultParser IncludeDirectiveParser) Closure {
	sc := scanner.getFileScanCtx(vfsPath, ctx, domain, defaultParser)

	defer scanner.putScanCtx(sc)

	return sc.closureOf(vfsPath)
}

func (scanner *IncludeScanner) getFileScanCtx(vfsPath VFS, ctx *ScanContext, domain ScanDomain, defaultParser IncludeDirectiveParser) *ScanCtx {
	parser := scanner.parsers.registry.registeredParserFor(vfsPath.relString())

	if parser == nil {
		parser = defaultParser
	}

	if domain == scanDomainCC {
		switch parser {
		case IncludeDirectiveParser(swigParser):
			domain = scanDomainSwig
		case IncludeDirectiveParser(yasmParser):
			domain = scanDomainAsm
		case IncludeDirectiveParser(scParser):
			domain = scanDomainSc
		}
	}

	return scanner.getScanCtx(ctx, domain, parser)
}

func includeScannerBasePaths() []VFS {
	return includeScannerBasePathsSlice
}

func (e *EmitContext) generatedModuleSourceVFS(srcRel string) *VFS {
	reg := e.codegen
	module := e.instance.Path.relString()

	var id STR

	if srcRel != "" && pathIsClean(srcRel) {
		id = internedPrefixedJoined("", module, srcRel)

		if module == "." && (id == 0 || reg.lookup(id.build()) == nil) {
			id = internedPrefixedJoined("", "", srcRel)
		}
	} else {
		id = internedPrefixed("", filepath.ToSlash(filepath.Clean(module+"/"+srcRel)))
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

func (scanner *IncludeScanner) discoverInput(input VFS, ctx *ScanContext) {
	if !input.isSource() || !scanner.parsers.registry.scansIncludes(input.relString()) {
		return
	}

	if _, cached := scanner.closure(input); cached {
		return
	}

	sc := scanner.getFileScanCtx(input, ctx, scanDomainCC, nil)

	defer scanner.putScanCtx(sc)

	sc.initialOnly = true
	sc.closureOf(input)
}

func (scanner *IncludeScanner) establishedClosure(input VFS) Closure {
	if cl, ok := scanner.closure(input); ok && !cl.incomplete {
		return cl
	}

	sc := scanner.scanCtxPool.Get().(*ScanCtx)

	*sc = ScanCtx{scanner: scanner, readOnly: true, tjc: tarjans.get()}

	defer scanner.putScanCtx(sc)

	return sc.closureOf(input)
}
