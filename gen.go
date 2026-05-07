package main

import (
	"path/filepath"
	"strings"
)

// gen.go — top-level "parse a ya.make and emit its build subgraph"
// driver. PR-25 wires the macro evaluator + per-instance flag
// derivation + dispatch by source extension + host-tool recursion
// into the walker that PR-23 retrofitted onto `ModuleInstance`. The
// shape stays depth-first, post-order, declaration-order over PEERDIR
// (R14 link order) — what changed is the per-module work performed
// once a ya.make has been parsed.
//
// Macro vocabulary the walker now understands:
//
//   - `IF (cond) ... [ELSE ...] ENDIF` — evaluated via macros.go's
//     EvalCond against a per-instance env (target/host platform +
//     ARCH_AARCH64 / ARCH_X86_64 / MUSL flags etc.). The taken branch
//     is inlined; unreached branches contribute nothing.
//   - `NO_LIBC`, `NO_UTIL`, `NO_RUNTIME`, `NO_PLATFORM`,
//     `NO_COMPILER_WARNINGS` — set the corresponding boolean on the
//     instance's FlagSet. Macro-derived flags take precedence over
//     `inferFlagsFromPath`'s heuristic.
//   - `ADDINCL([GLOBAL] paths...)`, `CFLAGS([GLOBAL] flags...)`,
//     `LDFLAGS(flags...)`, `SRCDIR(dir)` — collected per-module but
//     not yet threaded into the rule emitters' cmd_args (PR-26's
//     flag-bundle work). The collection happens here so PR-26 can
//     extend EmitCC's signature without touching gen.go again.
//   - `JOIN_SRCS(name srcs...)` — emits a JS node + a CC node that
//     compiles the joined output. The CC node's own `.cpp` source is
//     the JS output relative to the module path.
//   - `GLOBAL_SRCS(srcs...)` — collected as separate sources; their
//     CC outputs flow into a SECOND AR node (`<lib>.global.a`) with
//     `module_tag=global` per `EmitARGlobal`.
//   - `INCLUDE(path)` — already inlined by the parser at parse time;
//     the walker never sees an IncludeStmt.
//
// Source dispatch by extension:
//
//   - `.c` / `.cpp` / `.cc` / `.cxx` → EmitCC.
//   - `.h` / `.hpp` — silently skipped (headers in SRCS are rule-
//     metadata only, not compiled).
//   - `.S` / `.s` → EmitAS. Caller may need a host yasm LD ref; PR-25
//     plumbs it conditionally (only when `instance.Flags.PIC` and
//     the module's path matches the asmlib heuristic).
//   - `.rl6` → EmitR6 (host ragel6 LD via `WithHost` recursion into
//     `contrib/tools/ragel6`), then EmitCC of the generated `.cpp`.
//
// Cross-platform recursion (D31):
//
// When a `.rl6` source is processed, the walker constructs the host
// ragel6 instance (`instance.WithHost(ctx.cfg)` with Path overridden
// to `contrib/tools/ragel6`) and recurses through `genModule`. The
// resulting host LD NodeRef threads into EmitR6's `ragel6LD`
// parameter. Same shape applies for yasm when an `.S` source needs
// it. If the host tool's ya.make does not parse cleanly (the
// upstream uses `IF (USE_PREBUILT_TOOLS) ... INCLUDE(...)` blocks the
// PR-25 evaluator does not bind), the recursion throws — that is the
// expected PR-26 escalation point and is documented in the PR-25
// Completed entry.
//
// PR-25 acceptance scope: the walker mechanism itself. Synthetic
// tests in `gen_test.go` exercise IF / JOIN_SRCS / GLOBAL_SRCS /
// `.rl6` host recursion. The full `tools/archiver` PEERDIR closure
// is PR-26's job — PR-25 only ensures `Gen(...)` against
// `tools/archiver` does NOT panic at the walker's call site (it may
// throw a parse error from a deep peer it cannot evaluate yet, which
// is the documented partial-coverage point).

