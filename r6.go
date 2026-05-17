package main

import "strings"

// r6.go — emitter for R6 (ragel6) generated-source nodes.
//
// Reference R6 node carries `deps=[ragel6 host LD UID]` and
// `foreign_deps={tool:[…placeholder…]}`. The placeholder UID is unreachable
// in the reference graph itself; we wire the same ragel6LD into both
// DepRefs and ForeignDepRefs["tool"] to satisfy the topology fingerprint.

// Reference R6 nodes invoke ragel6 at `$(B)/contrib/tools/ragel6/ragel6`,
// while our walker builds it at `$(B)/contrib/tools/ragel6/bin/ragel6`
// because the upstream INCLUDE() of `bin/ya.make` is not yet expanded.
// EmitR6 rewrites the invocation path back to the canonical parent location
// so cmd_args[0] matches the reference byte-exact.
const (
	ragel6BinSubrel    = "contrib/tools/ragel6/bin/"
	ragel6CanonicalRel = "contrib/tools/ragel6/"
	// Mirror `set_default_flags(optimized)` in upstream
	// build/ymake_conf.py:2271-2277: release → -CG2, debug → -CT0.
	ragel6DefaultFlagOptimized = "-CG2"
	ragel6DefaultFlagDebug     = "-CT0"
)

// canonicalizeRagel6Binary maps `$(B)/contrib/tools/ragel6/bin/<base>`
// back to `$(B)/contrib/tools/ragel6/<base>`. All other inputs pass through
// unchanged so canonical-shape paths (fallback literal, synthetic tests)
// are not double-rewritten.
func canonicalizeRagel6Binary(v VFS) VFS {
	if !v.IsBuild() || !strings.HasPrefix(v.Rel, ragel6BinSubrel) {
		return v
	}

	return Build(ragel6CanonicalRel + v.Rel[len(ragel6BinSubrel):])
}

// EmitR6 emits an R6 node generating `<srcRel>.cpp` from
// `<instance.Path>/<srcRel>` via the host ragel6 binary referenced by
// ragel6LD at ragel6BinaryPath. canonicalizeRagel6BinaryPath rewrites the
// walker's `/bin/` path back to the reference's canonical parent path; a
// canonical fallback string passes through as a no-op.
//
// Output path: `$(B)/<instance.Path>/_/<srcRel>.cpp` when srcRel has a `/`,
// else `$(B)/<instance.Path>/<srcRel>.cpp`. closure is the SOURCE_ROOT-
// relative transitive header closure scanned from the .rl6 source; inputs
// read [ragel6Binary, .rl6 source, ...closure].
//
// ragel6Flags carries the per-module `SET(RAGEL6_FLAGS …)` override; empty
// → platform default (release and unsanitized → -CG2, otherwise -CT0). SET does
// not concatenate; upstream `_SRC("rl6", …)` in build/ymake.core.conf:3284
// expands $RAGEL6_FLAGS first.
//
// Returns (NodeRef, outputPath) so the caller can wire R6 as the input of
// a downstream EmitCC.
func EmitR6(instance ModuleInstance, srcRel string, ragel6LD NodeRef, ragel6BinaryPath VFS, ragel6Flags []string, closure []VFS, emit Emitter) (NodeRef, VFS) {
	// Add `_/` infix only when srcRel contains a `/` (subdir source). Flat
	// .rl6 at module root must land at `$(B)/<module>/<srcRel>.cpp` without
	// the infix. Reference: util/datetime/parser.rl6 → `util/_/datetime/
	// parser.rl6.cpp`; flat cpp_includes_parser.rl6 →
	// `include_parsers/cpp_includes_parser.rl6.cpp`.
	var outVFS VFS
	if strings.Contains(srcRel, "/") {
		outVFS = Build(instance.Path + "/_/" + srcRel + ".cpp")
	} else {
		outVFS = Build(instance.Path + "/" + srcRel + ".cpp")
	}
	inVFS := Source(instance.Path + "/" + srcRel)
	canonicalBinary := canonicalizeRagel6Binary(ragel6BinaryPath)

	// Effective RAGEL6_FLAGS: module SET wins; else platform default
	// follows ymake_conf.py's Ragel.configure_toolchain:
	// build.is_release && !build.is_sanitized -> -CG2, else -CT0.
	effectiveFlags := ragel6Flags
	if len(effectiveFlags) == 0 {
		if instance.Platform.Ragel6Optimized {
			effectiveFlags = []string{ragel6DefaultFlagOptimized}
		} else {
			effectiveFlags = []string{ragel6DefaultFlagDebug}
		}
	}

	cmdArgs := make([]string, 0, 5+len(effectiveFlags)+1)
	cmdArgs = append(cmdArgs, canonicalBinary.String())
	cmdArgs = append(cmdArgs, effectiveFlags...)
	cmdArgs = append(cmdArgs,
		"-L",
		"-I$(S)",
		"-o",
		outVFS.String(),
		inVFS.String(),
	)

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
	}

	// inputs = [ragel6 binary, .rl6 source, ...transitive header closure].
	// Binary entry matches the reference shape (inputs[0] = canonical
	// ragel6 path); closure is in DFS-discovery order.
	inputs := make([]VFS, 0, 2+len(closure))
	inputs = append(inputs, canonicalBinary, inVFS)
	inputs = append(inputs, closure...)

	// tags + host_platform come from instance.Platform. Empty
	// instance.Platform.Tags stays non-nil so JSON renders `[]` not `null`.
	tags := instance.Platform.Tags
	hostPlatform := instance.Platform.IsHost

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
			},
		},
		Env:          env,
		Inputs:       inputs,
		Outputs:      []VFS{outVFS},
		HostPlatform: hostPlatform,
		KV: map[string]string{
			"p":  "R6",
			"pc": "yellow",
		},
		Tags: tags,
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		Platform: string(instance.Platform.Target),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		// Wire ragel6LD into both DepRefs (for the L0 topology fingerprint,
		// which reads only deps) and ForeignDepRefs["tool"] (matching REF's
		// foreign_deps shape for the R6 aarch64 node).
		DepRefs:        []NodeRef{ragel6LD},
		ForeignDepRefs: map[string][]NodeRef{"tool": {ragel6LD}},
	}

	return emit.Emit(node), outVFS
}
