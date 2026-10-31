package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

var includeScannerBasePathsSlice = []VFS{
	v,
	bld,
	contribLibsLinuxHeaders,
	contribLibsLinuxHeadersNf,
}

type DeclaredSource struct {
	Token    ANY
	Resolved ResolvedSource
}

type ResolvedSource struct {
	GeneratedDirs int
	Path          VFS
	Existing      bool
	Fixed         bool
}

func resolveSourceVFS(ctx *GenCtx, instance ModuleInstance, src string, dirs []VFS) VFS {
	return resolveSource(ctx, instance, src, dirs).Path
}

func resolveSource(ctx *GenCtx, srcInstance ModuleInstance, srcRel string, srcDirs []VFS) ResolvedSource {
	if vfs, ok := moduleRootedVFS(srcInstance.Path.relString(), srcRel); ok {
		return ResolvedSource{Path: vfs, Existing: vfs.isSource() && ctx.fs.isFileVFS(vfs), Fixed: true}
	}

	srcRelClean := srcRel != "" && pathIsClean(srcRel)

	if srcRelClean {
		if ctx.fs.isFileClean(srcInstance.Path.rel(), srcRel) {
			path := sourceJoined(cleanRel(srcInstance.Path.relString()), srcRel)

			ctx.fs.noteFile(path)

			return ResolvedSource{Path: path, Existing: true}
		}
	} else if ctx.fs.isFile(srcInstance.Path.rel(), srcRel) {
		return ResolvedSource{Path: source(filepath.ToSlash(filepath.Clean(srcInstance.Path.relString() + "/" + srcRel))), Existing: true}
	}

	generatedDirs := 0

	for i := 1; i < len(srcDirs); i++ {
		if srcDirs[i].isBuild() {
			candidate := buildJoinClean(srcDirs[i].relString(), srcRel)

			if ctx.scannerForPlatform(srcInstance.Platform).codegen.lookup(candidate) != nil {
				return ResolvedSource{Path: candidate, GeneratedDirs: generatedDirs}
			}

			generatedDirs = i + 1

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
				return ResolvedSource{Path: sourceJoined(cleanRel(srcDirs[i].relString()), srcRel), Existing: true, GeneratedDirs: generatedDirs}
			}

			return ResolvedSource{Path: source(filepath.ToSlash(filepath.Clean(srcDirs[i].relString() + "/" + srcRel))), Existing: true, GeneratedDirs: generatedDirs}
		}
	}

	if srcRelClean && ctx.fs.isFilePathClean(srcRel) {
		return ResolvedSource{Path: source(srcRel), Existing: true, GeneratedDirs: generatedDirs}
	}

	if !srcRelClean && srcRel != "" {
		if rooted := filepath.ToSlash(filepath.Clean(srcRel)); rooted != ".." && !strings.HasPrefix(rooted, "../") && ctx.fs.isFilePathClean(rooted) {
			return ResolvedSource{Path: source(rooted), Existing: true, GeneratedDirs: generatedDirs}
		}
	}

	if srcRelClean {
		return ResolvedSource{Path: sourceJoined(cleanRel(srcInstance.Path.relString()), srcRel), GeneratedDirs: generatedDirs}
	}

	srcRelOnDisk := filepath.ToSlash(filepath.Clean(srcInstance.Path.relString() + "/" + srcRel))

	return ResolvedSource{Path: source(srcRelOnDisk), GeneratedDirs: generatedDirs}
}

func resolveSourceOrGeneratedVFS(ctx *GenCtx, instance ModuleInstance, src string, srcDirs []VFS) VFS {
	return resolveSource(ctx, instance, src, srcDirs).withGenerated(ctx.scannerForPlatform(instance.Platform).codegen, src, srcDirs)
}

func (r ResolvedSource) withGeneratedDirs(registry *CodegenRegistry, token string, dirs []VFS) VFS {
	if !r.Fixed && r.GeneratedDirs > 1 {
		for _, dir := range dirs[1:r.GeneratedDirs] {
			if dir.isBuild() {
				candidate := buildJoinClean(dir.relString(), token)

				if registry.lookup(candidate) != nil {
					return candidate
				}
			}
		}
	}

	return r.Path
}

