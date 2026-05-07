package main

import (
	"path/filepath"
	"strings"
)

// gen.go — top-level "parse a ya.make and emit its build subgraph"
// driver, generalised in PR-12 from a single-leaf LIBRARY emitter to a
// recursive PEERDIR walker.
//
// PR-12 scope (intentionally narrow; PR-13/PR-14/.../PR-22 widen it):
//
//   - Modules accepted at the top of `ya.make`: `LIBRARY()` and
//     `PROGRAM()`. PROGRAM is treated structurally identically to
//     LIBRARY for now — the module's archive is closed by `EmitAR`
//     regardless. PR-19 swaps in a real `EmitLD` for PROGRAM and the
//     `isPROGRAM` flag stored on the per-module result is the gate.
//   - Macros accepted: SRCS, PEERDIR, END, SET, plus a fixed whitelist
//     of metadata/no-op macros surfaced by the parser as
//     `*UnknownStmt` (NO_UTIL, NO_LIBC, NO_RUNTIME, NO_PLATFORM,
//     NO_LTO, NO_COMPILER_WARNINGS, LICENSE, LICENSE_TEXTS,
//     WITHOUT_LICENSE_TEXTS, VERSION, ORIGINAL_SOURCE, RECURSE,
//     RECURSE_FOR_TESTS, ALLOCATOR_IMPL, NEED_CHECK, IDE_FOLDER,
//     EXTRALIBS).
//   - Anything else (including IF, INCLUDE, JOIN_SRCS, ADDINCL,
//     CFLAGS, LDFLAGS, SRCDIR, GLOBAL_SRCS) throws
//     `gen: PR-12 does not yet support macro %q (deferred to PR-13)`.
//
// Walk discipline:
//
//   - Depth-first, post-order: a module's PEERDIRs are emitted before
//     the module itself, so when EmitAR runs, every peer's archive
//     UID is already known and can be passed in as a DepRef.
//   - Memoised by `targetDir`: a module that appears more than once
//     in the closure is emitted exactly once. The shared
//     `*moduleEmitResult` carries its archive ref/path for downstream
//     parents to reuse.
//   - Cycle-detected via a `walking` set: revisiting a directory that
//     is currently on the stack throws.
//
// AR-with-peers wiring caveat: when a module has PEERDIRs, the AR
// node's `DepRefs` and `inputs` include both its own .o files AND
// each peer's archive UID/path. PR-15 will refine the cmd_args shape
// (real ymake AR commands list peer libs explicitly, not as inputs);
// PR-12 only guarantees the dependency wiring is correct, not that
// the cmd_args is byte-exact for parent modules. The single-module
// M1 leaf (`build/cow/on`) has zero PEERDIRs, so the byte-exact
// regression is preserved.
//
// AR-for-PROGRAM caveat: PR-12 emits an AR node for PROGRAM modules
// too, as a placeholder. PR-19 replaces this with EmitLD; the
// `LDRef`/`LDPath` fields on `moduleEmitResult` (today aliased to
// `ARRef`/`ARPath`) will then diverge from the AR ones. Marking
// `Result(root.LDRef)` is correct as-is and won't change in PR-19.

// moduleEmitResult is the per-module "what did we emit?" record kept
// by `genCtx.memo`. Splitting AR vs LD even though they coincide
// today makes the PR-19 swap mechanical: replacing the LD fields
// with EmitLD's outputs is a one-line change in `genModule` and zero
// changes elsewhere.
type moduleEmitResult struct {
	ARRef     NodeRef
	ARPath    string
	isPROGRAM bool
	LDRef     NodeRef
	LDPath    string
}

// genCtx threads state through the recursive walk. `emit` accumulates
// every node emitted in the closure; `memo` deduplicates per-module
// emission; `walking` is the cycle-detection stack.
type genCtx struct {
	cfg        PlatformConfig
	sourceRoot string
	emit       Emitter
	memo       map[string]*moduleEmitResult
	walking    map[string]bool
}

// pr12SupportedUnknownMacros is the whitelist of UnknownStmt names
// that PR-12 treats as no-ops. Anything else throws. Kept as a
// package-level set so the lookup is O(1) and the brief's enumerated
// list is easy to audit/update.
var pr12SupportedUnknownMacros = map[string]struct{}{
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
}

// archivePathOf computes the archive path for a module under PR-12's
// simplified naming convention: `lib<dashed-modulePath>.a` rooted at
// `$(BUILD_ROOT)/<targetDir>/`. Mirrors `EmitAR`'s formula at
// ar.go:31. PR-15 will replace this with the real depth-aware
// function (NEW R10 in m2-plan §3); for PR-12 the naive form is
// sufficient because the single byte-exact M1 module (`build/cow/on`)
// happens to use exactly this shape.
func archivePathOf(targetDir string) string {
	return "$(BUILD_ROOT)/" + targetDir + "/lib" + strings.ReplaceAll(targetDir, "/", "-") + ".a"
}

// Gen produces the build graph rooted at `targetDir` (a module-relative
// path like "build/cow/on" or "tools/archiver"). Throws on parse
// error, unsupported macro, PEERDIR cycle, or emitter misuse.
//
// The function bootstraps a `genCtx`, recurses into the target via
// `genModule`, marks the resulting module's LD ref as the graph
// result (today equal to its AR ref; PR-19 separates them), and
// returns the finalised graph.
func Gen(cfg PlatformConfig, sourceRoot string, targetDir string) *Graph {
	ctx := &genCtx{
		cfg:        cfg,
		sourceRoot: sourceRoot,
		emit:       NewBufferedEmitter(),
		memo:       make(map[string]*moduleEmitResult),
		walking:    make(map[string]bool),
	}

	root := genModule(ctx, targetDir)

	ctx.emit.Result(root.LDRef)

	return Finalize(ctx.emit.(*BufferedEmitter))
}