// moduleEmitResult is the per-instance "what did we emit?" record
// kept by `genCtx.memo`. PR-24 distinguished ARRef/LDRef:
//
//   - LIBRARY modules populate ARRef (the .a archive); LDRef/LDPath
//     alias to ARRef/ARPath so PROGRAM modules peering this LIBRARY
//     can wire it as a peer-archive input through the AR fields.
//   - PROGRAM modules populate LDRef (the linked binary); ARRef/ARPath
//     alias to LDRef/LDPath defensively but in practice no LIBRARY
//     peers a PROGRAM, so the ARRef of a PROGRAM is never read.
//
// `isPROGRAM` records the module-shape so the caller (`Gen`) knows
// whether to mark `LDRef` or `ARRef` as the graph result.
type moduleEmitResult struct {
	ARRef      NodeRef
	ARPath     string
	isPROGRAM  bool
	LDRef      NodeRef
	LDPath     string
	GlobalRef  *NodeRef // non-nil when the module has GLOBAL_SRCS (EmitARGlobal was called)
	GlobalPath string   // BUILD_ROOT-relative path to the .global.a archive; empty when GlobalRef is nil
}

// genCtx threads state through the recursive walk. `emit`
// accumulates every node emitted in the closure; `memo`
// deduplicates per-instance emission; `walking` is the
// cycle-detection stack. PR-23 keys both maps on `ModuleInstance`
// (D34); PR-12 keyed them on the bare path string.
type genCtx struct {
	cfg        PlatformConfig
	sourceRoot string
	emit       Emitter
	memo       map[ModuleInstance]*moduleEmitResult
	walking    map[ModuleInstance]bool
}

// whitelistedMetadataMacros is the whitelist of UnknownStmt names
// that the walker treats as no-ops (metadata only — they do not
// participate in node emission). The "real" effects (NO_LIBC etc.)
// are handled directly in `collectModule` and override the
// inferred-from-path FlagSet bag. Names that remain pure metadata
// (LICENSE, VERSION, ALLOCATOR_IMPL, ...) stay as no-ops.
// Whitelisted metadata macros (NO_BUILD effect, parser-permissive).
// Owners: PR-25 extended; new entries OK if confirmed metadata-only.
var whitelistedMetadataMacros = map[string]struct{}{
	"NO_UTIL":               {},
	"NO_LIBC":               {},
	"NO_RUNTIME":            {},
	"NO_PLATFORM":           {},
	"NO_LTO":                {},
	"NO_COMPILER_WARNINGS":  {},
	"LICENSE":               {},
	"LICENSE_TEXTS":         {},
	"WITHOUT_LICENSE_TEXTS": {},
	"VERSION":               {},
	"ORIGINAL_SOURCE":       {},
	"RECURSE":               {},
	"RECURSE_FOR_TESTS":     {},
	"ALLOCATOR_IMPL":        {},
	"NEED_CHECK":            {},
	"IDE_FOLDER":            {},
	"EXTRALIBS":             {},
	"HEADERS":               {},
	"DISABLE":               {},
	"ENABLE":                {},
	"NO_BUILD_IF":           {},
	"NO_SANITIZE":           {},
	"NO_SANITIZE_COVERAGE":  {},
	"SRC_C_NO_LTO":          {},
	"DEFAULT":               {},
	"PROVIDES":              {},
	"USE_CXX":               {},
	"DEFINE_VARIABLE":       {},
	"PYTHON3":               {},
}

