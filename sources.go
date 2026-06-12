package main

import (
	"path/filepath"
)

func joinSrcsIncludeClosure(ctx *GenCtx, scanPlatform *Platform, srcInstance ModuleInstance, sources []string, in ModuleCCInputs) []VFS {
	scanner := ctx.scannerForPlatform(scanPlatform)

	if scanner == nil {
		return nil
	}

	// Union each source's transitive closure (closureOf), deduping across sources
	// with a reused IdSet. The source files themselves drop out for free: seed the
	// IdSet with every source VFS up front, so the union loop's visited-skip leaves
	// them out — no post-filter, no side set. Seeding ALL sources before any
	// closure walk also excludes a source that is a transitive dep of an earlier
	// source (an incremental seed would miss it; the old full-set filter caught it).
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
	// gen_join_srcs.py + its import closure (process_command_files.py).
	out = append(out, scripts[buildScriptsGenJoinSrcsPy]...)

	for _, s := range sources {
		out = append(out, source(srcInstance.Path.rel()+"/"+s))
	}

	out = append(out, closure...)

	return out
}

func resolveSourceVFS(ctx *GenCtx, srcInstance ModuleInstance, srcRel string, srcDirs []VFS) VFS {
	// A rooted spelling — $(S)/$(B) or ${ARCADIA_ROOT}/${ARCADIA_BUILD_ROOT}/
	// ${CURDIR}/${BINDIR}/ — names an exact VFS; build it directly rather than
	// treating the whole token as a module-relative tail (which would bury the
	// macro inside $(S)/<mod>/…). Plain relative paths fall through.
	if vfs := moduleRootedVFS(srcInstance.Path.rel(), srcRel); vfs != nil {
		return *vfs
	}

	// srcDirs is [moduleDir, SRCDIR1, SRCDIR2, …] (collectModule seeds index 0
	// with the module's own dir). SRCDIR is a cumulative search path where a
	// later declaration wins, so search in reverse and take the first entry that
	// has the file; the module dir (index 0) is the final fallback.
	for i := len(srcDirs) - 1; i >= 1; i-- {
		if ctx.fs.isFile(srcDirs[i], srcRel) {
			if srcRel != "" && pathIsClean(srcRel) {
				return sourceJoined(srcDirs[i].rel(), srcRel)
			}

			return source(filepath.ToSlash(filepath.Clean(srcDirs[i].rel() + "/" + srcRel)))
		}
	}

	// Normalise any literal `..` / `.` segments so SRCS(../foo.cpp) lands
	// at the canonical source path (REF tracks the cleaned form, e.g.
	// $(S)/ydb/public/lib/ydb_cli/commands/ydb_command.cpp, not the
	// command_base/../ydb_command.cpp shape).
	if srcRel != "" && pathIsClean(srcRel) {
		return sourceJoined(srcInstance.Path.rel(), srcRel)
	}

	srcRelOnDisk := filepath.ToSlash(filepath.Clean(srcInstance.Path.rel() + "/" + srcRel))

	return source(srcRelOnDisk)
}

// walkClosure returns the transitive include closure WINDOW of vfsPath — the
// root is a member (windows are self-containing; subsumption needs that), first
// for plain files, anywhere within for SCC members. Consumers treat the window
// as the node's full input list and must not re-add the root. It depends on
// exactly (scanner, vfsPath, cfg): the unregistered-extension parser derives
// from the root (a .swg root parses its .i includes as swig).
func walkClosure(scanner *IncludeScanner, vfsPath VFS, cfg ScanContext) []VFS {
	if scanner == nil {
		return nil
	}

	sc := scanner.newScanCtx(cfg, includeDirectiveParsers.registeredParserFor(vfsPath.rel()))
	scanner.walkClosureCalls++

	return sc.closureOf(vfsPath)
}

// walkClosureTail returns only the transitive part of the window — the root
// stripped. Sound only for roots that cannot be SCC members (build outputs:
// include cycles arise among real headers, never through registered generated
// files), where closureOf is guaranteed to lead the window with the root.
func walkClosureTail(scanner *IncludeScanner, vfsPath VFS, cfg ScanContext) []VFS {
	full := walkClosure(scanner, vfsPath, cfg)

	if len(full) == 0 {
		return nil
	}

	return full[1:]
}

// rewriteClosureCPSource maps any CP (COPY_FILE) output VFS in a closure to
// its registered SourcePath. Used by CP-node emitters (where upstream reports
// sibling COPY sources, not their $(B) outputs, as the canonical input). CC
// compile closures must NOT use this — upstream tracks the $(B) COPY output
// as the CC input directly (it is the file the compiler actually opens).
func rewriteClosureCPSource(scanner *IncludeScanner, out []VFS) []VFS {
	if scanner == nil || scanner.codegen == nil {
		return out
	}

	// out may be a shared cached closure (closureOf returns its slice without
	// copying), so clone on the first rewrite instead of mutating in place. The
	// common case rewrites nothing and returns out untouched.
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

// keepOnlySourceVFS drops any $(B) (build-tree) entry from a closure. CP
// node inputs in upstream are purely source-level — generated files reach the
// CP's cache key indirectly through their own producer nodes (deps), so any
// $(B) hit picked up by transitive include resolution (typically tablegen
// outputs like llvm/IR/Attributes.inc, which surface from a deep LLVM header
// chain) does not belong as a direct CP input. Run AFTER rewriteClosureCPSource
// so CP $(B) outputs already mapped to their SourcePath survive as sources.
func keepOnlySourceVFS(out []VFS) []VFS {
	// Build a fresh slice rather than compacting out[:0] in place: out may be a
	// shared cached closure (closureOf returns its slice uncopied).
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
	// Per upstream module_resolver.cpp:329-331, system/`<…>` includes resolve
	// via MakeResolvePlan(fileConf.BldDir(), fileConf.SrcDir()) — BOTH the
	// build and the source roots. Local `"…"` includes consult both too when
	// IsRequiredBuildAndSrcRoots() is on (line 325). Without $(B) here, an
	// angle include of a codegen-produced header — e.g. flat_boot_lease.cpp's
	// <ydb/core/tablet_flat/flat_executor.pb.h> — falls through to the
	// per-addincl Own/Peer build loops, which key on the addincl prefix and
	// miss bare `$(B)/<full/path>` lookups that the codegen registry's
	// LookupRel handles.
	return []VFS{
		v,
		bld,
		contribLibsLinuxHeaders,
		contribLibsLinuxHeadersNf,
	}
}