func (r ResolvedSource) withGenerated(registry *CodegenRegistry, token string, dirs []VFS) VFS {
	if path := r.withGeneratedDirs(registry, token, dirs); path != r.Path {
		return path
	}

	if r.Existing || r.Fixed || !r.Path.isSource() {
		return r.Path
	}

	candidate := r.Path.rel().build()

	if registry.lookup(candidate) != nil {
		return candidate
	}

	if pathIsClean(token) {
		candidate = build(token)

		if registry.lookup(candidate) != nil {
			return candidate
		}
	}

	return r.Path
}

func (e *EmitContext) resolveDeclaredSource(token ANY) ResolvedSource {
	if v := token.vfs(); v != 0 {
		existing := v.isSource() && e.ctx.fs.isFileVFS(v)

		if v.isSource() && !existing && !slices.Contains(e.unknownSources, v) {
			e.unknownSources = append(e.unknownSources, v)
			e.ctx.onWarn(Warn{Kind: WarnUnknownLocation, Message: fmt.Sprintf("File location still unknown: %s; resolving it in current source dir $(S)/%s", v.string(), e.instance.Path.relString())})
		}

		return ResolvedSource{Path: v, Existing: existing, Fixed: true}
	}

	return resolveSource(e.ctx, e.instance, token.string(), e.d.srcDirs)
}

func (e *EmitContext) resolveSrc(meta *SrcMeta) {
	if meta.Resolved.Path == 0 {
		meta.Resolved = e.resolveDeclaredSource(meta.Source)
	}
}

func (e *EmitContext) sourceForEmission(meta SrcMeta) VFS {
	e.resolveSrc(&meta)

	if meta.Resolved.Fixed || meta.Resolved.GeneratedDirs == 0 && (meta.Resolved.Existing || meta.Resolved.Path.isBuild()) {
		return meta.Resolved.Path
	}

	return meta.Resolved.withGenerated(e.codegen, meta.Source.string(), e.d.srcDirs)
}

func (e *EmitContext) schedulingSource(token string, resolved ResolvedSource) VFS {
	if resolved.Path.isSource() && !resolved.Existing {
		return moduleInputCandidateVFS(e.instance.Path.relString(), token)
	}

	return resolved.Path
}

func (scanner *IncludeScanner) walkClosure(vfsPath VFS, ctx *ScanContext, domain ScanDomain) Closure {
	return scanner.walkClosureWithDefaultParser(vfsPath, ctx, domain, scanner.parsers.registry.defaultParser)
}

func (scanner *IncludeScanner) walkClosureWithDefaultParser(vfsPath VFS, ctx *ScanContext, domain ScanDomain, defaultParser IncludeDirectiveParser) Closure {
	cl := scanner.scanClosureWithDefaultParser(vfsPath, ctx, domain, defaultParser)

	scanner.reportClosureDiagnostics(vfsPath, cl)

	return cl
}

func (scanner *IncludeScanner) scanClosure(vfsPath VFS, ctx *ScanContext, domain ScanDomain) Closure {
	return scanner.scanClosureWithDefaultParser(vfsPath, ctx, domain, scanner.parsers.registry.defaultParser)
}

func (scanner *IncludeScanner) scanClosureWithDefaultParser(vfsPath VFS, ctx *ScanContext, domain ScanDomain, defaultParser IncludeDirectiveParser) Closure {
	sc := scanner.getFileScanCtx(vfsPath, ctx, domain, defaultParser)

	defer scanner.putScanCtx(sc)

	return sc.closureOf(vfsPath)
}

func (scanner *IncludeScanner) getFileScanCtx(vfsPath VFS, ctx *ScanContext, domain ScanDomain, defaultParser IncludeDirectiveParser) *ScanCtx {
	parser, registered := scanner.parsers.registry.registeredParserFor(vfsPath.relString())

	if !registered {
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

func includeScannerBasePaths(p *Platform) []VFS {
	if p.OS != OSLinux {
		return includeScannerBasePathsSlice[:2]
	}

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

	buildVFS := id.build()

	if reg.lookup(buildVFS) != nil {
		return ptr(buildVFS)
	}

	return nil
}

func (e *EmitContext) resolveModuleSourceVFS(srcAny ANY, srcDirs []VFS) VFS {
	if v := srcAny.vfs(); v != 0 {
		return v
	}

	return resolveSourceOrGeneratedVFS(e.ctx, e.instance, srcAny.str().string(), srcDirs)
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

	sc := scanner.getFileScanCtx(input, ctx, scanDomainCC, scanner.parsers.registry.defaultParser)

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