// Gen produces the build graph rooted at `targetDir`. PR-23 wraps
// the call into the new ModuleInstance addressing model: the seed
// instance is constructed from `cfg.Target`, language=cpp,
// flags=inferFlagsFromPath(targetDir, false). The walker
// (`genModule`) takes the ModuleInstance directly so future host-
// tool recursion (PR-25) can fork the walker into a host instance
// without changing this entry point.
func Gen(cfg PlatformConfig, sourceRoot string, targetDir string) *Graph {
	ctx := &genCtx{
		cfg:        cfg,
		sourceRoot: sourceRoot,
		emit:       NewBufferedEmitter(),
		memo:       make(map[ModuleInstance]*moduleEmitResult),
		walking:    make(map[ModuleInstance]bool),
	}

	seed := ModuleInstance{
		Path:     filepath.Clean(targetDir),
		Language: LangCPP,
		Target:   cfg.Target.ID,
		Flags:    inferFlagsFromPath(filepath.Clean(targetDir), false),
	}

	root := genModule(ctx, seed)

	ctx.emit.Result(root.LDRef)

	return Finalize(ctx.emit.(*BufferedEmitter))
}

// moduleData is the per-module accumulator populated by
// `collectModule`. It captures everything the rule-emission stage
// needs after macro evaluation has flattened IF branches and
// inlined macros. The `flags` field starts from the path-based
// heuristic and is overlaid with macro-derived bools (NO_LIBC etc.).
type moduleData struct {
	moduleStmt  *ModuleStmt
	srcs        []string
	globalSrcs  []string
	peerdirs    []string
	joinSrcs    []*JoinSrcsStmt
	addIncl     []string // collected ADDINCL paths, all variants
	cFlags      []string // collected CFLAGS values, all variants
	cxxFlags    []string // collected CXXFLAGS values (C++ only); PR-26 wires into flag bundle
	cOnlyFlags  []string // collected CONLYFLAGS values (C only); PR-26 wires into flag bundle
	ldFlags     []string // collected LDFLAGS values
	srcDir      string   // last SRCDIR setting (empty = module dir)
	flags       FlagSet  // overlay of inferFlagsFromPath + macro bools
	conflictMod *ModuleStmt
}

// collectModule walks `mf.Stmts` (after IF branches have been
// resolved against `env`) and returns a `moduleData` with all
// macros classified. IfStmts are recursively inlined; nested
// JOIN_SRCS / SRCS / PEERDIR / NO_*  inside an IF taken branch are
// processed as if they were top-level. INCLUDE never reaches this
// point (the parser already inlined includes).
//
// The `pathFlags` argument is the path-based heuristic seed; macro
// overlays mutate it in place on the returned moduleData so the
// caller does not need to compose two separate bags.
func collectModule(modulePath string, stmts []Stmt, env map[string]bool, pathFlags FlagSet) *moduleData {
	d := &moduleData{flags: pathFlags}

	collectStmts(modulePath, stmts, env, d)

	return d
}

// collectStmts is the shared walker collectModule and IfStmt-branch
// expansion both use. It mutates `d` in place.
func collectStmts(modulePath string, stmts []Stmt, env map[string]bool, d *moduleData) {
	for _, s := range stmts {
		switch v := s.(type) {
		case *ModuleStmt:
			if d.moduleStmt != nil {
				d.conflictMod = v

				return
			}

			d.moduleStmt = v
		case *SrcsStmt:
			d.srcs = append(d.srcs, v.Sources...)
		case *PeerdirStmt:
			d.peerdirs = append(d.peerdirs, v.Paths...)
		case *SetStmt:
			// SET is parsed but PR-25 has no evaluator. The taken
			// IF branches above already flattened any conditional
			// SET; an unconditional SET that influences downstream
			// IFs would need a real macro evaluator (PR-26+).
		case *EndStmt:
			// Structural sentinel; nothing to do.
		case *JoinSrcsStmt:
			d.joinSrcs = append(d.joinSrcs, v)
		case *AddInclStmt:
			d.addIncl = append(d.addIncl, v.Paths...)
		case *CFlagsStmt:
			d.cFlags = append(d.cFlags, v.Flags...)
		case *LDFlagsStmt:
			d.ldFlags = append(d.ldFlags, v.Flags...)
		case *SrcDirStmt:
			// SRCDIR shifts source resolution base (e.g. SRCDIR(../shared) SRCS(foo.cpp) → ../shared/foo.cpp).
			// PR-25 collects but does NOT thread this into emitOneSource — tools/archiver closure
			// has no SRCDIR usages so the gap is invisible today. PR-26 wires it through.
			d.srcDir = v.Dir
		case *GlobalSrcsStmt:
			d.globalSrcs = append(d.globalSrcs, v.Sources...)
		case *IfStmt:
			taken := v.Then

			if !EvalCond(v.Cond, env) {
				taken = v.Else
			}

			collectStmts(modulePath, taken, env, d)
		case *UnknownStmt:
			applyUnknownStmt(v, d)
		default:
			ThrowFmt("gen: %s: unhandled Stmt type %T (parser added a new Stmt subclass without updating gen.go)", modulePath, s)
		}
	}
}

