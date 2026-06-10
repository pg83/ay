package main

import (
	"path/filepath"
)

func joinSrcsIncludeClosure(ctx *genCtx, scanPlatform *Platform, srcInstance ModuleInstance, sources []string, in ModuleCCInputs) []VFS {
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
	modDirKey := dirKey(srcInstance.Path.Rel())

	srcRels := make([]string, len(sources))

	for i, src := range sources {
		srcRelOnDisk := srcInstance.Path.Rel() + "/" + src

		if !ctx.fs.IsFile(modDirKey, src) {
			for _, dir := range in.SrcDirs {
				if dir != modDirKey && ctx.fs.IsFile(dir, src) {
					srcRelOnDisk = dir.Rel() + "/" + src

					break
				}
			}
		}

		srcRels[i] = srcRelOnDisk
		visited.add(Source(srcRelOnDisk))
	}

	order := make([]VFS, 0, 1024)

	for _, srcRelOnDisk := range srcRels {
		cfg := ScanContext{
			OwnAddIncl:      in.AddIncl,
			PeerAddInclSet:  in.PeerAddInclGlobal,
			BaseSearchPaths: includeScannerBasePaths(),
			OwnerModuleDir:  srcInstance.Path.Rel(),
		}

		sc := scanner.NewScanCtx(cfg)

		for _, v := range sc.closureOf(Source(srcRelOnDisk)) {
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

func jsCCIncludeInputs(srcInstance ModuleInstance, sources []string, closure []VFS, scripts scriptDeps) []VFS {
	out := make([]VFS, 0, 2+len(sources)+len(closure))
	// gen_join_srcs.py + its import closure (process_command_files.py).
	out = append(out, scripts[buildScriptsGenJoinSrcsPy]...)

	for _, s := range sources {
		out = append(out, Source(srcInstance.Path.Rel()+"/"+s))
	}

	out = append(out, closure...)

	return out
}

func resolveSourceVFS(ctx *genCtx, srcInstance ModuleInstance, srcRel string, srcDirs []VFS) VFS {
	// A rooted spelling — $(S)/$(B) or ${ARCADIA_ROOT}/${ARCADIA_BUILD_ROOT}/
	// ${CURDIR}/${BINDIR}/ — names an exact VFS; build it directly rather than
	// treating the whole token as a module-relative tail (which would bury the
	// macro inside $(S)/<mod>/…). Plain relative paths fall through.
	if vfs := moduleRootedVFS(srcInstance.Path.Rel(), srcRel); vfs != nil {
		return *vfs
	}

	// srcDirs is [moduleDir, SRCDIR1, SRCDIR2, …] (collectModule seeds index 0
	// with the module's own dir). SRCDIR is a cumulative search path where a
	// later declaration wins, so search in reverse and take the first entry that
	// has the file; the module dir (index 0) is the final fallback.
	for i := len(srcDirs) - 1; i >= 1; i-- {
		if ctx.fs.IsFile(srcDirs[i], srcRel) {
			return Source(filepath.ToSlash(filepath.Clean(srcDirs[i].Rel() + "/" + srcRel)))
		}
	}

	// Normalise any literal `..` / `.` segments so SRCS(../foo.cpp) lands
	// at the canonical source path (REF tracks the cleaned form, e.g.
	// $(S)/ydb/public/lib/ydb_cli/commands/ydb_command.cpp, not the
	// command_base/../ydb_command.cpp shape).
	srcRelOnDisk := filepath.ToSlash(filepath.Clean(srcInstance.Path.Rel() + "/" + srcRel))

	return Source(srcRelOnDisk)
}

// walkClosure returns the transitive include closure of vfsPath, root excluded.
// closureOf leads its cached window with the queried node (the scanner's
// subsumption machinery needs self-containing windows); emitters only ever
// want the transitive part, so the root is stripped here once.
func walkClosure(ctx *genCtx, srcInstance ModuleInstance, vfsPath VFS, in ModuleCCInputs) []VFS {
	scanner := ctx.scannerFor(srcInstance)

	if scanner == nil {
		return nil
	}

	cfg := ScanContext{
		OwnAddIncl:      in.AddIncl,
		PeerAddInclSet:  in.PeerAddInclGlobal,
		BaseSearchPaths: includeScannerBasePaths(),
		OwnerModuleDir:  srcInstance.Path.Rel(),
	}

	sc := scanner.NewScanCtx(cfg)
	scanner.walkClosureCalls++

	full := sc.closureOf(vfsPath)

	if full == nil {
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
		info := scanner.codegen.Lookup(v)

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
		if !v.IsSource() {
			continue
		}

		w = append(w, v)
	}

	return w
}

// appendRagelNativeDeps resolves the ragel-native include directives of rl6VFS
// (stored in parsedIncludesRagelNative) and appends the resulting ragel files to
// existing, deduplicating against already-present entries. Also follows
// transitive ragel-native includes. This keeps ragel-included files as
// dependencies without following their C headers (the scanner does not walk
// parsedIncludesRagelNative, only parsedIncludesLocal).
func appendRagelNativeDeps(scanner *IncludeScanner, srcInstance ModuleInstance, rl6VFS VFS, in ModuleCCInputs, existing []VFS) []VFS {
	directives := scanner.parsers.sourceParsedBuckets(rl6VFS).bucket(parsedIncludesRagelNative)

	if len(directives) == 0 {
		return existing
	}

	cfg := ScanContext{
		OwnAddIncl:      in.AddIncl,
		PeerAddInclSet:  in.PeerAddInclGlobal,
		BaseSearchPaths: includeScannerBasePaths(),
		OwnerModuleDir:  srcInstance.Path.Rel(),
	}
	sc := scanner.NewScanCtx(cfg)

	seen := make(map[VFS]bool, len(existing)+len(directives))

	for _, v := range existing {
		seen[v] = true
	}

	type ragelItem struct {
		includer VFS
		incDir   VFS
		d        includeDirective
	}

	rl6Dir := dirKey(pathDir(rl6VFS.Rel()))
	var queue []ragelItem

	for _, d := range directives {
		queue = append(queue, ragelItem{rl6VFS, rl6Dir, d})
	}

	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]

		resolved := sc.resolve(item.includer, item.incDir, item.d)

		for _, f := range resolved {
			if seen[f] {
				continue
			}

			seen[f] = true
			existing = append(existing, f)

			// Follow transitive ragel-native includes of f
			sub := scanner.parsers.sourceParsedBuckets(f).bucket(parsedIncludesRagelNative)

			if len(sub) == 0 {
				continue
			}

			fDir := dirKey(pathDir(f.Rel()))

			for _, sd := range sub {
				queue = append(queue, ragelItem{f, fDir, sd})
			}
		}
	}

	return existing
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
