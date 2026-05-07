package main

import (
	"path/filepath"
)

// gen.go — top-level "parse a ya.make and emit its build subgraph"
// driver. PR-23 retrofitted memo keys / cycle keys / rule call sites
// from `string` to `ModuleInstance` (D34) and threaded
// `ModuleInstance` through every emitter call. The walker shape is
// otherwise unchanged from PR-12: depth-first, post-order recursion
// driven by PEERDIR, declaration-order traversal so R14 link order
// is preserved.
//
// Scope discipline (PR-23):
//
//   - Modules accepted: LIBRARY() and PROGRAM(). PROGRAM is treated
//     structurally identically to LIBRARY for now — closed by EmitAR.
//     PR-24 swaps in EmitLD for PROGRAM modules.
//   - Macros accepted: SRCS, PEERDIR, END, SET, plus the
//     pr12SupportedUnknownMacros whitelist of metadata/no-op macros.
//   - PR-13's typed Stmts (IF / INCLUDE / JOIN_SRCS / ADDINCL /
//     CFLAGS / LDFLAGS / SRCDIR / GLOBAL_SRCS) are PARSED but NOT
//     YET PROCESSED by gen.go. PR-25 wires the macro evaluator.
//     They throw via the default *Stmt arm with an
//     "unhandled Stmt type" message so reviewers see exactly which
//     kind tripped the walker.
//
// Walk discipline:
//
//   - Depth-first, post-order: peers emitted before parents.
//   - Memoised by `ModuleInstance` (D34): two distinct instances
//     of the same path (e.g. host vs target build/cow/on)
//     memoise separately; both emit.
//   - Cycle detection per-instance: revisiting an instance that
//     is currently on the stack throws.
//
// PR-23 acceptance scope: Gen(DefaultLinuxConfig, root, "build/cow/on")
// must still emit 2 nodes (1 CC + 1 AR) byte-exact against the
// reference target subgraph for `build/cow/on`. Host-tool recursion
// (D31) is wired in PR-25 via the macro evaluator (which gates host
// instance creation on detecting tool-style PEERDIRs and module
// attributes).

// moduleEmitResult is the per-instance "what did we emit?" record
// kept by `genCtx.memo`. Splitting AR vs LD even though they
// coincide today makes the PR-24 swap mechanical: replacing the LD
// fields with EmitLD's outputs is a one-line change in `genModule`
// and zero changes elsewhere.
type moduleEmitResult struct {
	ARRef     NodeRef
	ARPath    string
	isPROGRAM bool
	LDRef     NodeRef
	LDPath    string
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

// pr12SupportedUnknownMacros is the whitelist of UnknownStmt names
// that the walker treats as no-ops. Anything else throws. Kept as
// a package-level set so the lookup is O(1); PR-25's macro
// evaluator will start respecting these (e.g. NO_LIBC will toggle
// FlagSet.NoLibc on the inferred instance).
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

// genModule emits the subgraph for `instance` and returns its
// `*moduleEmitResult`. Memoised: a second call for the same
// instance returns the cached result without re-emitting.
//
// Algorithm:
//
//  1. Memo hit → return.
//  2. Cycle check: if `instance` is already on the walking stack,
//     throw.
//  3. Parse `<sourceRoot>/<instance.Path>/ya.make`.
//  4. Walk top-level statements in source order, collecting
//     module/srcs/peerdirs. Reject unsupported macros.
//  5. Validate: exactly one module, non-empty srcs.
//  6. Recurse into each PEERDIR in declaration order
//     (post-order — peers emitted before parent). Peer instance
//     inherits parent.Target and parent.Flags.PIC; flag bag is
//     derived per-path via inferFlagsFromPath.
//  7. Emit one CC node per source (declaration order).
//  8. Emit one AR node closing the module.
//  9. Memoise and return.
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

	var (
		moduleStmt *ModuleStmt
		srcs       []string
		peerdirs   []string
	)

	for _, s := range mf.Stmts {
		switch v := s.(type) {
		case *ModuleStmt:
			if moduleStmt != nil {
				ThrowFmt("gen: %s declares multiple modules (%s and %s); only one is allowed", instance.Path, moduleStmt.Name, v.Name)
			}

			moduleStmt = v
		case *SrcsStmt:
			srcs = append(srcs, v.Sources...)
		case *PeerdirStmt:
			peerdirs = append(peerdirs, v.Paths...)
		case *SetStmt:
			// SET is parsed but PR-23 has no evaluator. PR-25's
			// macro evaluator will start respecting it.
		case *EndStmt:
			// Structural sentinel; nothing to do.
		case *UnknownStmt:
			if _, ok := pr12SupportedUnknownMacros[v.Name]; !ok {
				ThrowFmt("gen: PR-23 does not yet support macro %q (deferred to PR-25)", v.Name)
			}
		default:
			ThrowFmt("gen: PR-23: unhandled Stmt type %T (parser added a new Stmt subclass without updating gen.go)", s)
		}
	}

	if moduleStmt == nil {
		ThrowFmt("gen: %s has no module declaration (PROGRAM/LIBRARY)", instance.Path)
	}

	if moduleStmt.Name != "LIBRARY" && moduleStmt.Name != "PROGRAM" {
		ThrowFmt("gen: %s declares unsupported module type %q (PR-23 accepts LIBRARY and PROGRAM only)", instance.Path, moduleStmt.Name)
	}

	if len(srcs) == 0 {
		ThrowFmt("gen: %s has no SRCS; PR-23 requires at least one source", instance.Path)
	}

	// Recurse into peers in declaration order (R14 — link order
	// is non-alphabetical, follows ymake's post-order walk).
	// Peer instance inherits the parent's platform and PIC axis;
	// the per-path flag derivation runs against the peer's own
	// path (so e.g. a musl peer gets its own NoLibc flags).
	peerArchiveRefs := make([]NodeRef, 0, len(peerdirs))

	for _, p := range peerdirs {
		peerInstance := ModuleInstance{
			Path:     filepath.Clean(p),
			Language: instance.Language,
			Target:   instance.Target,
			Flags:    inferFlagsFromPath(filepath.Clean(p), instance.Flags.PIC),
		}
		peerResult := genModule(ctx, peerInstance)
		peerArchiveRefs = append(peerArchiveRefs, peerResult.ARRef)
	}

	// Emit CC nodes in source declaration order. EmitCC returns
	// the output path so we don't re-derive it here (PR-10-D03).
	ccRefs := make([]NodeRef, 0, len(srcs))
	ccOutputs := make([]string, 0, len(srcs))

	for _, src := range srcs {
		ref, outPath := EmitCC(instance, src, ctx.emit)
		ccRefs = append(ccRefs, ref)
		ccOutputs = append(ccOutputs, outPath)
	}

	// Emit AR closing the module. Peer archives are wired as
	// DepRefs (NOT cmd_args inputs) so the UID flow accounts for
	// PEERDIR linkage; ar(1) only sees own .o files.
	arRef := EmitAR(instance, ccRefs, ccOutputs, peerArchiveRefs, ctx.emit)
	arPath := "$(BUILD_ROOT)/" + instance.Path + "/" + ArchiveName(instance.Path)

	result := &moduleEmitResult{
		ARRef:     arRef,
		ARPath:    arPath,
		isPROGRAM: moduleStmt.Name == "PROGRAM",
		// PR-23: LD ref/path alias to AR. PR-24 will set these
		// from EmitLD for PROGRAM modules; LIBRARY modules will
		// continue to alias.
		LDRef:  arRef,
		LDPath: arPath,
	}
	ctx.memo[instance] = result

	return result
}
