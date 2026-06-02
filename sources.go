package main

import (
	"path/filepath"
)

var (
	// Path constants hoisted by `ay refac consts`.
	bld                       = Build("")
	contribLibsLinuxHeaders   = Source("contrib/libs/linux-headers")
	contribLibsLinuxHeadersNf = Source("contrib/libs/linux-headers/_nf")
	v                         = Source("")
)

func joinSrcsIncludeClosure(ctx *genCtx, scanPlatform *Platform, srcInstance ModuleInstance, sources []string, in ModuleCCInputs) []VFS {
	scanner := ctx.scannerForPlatform(scanPlatform)

	if scanner == nil {
		return nil
	}

	// Union each source's transitive closure (closureOf), deduping across
	// sources with a reused idSet, then drop the source files themselves.
	visited := scanner.visitedIDPool.Get().(*idSet)
	visited.reset(vfsBound())
	defer scanner.visitedIDPool.Put(visited)
	order := make([]VFS, 0, 1024)
	srcAbsSet := make(map[VFS]struct{}, len(sources))

	for _, src := range sources {
		srcRelOnDisk := srcInstance.Path + "/" + src

		if in.SrcDir != nil && *in.SrcDir != srcInstance.Path {
			if !ctx.fs.IsFile(srcInstance.Path + "/" + src) {
				srcRelOnDisk = *in.SrcDir + "/" + src
			}
		}

		cfg := ScanContext{
			SourceRel:       srcRelOnDisk,
			OwnAddIncl:      in.AddIncl,
			PeerAddInclSet:  in.PeerAddInclGlobal,
			BaseSearchPaths: includeScannerBasePaths(),
			OwnerModuleDir:  srcInstance.Path,
		}

		sc := scanner.NewScanCtx(cfg)

		srcAbs := Source(srcRelOnDisk)
		srcAbsSet[srcAbs] = struct{}{}

		for _, v := range sc.closureOf(srcAbs) {
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

	out := make([]VFS, 0, len(order))

	for _, abs := range order {
		if _, isSrc := srcAbsSet[abs]; isSrc {
			continue
		}

		out = append(out, abs)
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

func appendVFSUnique(dst []VFS, src []VFS) []VFS {
	seen := make(map[VFS]struct{}, len(dst)+len(src))

	for _, v := range dst {
		seen[v] = struct{}{}
	}

	for _, v := range src {
		if _, dup := seen[v]; dup {
			continue
		}

		seen[v] = struct{}{}
		dst = append(dst, v)
	}

	return dst
}

func jsCCIncludeInputs(srcInstance ModuleInstance, sources []string, closure []VFS, scripts scriptDeps) []VFS {
	out := make([]VFS, 0, 2+len(sources)+len(closure))
	// gen_join_srcs.py + its import closure (process_command_files.py).
	out = append(out, scripts[buildScriptsGenJoinSrcsPy]...)

	for _, s := range sources {
		out = append(out, Source(srcInstance.Path+"/"+s))
	}

	out = append(out, closure...)

	return out
}

func resolveSourceVFS(ctx *genCtx, srcInstance ModuleInstance, srcRel string, srcDir *string) VFS {
	srcRelOnDisk := srcInstance.Path + "/" + srcRel

	if srcDir != nil && filepath.Clean(*srcDir) != "." && filepath.Clean(*srcDir) != srcInstance.Path {
		cleanSrcDir := filepath.Clean(*srcDir)

		if !ctx.fs.IsFile(srcInstance.Path + "/" + srcRel) {
			srcRelOnDisk = filepath.ToSlash(filepath.Clean(cleanSrcDir + "/" + srcRel))
		}
	}

	// Normalise any literal `..` / `.` segments so SRCS(../foo.cpp) lands
	// at the canonical source path (REF tracks the cleaned form, e.g.
	// $(S)/ydb/public/lib/ydb_cli/commands/ydb_command.cpp, not the
	// command_base/../ydb_command.cpp shape).
	srcRelOnDisk = filepath.ToSlash(filepath.Clean(srcRelOnDisk))

	return Source(srcRelOnDisk)
}

func walkClosure(ctx *genCtx, srcInstance ModuleInstance, vfsPath VFS, in ModuleCCInputs) []VFS {
	return walkClosureWithSourceRel(ctx, srcInstance, vfsPath, vfsPath.Rel(), in)
}

func walkClosureWithSourceRel(ctx *genCtx, srcInstance ModuleInstance, vfsPath VFS, sourceRel string, in ModuleCCInputs) []VFS {
	full := walkClosureRoot(ctx, srcInstance, vfsPath, sourceRel, in)

	if full == nil {
		return nil
	}

	return full[1:]
}

func walkClosureRoot(ctx *genCtx, srcInstance ModuleInstance, vfsPath VFS, sourceRel string, in ModuleCCInputs) []VFS {
	scanner := ctx.scannerFor(srcInstance)

	if scanner == nil {
		return nil
	}

	cfg := ScanContext{
		SourceRel:       sourceRel,
		OwnAddIncl:      in.AddIncl,
		PeerAddInclSet:  in.PeerAddInclGlobal,
		BaseSearchPaths: includeScannerBasePaths(),
		OwnerModuleDir:  srcInstance.Path,
	}

	sc := scanner.NewScanCtx(cfg)
	scanner.walkClosureCalls++

	if vfsPath.IsSource() {
		sc.cfg.SourceRel = vfsPath.Rel()
	}

	// The closure is rooted at vfsPath (closureOf leads its result with the
	// queried node); callers that want only the transitive includes strip
	// element 0.
	return sc.closureOf(vfsPath)
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
		SourceRel:       rl6VFS.Rel(),
		OwnAddIncl:      in.AddIncl,
		PeerAddInclSet:  in.PeerAddInclGlobal,
		BaseSearchPaths: includeScannerBasePaths(),
		OwnerModuleDir:  srcInstance.Path,
	}
	sc := scanner.NewScanCtx(cfg)

	seen := make(map[VFS]bool, len(existing)+len(directives))

	for _, v := range existing {
		seen[v] = true
	}

	var queue []struct {
		includer VFS
		d        includeDirective
	}

	for _, d := range directives {
		queue = append(queue, struct {
			includer VFS
			d        includeDirective
		}{rl6VFS, d})
	}

	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]

		resolved := sc.resolve(item.includer, item.d)

		for _, f := range resolved {
			if seen[f] {
				continue
			}

			seen[f] = true
			existing = append(existing, f)

			// Follow transitive ragel-native includes of f
			sub := scanner.parsers.sourceParsedBuckets(f).bucket(parsedIncludesRagelNative)

			for _, sd := range sub {
				queue = append(queue, struct {
					includer VFS
					d        includeDirective
				}{f, sd})
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