// applyUnknownStmt routes an UnknownStmt by name. The five flag-
// flipping macros (NO_LIBC / NO_UTIL / NO_RUNTIME / NO_PLATFORM /
// NO_COMPILER_WARNINGS) override the inferFlagsFromPath heuristic.
// Anything else must be in the metadata whitelist; an unknown name
// throws so a new ya.make macro surfaces immediately rather than
// being silently dropped (D27 discipline extended to UnknownStmts).
func applyUnknownStmt(v *UnknownStmt, d *moduleData) {
	switch v.Name {
	case "NO_LIBC":
		d.flags.NoLibc = true
	case "NO_UTIL":
		d.flags.NoUtil = true
	case "NO_RUNTIME":
		d.flags.NoRuntime = true
	case "NO_PLATFORM":
		d.flags.NoPlatform = true
	case "NO_COMPILER_WARNINGS":
		d.flags.NoCompilerWarnings = true
	case "CXXFLAGS":
		d.cxxFlags = append(d.cxxFlags, v.Args...)
	case "CONLYFLAGS":
		d.cOnlyFlags = append(d.cOnlyFlags, v.Args...)
	default:
		if _, ok := whitelistedMetadataMacros[v.Name]; !ok {
			ThrowFmt("gen: PR-25 does not yet support macro %q (extend whitelistedMetadataMacros or add a typed Stmt)", v.Name)
		}
	}
}

// buildIfEnv constructs the per-instance bound-variable environment
// for IF predicates. The base set is `DefaultIfEnv` (M2 default =
// aarch64 / linux / clang / musl). For host instances (Flags.PIC),
// flip ARCH_AARCH64↔ARCH_X86_64 so the same ya.make produces the
// other architecture's branches. The result is a fresh map; the
// caller is free to mutate it.
func buildIfEnv(instance ModuleInstance) map[string]bool {
	env := make(map[string]bool, len(DefaultIfEnv))
	for k, v := range DefaultIfEnv {
		env[k] = v
	}

	if instance.Target == PlatformDefaultLinuxX8664 {
		env["ARCH_AARCH64"] = false
		env["ARCH_X86_64"] = true
	}

	if instance.Target == PlatformDefaultLinuxAArch64 {
		env["ARCH_AARCH64"] = true
		env["ARCH_X86_64"] = false
	}

	return env
}

// derivePeerInstance constructs the peer module's ModuleInstance.
// The peer inherits the parent's Language and Target and the PIC
// axis (host-tool peers stay on host); its FlagSet is seeded from
// `inferFlagsFromPath(peerPath, parent.PIC)` and macro-overlaid by
// `genModule` itself (so the peer's flag bag reflects its own
// ya.make's NO_LIBC / NO_UTIL declarations). Macro overlay happens
// inside `genModule` because that is where the peer's ya.make is
// parsed; this helper only builds the cycle-detection key.
func derivePeerInstance(parent ModuleInstance, peerPath string) ModuleInstance {
	return ModuleInstance{
		Path:     peerPath,
		Language: parent.Language,
		Target:   parent.Target,
		Flags:    inferFlagsFromPath(peerPath, parent.Flags.PIC),
	}
}

