package main

import (
	"path/filepath"
)

// gen.go — top-level "parse a ya.make and emit its build subgraph"
// driver for M1's vertical slice.
//
// Scope is intentionally narrow: PR-10 supports exactly one shape of
// module — a `LIBRARY()` with a non-empty `SRCS(...)` and zero
// `PEERDIR(...)` entries — because the only M1 leaf the loop pins
// byte-exact is `build/cow/on`. Everything else throws a concrete error
// rather than silently producing a wrong subgraph.
//
// The walk over `mf.Stmts` is positional but tolerant: unknown or
// unmodelled statements (NO_UTIL, NO_LIBC, NO_RUNTIME, etc.) are
// permitted and ignored — those are the macros that PR-08's hardcoded
// flag bundles already assume. When a future PR generalises the rule
// emitter, this function will need to gate flag bundles on those
// macros instead of swallowing them.
//
// Path composition for the .o output mirrors EmitCC's formula
// (`$(BUILD_ROOT)/<moduleDir>/<basename(srcRel)>.o`); EmitAR consumes
// those paths. If EmitCC's output path ever changes, this function
// must change in lockstep — the reviewer is expected to flag any
// drift.

// Gen produces the build graph for `targetDir` (a module-relative path
// like "build/cow/on") on the given platform config and returns the
// finalized *Graph. Throws on parse error, unsupported module shape,
// or emitter misuse.
//
// PR-10 supports only `LIBRARY()` modules with non-empty `SRCS()` and
// zero `PEERDIR()`. The parser is permissive (other ya.make files
// will parse cleanly) but Gen rejects unsupported shapes with a
// concrete error so future generalisations are obvious failures, not
// silent drift.
func Gen(cfg PlatformConfig, sourceRoot string, targetDir string) *Graph {
	yamakePath := filepath.Join(sourceRoot, targetDir, "ya.make")
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
		}
	}

	if moduleStmt == nil {
		ThrowFmt("gen: %s has no module declaration (PROGRAM/LIBRARY)", targetDir)
	}

	if moduleStmt.Name != "LIBRARY" {
		ThrowFmt("gen: PR-10 only supports LIBRARY modules; %s declares %s", targetDir, moduleStmt.Name)
	}

	if len(peerdirs) > 0 {
		ThrowFmt("gen: PR-10 does not support PEERDIR yet; %s has %d entries", targetDir, len(peerdirs))
	}

	if len(srcs) == 0 {
		ThrowFmt("gen: %s has no SRCS; PR-10 requires at least one source", targetDir)
	}

	emit := NewBufferedEmitter()

	ccRefs := make([]NodeRef, 0, len(srcs))
	ccOutputs := make([]string, 0, len(srcs))

	for _, src := range srcs {
		ref := EmitCC(cfg, targetDir, src, emit)
		// Output-path formula MUST stay in sync with cc.go's `outputPath`.
		// Both compose `$(BUILD_ROOT)/<moduleDir>/<basename(srcRel)>.o`.
		outPath := "$(BUILD_ROOT)/" + targetDir + "/" + filepath.Base(src) + ".o"
		ccRefs = append(ccRefs, ref)
		ccOutputs = append(ccOutputs, outPath)
	}

	arRef := EmitAR(cfg.Name, targetDir, ccRefs, ccOutputs, emit)
	emit.Result(arRef)

	return Finalize(emit)
}
