package main

import (
	"path/filepath"
)

func joinSrcsIncludeClosure(ctx *GenCtx, scanPlatform *Platform, srcInstance ModuleInstance, sources []string, in ModuleCCInputs) []VFS {
	scanner := ctx.scannerForPlatform(scanPlatform)

	visited := scanner.visitedIDPool.Get().(*IdSet)
	visited.reset(vfsBound())

	defer scanner.visitedIDPool.Put(visited)

	modDirKey := dirKey(srcInstance.Path.rel())

	srcRels := make([]string, len(sources))

	for i, src := range sources {
		srcRelOnDisk := srcInstance.Path.rel() + "/" + src

		if !ctx.fs.isFile(modDirKey, src) {
			for _, dir := range in.SrcDirs {
				if dir != modDirKey && ctx.fs.isFile(dir, src) {
					srcRelOnDisk = dir.rel() + "/" + src

					break
				}
			}
		}

		srcRels[i] = srcRelOnDisk
		visited.add(source(srcRelOnDisk))
	}

	order := make([]VFS, 0, 1024)

	cfg := in.ScanCfg

	for _, srcRelOnDisk := range srcRels {
		sc := scanner.getScanCtx(cfg, includeDirectiveParsers.registeredParserFor(srcRelOnDisk))

		for _, v := range sc.closureOf(source(srcRelOnDisk)) {
			if visited.has(v) {
				continue
			}

			visited.add(v)
			order = append(order, v)
		}

		scanner.putScanCtx(sc)
	}

	if len(order) == 0 {
		return nil
	}

	return order
}

func jsCCIncludeInputs(srcInstance ModuleInstance, joinOut VFS, sources []string, closure []VFS, scripts ScriptDeps) []VFS {
	out := make([]VFS, 0, 3+len(sources)+len(closure))

	out = append(out, joinOut)
	out = append(out, scripts[buildScriptsGenJoinSrcsPy]...)

	for _, s := range sources {
		out = append(out, source(srcInstance.Path.rel()+"/"+s))
	}

	out = append(out, closure...)

	return out
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
	sc := scanner.getScanCtx(cfg, includeDirectiveParsers.registeredParserFor(vfsPath.rel()))

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