// genModule emits the subgraph for `instance` and returns its
// `*moduleEmitResult`. Memoised: a second call for the same
// instance returns the cached result without re-emitting.
//
// Algorithm (PR-25):
//
//  1. Memo hit → return.
//  2. Cycle check: if `instance` is already on the walking stack,
//     throw.
//  3. Parse `<sourceRoot>/<instance.Path>/ya.make`.
//  4. Resolve IF branches and collect SRCS / PEERDIR / JOIN_SRCS /
//     GLOBAL_SRCS / NO_*  / ADDINCL / CFLAGS / SRCDIR via
//     `collectModule`. Apply macro-derived NO_*  flags as overrides
//     on the path-based seed FlagSet.
//  5. Validate: exactly one module, non-empty effective sources.
//  6. Recurse into each PEERDIR in declaration order (post-order —
//     peers emitted before parent) using the macro-aware
//     `genModule`.
//  7. For each source dispatch by extension to EmitCC / EmitAS /
//     EmitR6 (which itself recurses into the host ragel6 instance);
//     headers (`.h`/`.hpp`) are skipped silently.
//  8. For each JOIN_SRCS, EmitJS then EmitCC against the joined
//     output's module-relative path.
//  9. For LIBRARY: EmitAR over own CC outputs (regular srcs +
//     joined srcs); plus EmitARGlobal if GLOBAL_SRCS was non-empty.
//     For PROGRAM: EmitLD over own CC outputs and peer archives.
//
// 10. Memoise and return.
func genModule(ctx *genCtx, instance ModuleInstance) *moduleEmitResult {
	if existing, ok := ctx.memo[instance]; ok {
		return existing
	}

	if ctx.walking[instance] {
		ThrowFmt("gen: PEERDIR cycle detected involving %s", instance)
	}

	ctx.walking[instance] = true
	defer delete(ctx.walking, instance)

	yamakePath := filepath.Join(ctx.sourceRoot, instance.Path, "ya.make")
	mf := Throw2(ParseFile(yamakePath))

	env := buildIfEnv(instance)
	d := collectModule(instance.Path, mf.Stmts, env, instance.Flags)

	if d.conflictMod != nil {
		ThrowFmt("gen: %s declares multiple modules (%s and %s); only one is allowed", instance.Path, d.moduleStmt.Name, d.conflictMod.Name)
	}

	if d.moduleStmt == nil {
		ThrowFmt("gen: %s has no module declaration (PROGRAM/LIBRARY)", instance.Path)
	}

	if d.moduleStmt.Name != "LIBRARY" && d.moduleStmt.Name != "PROGRAM" {
		ThrowFmt("gen: %s declares unsupported module type %q (PR-25 accepts LIBRARY and PROGRAM only)", instance.Path, d.moduleStmt.Name)
	}

	// Update the instance's flags from macro overlay so downstream
	// emitters see the post-macro view. The instance is value-typed
	// so this rebinds locally without affecting the caller.
	instance.Flags = d.flags

	if !hasCompilableSource(d) {
		ThrowFmt("gen: %s has no compilable sources (after IF/header filter)", instance.Path)
	}

	// Recurse into peers in declaration order (R14). Each peer's
	// own ya.make is parsed for macro-derived flags inside the
	// recursive call.
	peerArchiveRefs := make([]NodeRef, 0, len(d.peerdirs))
	peerArchivePaths := make([]string, 0, len(d.peerdirs))
	peerGlobalRefs := make([]NodeRef, 0, len(d.peerdirs))
	peerGlobalPaths := make([]string, 0, len(d.peerdirs))

	for _, p := range d.peerdirs {
		peerPath := filepath.Clean(p)
		peerInstance := derivePeerInstance(instance, peerPath)
		peerResult := genModule(ctx, peerInstance)

		if peerResult.isPROGRAM {
			ThrowFmt("gen: %s peers PROGRAM module %s; only LIBRARY peers are linkable", instance.Path, peerPath)
		}

		peerArchiveRefs = append(peerArchiveRefs, peerResult.ARRef)
		peerArchivePaths = append(peerArchivePaths, peerPath+"/"+ArchiveName(peerPath))

		if peerResult.GlobalRef != nil {
			peerGlobalRefs = append(peerGlobalRefs, *peerResult.GlobalRef)
			peerGlobalPaths = append(peerGlobalPaths, peerResult.GlobalPath)
		}
	}

	// Per-source dispatch. JoinSrcs entries become JS+CC pairs
	// folded in alongside regular SRCS. Header sources (`.h` /
	// `.hpp`) are skipped. PR-25 keeps own-source ordering
	// faithful: regular SRCS in declaration order, then each
	// JOIN_SRCS's compiled output appended, then global srcs are
	// processed as a separate AR step (so they don't pollute the
	// regular `.a`).
	ccRefs := make([]NodeRef, 0, len(d.srcs)+len(d.joinSrcs))
	ccOutputs := make([]string, 0, len(d.srcs)+len(d.joinSrcs))

	for _, src := range d.srcs {
		ref, outPath, ok := emitOneSource(ctx, instance, src, d.addIncl)

		if !ok {
			continue
		}

		ccRefs = append(ccRefs, ref)
		ccOutputs = append(ccOutputs, outPath)
	}

	for _, js := range d.joinSrcs {
		_, joinOut := EmitJS(instance, js.OutputName, js.Sources, ctx.emit)

		// EmitJS returns a $(BUILD_ROOT)/<modulePath>/<name>
		// absolute path; convert to module-relative for the
		// downstream EmitCC. The relative path is just `name`
		// when modulePath is the immediate parent.
		jsRel := strings.TrimPrefix(joinOut, "$(BUILD_ROOT)/"+instance.Path+"/")

		ref, outPath := EmitCC(instance, jsRel, ctx.emit)
		ccRefs = append(ccRefs, ref)
		ccOutputs = append(ccOutputs, outPath)
	}

	// GLOBAL_SRCS get their own CC nodes and a separate AR pass
	// (see below). Filter headers here too.
	globalRefs := make([]NodeRef, 0, len(d.globalSrcs))
	globalOutputs := make([]string, 0, len(d.globalSrcs))

	for _, src := range d.globalSrcs {
		ref, outPath, ok := emitOneSource(ctx, instance, src, d.addIncl)

		if !ok {
			continue
		}

		globalRefs = append(globalRefs, ref)
		globalOutputs = append(globalOutputs, outPath)
	}

	if d.moduleStmt.Name == "PROGRAM" {
		ldRef := EmitLD(
			instance,
			ccRefs, ccOutputs,
			peerArchiveRefs, peerArchivePaths,
			nil, nil,
			peerGlobalRefs, peerGlobalPaths,
			ctx.emit,
		)
		ldPath := LDOutputPath(instance)

		result := &moduleEmitResult{
			ARRef:     ldRef,
			ARPath:    ldPath,
			isPROGRAM: true,
			LDRef:     ldRef,
			LDPath:    ldPath,
		}
		ctx.memo[instance] = result

		return result
	}

	// LIBRARY: regular AR over own CCs + peer-archive DepRefs.
	arRef := EmitAR(instance, ccRefs, ccOutputs, peerArchiveRefs, ctx.emit)
	arPath := "$(BUILD_ROOT)/" + instance.Path + "/" + ArchiveName(instance.Path)

	result := &moduleEmitResult{
		ARRef:     arRef,
		ARPath:    arPath,
		isPROGRAM: false,
		LDRef:     arRef,
		LDPath:    arPath,
	}

	if len(globalRefs) > 0 {
		globalRef := EmitARGlobal(instance, globalRefs, globalOutputs, ctx.emit)
		result.GlobalRef = &globalRef
		result.GlobalPath = instance.Path + "/" + globalArchiveName(instance.Path)
	}

	ctx.memo[instance] = result

	return result
}