// genModule emits the subgraph for `targetDir` and returns its
// `*moduleEmitResult`. Memoised: a second call for the same
// `targetDir` returns the cached result without re-emitting.
//
// Algorithm:
//
//  1. Memo hit → return.
//  2. Cycle check: if `targetDir` is already on the walking stack,
//     throw.
//  3. Parse `<sourceRoot>/<targetDir>/ya.make`.
//  4. Walk top-level statements in source order, collecting
//     module/srcs/peerdirs. Reject unsupported macros.
//  5. Validate: exactly one module, non-empty srcs.
//  6. Recurse into each PEERDIR in declaration order
//     (post-order — peers emitted before parent). The walk order
//     determines link order (R14): preserve as-declared, do NOT
//     sort.
//  7. Emit one CC node per source (declaration order).
//  8. Emit one AR node closing the module, with both own-CC refs
//     and peer-archive refs threaded as DepRefs.
//  9. Memoise and return.
func genModule(ctx *genCtx, targetDir string) *moduleEmitResult {
	targetDir = filepath.Clean(targetDir)

	if existing, ok := ctx.memo[targetDir]; ok {
		return existing
	}

	if ctx.walking[targetDir] {
		ThrowFmt("gen: PEERDIR cycle detected involving %s", targetDir)
	}

	ctx.walking[targetDir] = true
	defer delete(ctx.walking, targetDir)

	yamakePath := filepath.Join(ctx.sourceRoot, targetDir, "ya.make")
	mf := Throw2(ParseFile(yamakePath))

	var (
		moduleStmt *ModuleStmt
		srcs       []string
		peerdirs   []string
	)

	for _, s := range mf.Stmts {
		switch v := s.(type) {
		case *ModuleStmt:
			if moduleStmt != nil {
				ThrowFmt("gen: %s declares multiple modules (%s and %s); only one is allowed", targetDir, moduleStmt.Name, v.Name)
			}

			moduleStmt = v
		case *SrcsStmt:
			srcs = append(srcs, v.Sources...)
		case *PeerdirStmt:
			peerdirs = append(peerdirs, v.Paths...)
		case *SetStmt:
			// SET is parsed but PR-12 has no evaluator; the value
			// never feeds emitted output. Tolerate silently —
			// PR-13's macro evaluator will start respecting it.
		case *EndStmt:
			// Structural sentinel; nothing to do.
		case *UnknownStmt:
			if _, ok := pr12SupportedUnknownMacros[v.Name]; !ok {
				ThrowFmt("gen: PR-12 does not yet support macro %q (deferred to PR-13)", v.Name)
			}
		default:
			ThrowFmt("gen: PR-12: unhandled Stmt type %T (parser added a new Stmt subclass without updating gen.go)", s)
		}
	}

	if moduleStmt == nil {
		ThrowFmt("gen: %s has no module declaration (PROGRAM/LIBRARY)", targetDir)
	}

	if moduleStmt.Name != "LIBRARY" && moduleStmt.Name != "PROGRAM" {
		ThrowFmt("gen: %s declares unsupported module type %q (PR-12 accepts LIBRARY and PROGRAM only)", targetDir, moduleStmt.Name)
	}

	if len(srcs) == 0 {
		ThrowFmt("gen: %s has no SRCS; PR-12 requires at least one source", targetDir)
	}

	// Recurse into peers in declaration order (R14 — link order
	// is non-alphabetical, follows ymake's post-order walk).
	peerArchiveRefs := make([]NodeRef, 0, len(peerdirs))
	peerArchivePaths := make([]string, 0, len(peerdirs))

	for _, p := range peerdirs {
		peerResult := genModule(ctx, p)
		peerArchiveRefs = append(peerArchiveRefs, peerResult.ARRef)
		peerArchivePaths = append(peerArchivePaths, peerResult.ARPath)
	}

	// Emit CC nodes in source declaration order. EmitCC returns the
	// output path so we don't re-derive it here (PR-10-D03).
	ccRefs := make([]NodeRef, 0, len(srcs))
	ccOutputs := make([]string, 0, len(srcs))

	for _, src := range srcs {
		ref, outPath := EmitCC(ctx.cfg, targetDir, src, ctx.emit)
		ccRefs = append(ccRefs, ref)
		ccOutputs = append(ccOutputs, outPath)
	}

	// Compose AR's DepRefs: own CC refs FIRST, then peer archive
	// refs (so the M1 leaf — zero peers — sees an unchanged
	// argument list and the byte-exact regression holds). For
	// modules with peers, the AR's cmd_args/inputs include peer
	// archives in DepRef order; PR-15 will refine that shape.
	arDepRefs := append([]NodeRef{}, ccRefs...)
	arDepRefs = append(arDepRefs, peerArchiveRefs...)
	arDepPaths := append([]string{}, ccOutputs...)
	arDepPaths = append(arDepPaths, peerArchivePaths...)

	arRef := EmitAR(ctx.cfg.Name, targetDir, arDepRefs, arDepPaths, ctx.emit)
	arPath := archivePathOf(targetDir)

	result := &moduleEmitResult{
		ARRef:     arRef,
		ARPath:    arPath,
		isPROGRAM: moduleStmt.Name == "PROGRAM",
		// PR-12: LD ref/path alias to AR. PR-19 will set these
		// from EmitLD for PROGRAM modules; LIBRARY modules will
		// continue to alias.
		LDRef:  arRef,
		LDPath: arPath,
	}
	ctx.memo[targetDir] = result

	return result
}
