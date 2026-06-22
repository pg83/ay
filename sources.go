package main

import (
	"path/filepath"
)

func joinSrcsIncludeClosure(ctx *GenCtx, scanPlatform *Platform, srcInstance ModuleInstance, sources []string, in ModuleCCInputs) []VFS {
	scanner := ctx.scannerForPlatform(scanPlatform)

	// Union each source's transitive closure, deduping with a reused IdSet. Seed
	// every source VFS up front so the visited-skip leaves the sources out (and
	// excludes a source that is a transitive dep of an earlier one).
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
		sc := scanner.newScanCtx(cfg, includeDirectiveParsers.registeredParserFor(srcRelOnDisk))

		for _, v := range sc.closureOf(source(srcRelOnDisk)) {
			if visited.has(v) {
				continue
			}

			visited.add(v)
			order = append(order, v)
		}
	}

	if len(order) == 0 {
		return nil
	}

	return order
}

func jsCCIncludeInputs(srcInstance ModuleInstance, joinOut VFS, sources []string, closure []VFS, scripts ScriptDeps) []VFS {
	out := make([]VFS, 0, 3+len(sources)+len(closure))
	// The compiled join output leads (IncludeInputs is the full input window).
	out = append(out, joinOut)
	out = append(out, scripts[buildScriptsGenJoinSrcsPy]...)

	for _, s := range sources {
		out = append(out, source(srcInstance.Path.rel()+"/"+s))
	}

	out = append(out, closure...)

	return out
}

func resolveSourceVFS(ctx *GenCtx, srcInstance ModuleInstance, srcRel string, srcDirs []VFS) VFS {
	// A rooted spelling ($(S)/$(B) or ${ARCADIA_ROOT}/${CURDIR}/…) names an exact
	// VFS; build it directly. Plain relative paths fall through.
	if vfs := moduleRootedVFS(srcInstance.Path.rel(), srcRel); vfs != nil {
		return *vfs
	}

	// srcDirs is [moduleDir, SRCDIR1, …]. SRCDIR is cumulative and later wins, so
	// search in reverse; the module dir (index 0) is the final fallback.
	for i := len(srcDirs) - 1; i >= 1; i-- {
		if ctx.fs.isFile(srcDirs[i], srcRel) {
			if srcRel != "" && pathIsClean(srcRel) {
				return sourceJoined(srcDirs[i].rel(), srcRel)
			}

			return source(filepath.ToSlash(filepath.Clean(srcDirs[i].rel() + "/" + srcRel)))
		}
	}

	// Root-relative SRCS: a clean path resolving under neither the module dir nor
	// any SRCDIR but existing at the source root binds to $(S)/<path>, not the
	// doubled form. The module dir is consulted first (curdir wins).
	if srcRel != "" && pathIsClean(srcRel) &&
		!ctx.fs.isFile(dirKey(srcInstance.Path.rel()), srcRel) &&
		ctx.fs.isFile(srcRootVFS, srcRel) {
		return source(srcRel)
	}

	// Normalise `..` / `.` segments so SRCS(../foo.cpp) lands at the canonical
	// source path (REF tracks the cleaned form).
	if srcRel != "" && pathIsClean(srcRel) {
		return sourceJoined(srcInstance.Path.rel(), srcRel)
	}

	srcRelOnDisk := filepath.ToSlash(filepath.Clean(srcInstance.Path.rel() + "/" + srcRel))

	return source(srcRelOnDisk)
}

// walkClosure returns the transitive include closure WINDOW of vfsPath. The root
// is a member (first for plain files, anywhere within for SCC members), so
// consumers must not re-add it. The parser is keyed on vfsPath.
func walkClosure(scanner *IncludeScanner, vfsPath VFS, cfg ScanContext) []VFS {
	sc := scanner.newScanCtx(cfg, includeDirectiveParsers.registeredParserFor(vfsPath.rel()))
	scanner.walkClosureCalls++

	return sc.closureOf(vfsPath)
}

// walkClosureTail returns only the transitive part of the window, root stripped.
// Sound only for roots that cannot be SCC members (build outputs), where
// closureOf leads the window with the root.
func walkClosureTail(scanner *IncludeScanner, vfsPath VFS, cfg ScanContext) []VFS {
	full := walkClosure(scanner, vfsPath, cfg)

	if len(full) == 0 {
		return nil
	}

	return full[1:]
}

// rewriteClosureCPSource maps any CP (COPY_FILE) output VFS in a closure to its
// registered SourcePath, for CP-node emitters. CC compile closures must NOT use
// this — the $(B) COPY output is the CC input.
func rewriteClosureCPSource(scanner *IncludeScanner, out []VFS) []VFS {
	// out may be a shared cached closure, so clone on the first rewrite instead
	// of mutating in place.
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

// keepOnlySourceVFS drops any $(B) entry from a closure: CP node inputs are
// purely source-level, since generated files reach the cache key through their
// own producers. Run AFTER rewriteClosureCPSource so mapped CP outputs survive.
func keepOnlySourceVFS(out []VFS) []VFS {
	// Build a fresh slice: out may be a shared cached closure.
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
	// Includes resolve via BOTH the build and source roots. Without $(B) here, an
	// angle include of a codegen-produced header falls through to the per-addincl
	// Own/Peer loops, which miss bare `$(B)/<full/path>` lookups the codegen
	// registry handles.
	return []VFS{
		v,
		bld,
		contribLibsLinuxHeaders,
		contribLibsLinuxHeadersNf,
	}
}