// hasCompilableSource reports whether the module has at least one
// source the rule emitter would actually compile (excluding pure
// headers in SRCS, which the upstream uses as IDE / dependency-
// tracking metadata). Modules that contain only JOIN_SRCS / globals
// also count.
func hasCompilableSource(d *moduleData) bool {
	for _, s := range d.srcs {
		if !isHeaderSource(s) {
			return true
		}
	}

	if len(d.joinSrcs) > 0 {
		return true
	}

	for _, s := range d.globalSrcs {
		if !isHeaderSource(s) {
			return true
		}
	}

	return false
}

// isHeaderSource reports whether `srcRel` is a header file the
// emitter should skip.
func isHeaderSource(srcRel string) bool {
	return strings.HasSuffix(srcRel, ".h") || strings.HasSuffix(srcRel, ".hpp")
}

// emitOneSource dispatches a single source by extension. Returns
// `(ref, outputPath, true)` when a node was emitted; `(_, _, false)`
// for headers (silently skipped). Throws on unknown extensions so a
// new source kind surfaces during integration rather than being
// silently dropped.
//
// `_addIncl` is reserved for PR-26's per-module include threading;
// PR-25 collects ADDINCL into moduleData but does not yet wire it
// into EmitCC's cmd_args (the existing rule emitters carry the
// hardcoded include bundles).
func emitOneSource(ctx *genCtx, instance ModuleInstance, srcRel string, _addIncl []string) (NodeRef, string, bool) {
	if isHeaderSource(srcRel) {
		return NodeRef{}, "", false
	}

	switch {
	case strings.HasSuffix(srcRel, ".c"),
		strings.HasSuffix(srcRel, ".cpp"),
		strings.HasSuffix(srcRel, ".cc"),
		strings.HasSuffix(srcRel, ".cxx"):
		ref, outPath := EmitCC(instance, srcRel, ctx.emit)

		return ref, outPath, true
	case strings.HasSuffix(srcRel, ".S"), strings.HasSuffix(srcRel, ".s"):
		// PR-25 wires AS without a yasm host dep; the asmlib
		// closure (the only consumer of yasm) is not in PR-25's
		// synthetic test set. PR-26 will plumb yasmLD when it
		// extends the walker into asmlib.
		ref, outPath := EmitAS(instance, srcRel, nil, nil, ctx.emit)

		return ref, outPath, true
	case strings.HasSuffix(srcRel, ".rl6"):
		// Host-ragel6 recursion (D31). Construct the host instance
		// of `contrib/tools/ragel6`, walk it to completion, and
		// thread the resulting LD ref through EmitR6. Then EmitCC
		// over the generated `.cpp`.
		ragelInstance := instance.WithHost(ctx.cfg)
		ragelInstance.Path = "contrib/tools/ragel6"
		ragelInstance.Flags = inferFlagsFromPath(ragelInstance.Path, true)
		ragelResult := genModule(ctx, ragelInstance)

		_, r6Out := EmitR6(instance, srcRel, ragelResult.LDRef, ctx.emit)
		// EmitR6's output is `$(BUILD_ROOT)/<modulePath>/_/<srcRel>.cpp`.
		// EmitCC needs a module-relative source path; strip the
		// `$(BUILD_ROOT)/<modulePath>/` prefix to recover it.
		//
		// R6 emits its .cpp output into $(BUILD_ROOT)/<modulePath>/_/<srcRel>.cpp.
		// EmitCC currently composes inputPath as $(SOURCE_ROOT)/<modulePath>/<srcRel>,
		// which produces a wrong inputPath for this generated source. Mirror of the
		// JS gap documented earlier in this function (search for EmitJS). PR-26 lands
		// EmitCCFromBuildRoot variant that takes the BUILD_ROOT path AND threads the
		// generator NodeRef into DepRefs.
		ccSrcRel := strings.TrimPrefix(r6Out, "$(BUILD_ROOT)/"+instance.Path+"/")
		ccRef, ccOut := EmitCC(instance, ccSrcRel, ctx.emit)

		return ccRef, ccOut, true
	}

	ThrowFmt("gen: %s: unsupported source extension in %q", instance.Path, srcRel)

	return NodeRef{}, "", false
}
