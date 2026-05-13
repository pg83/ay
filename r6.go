package main

import "strings"

// r6.go — emitter for R6 (ragel6) generated-source nodes.
//
// PR-23 rewrites the PR-17 r6.go to take a real `ragel6LD NodeRef`
// (D31 — cross-platform recursion replaces stub-host UIDs). PR-28
// then re-routes the ragel6 LD edge from `ForeignDepRefs["tool"]` into
// `DepRefs` to match the empirical reference shape (F2 of the PR-28
// plan): the reference R6 node has `deps=[ragel6 host LD UID]` and
// `foreign_deps={tool:[<dangling internal placeholder>]}`. The
// dangling placeholder UID is unreachable in the reference graph
// itself, so we cannot reproduce it byte-exact and we omit
// `foreign_deps` entirely instead.
//
// Reference R6 node: `$(B)/util/_/datetime/parser.rl6.cpp`.
// 7 cmd_args, kv={p:R6, pc:yellow}, tags=[],
// requirements={cpu:1,network:restricted,ram:32},
// deps=[<ragel6 host LD UID>].

// canonicalRagel6BinaryPath is the reference-shaped invocation path
// for the host ragel6 binary. Reference R6 nodes invoke ragel6 at
// `$(B)/contrib/tools/ragel6/ragel6` even though our walker
// builds the binary one level deeper — at
// `$(B)/contrib/tools/ragel6/bin/ragel6` — because the
// upstream `contrib/tools/ragel6/ya.make` declares
// `INCLUDE(${ARCADIA_ROOT}/contrib/tools/ragel6/bin/ya.make)` which
// our parser does not yet expand. The semantic intent of that INCLUDE
// is "the PROGRAM lives under contrib/tools/ragel6"; we walk
// `contrib/tools/ragel6/bin` as a stopgap, and `EmitR6` rewrites the
// invocation path back to the canonical parent location so the R6
// node's cmd_args[0] matches the reference graph byte-exact (PR-35j,
// closure of PR-33-C2_07).
const (
	ragel6BinSubpath   = "$(B)/contrib/tools/ragel6/bin/"
	ragel6CanonicalDir = "$(B)/contrib/tools/ragel6/"
	// ragel6DefaultFlagOptimized / ragel6DefaultFlagDebug mirror the
	// `set_default_flags(optimized)` branch in upstream
	// `build/ymake_conf.py:2271-2277`: the release toolchain emits
	// `-CG2` (host build), the debug toolchain emits `-CT0` (target
	// build).
	ragel6DefaultFlagOptimized = "-CG2"
	ragel6DefaultFlagDebug     = "-CT0"
)

// canonicalizeRagel6BinaryPath maps the host walker's
// `$(B)/contrib/tools/ragel6/bin/<basename>` output back to
// the reference-shaped `$(B)/contrib/tools/ragel6/<basename>`.
// All other inputs pass through unchanged so the `(parse-gap →
// fallback string)` codepath in gen.go (which already supplies the
// canonical literal) and synthetic-test inputs that hand us a
// canonical-shape path are not double-rewritten.
func canonicalizeRagel6BinaryPath(p string) string {
	if !strings.HasPrefix(p, ragel6BinSubpath) {
		return p
	}

	return ragel6CanonicalDir + p[len(ragel6BinSubpath):]
}

// EmitR6 emits an R6 node generating `<srcRel>.cpp` from
// `<instance.Path>/<srcRel>` using the host ragel6 binary referenced
// by `ragel6LD` and located at `ragel6BinaryPath`.
//
// `ragel6BinaryPath` is the absolute `$(B)/...` path of the
// ragel6 binary as emitted by our own host LD walker. PR-35j applies
// `canonicalizeRagel6BinaryPath` to it: the upstream `ya.make`
// `INCLUDE`s `bin/ya.make` so the reference graph's R6 cmd_args[0]
// uses the parent path `$(B)/contrib/tools/ragel6/ragel6`
// while our walker (which doesn't expand INCLUDE yet) emits the
// host LD at `$(B)/contrib/tools/ragel6/bin/ragel6`. The
// rewrite restores byte-exact reference parity for the R6 node
// itself; the host LD node is unaffected (it remains at the walked
// `/bin/` path and continues not to pair with the reference's host
// LD, but R6 is the L3 lever for the util ragel6 closure). When the
// host walk failed (parse gap; ragel6LD is the zero NodeRef), the
// caller passes the canonical literal fallback string and the
// rewrite is a no-op.
//
// Output path: `$(B)/<instance.Path>/_/<srcRel>.cpp`. Note
// the `_/` infix matches the AS convention (D29) — generated sources
// are nested-output regardless of srcRel depth.
//
// PR-35z: `closure` is the SOURCE_ROOT-relative transitive header
// closure scanned from the `.rl6` source (same scanner pass as
// regular `.cpp`/`.S` sources; the `.rl6` body embeds `#include`
// directives that resolve through the same search-path / sysincl
// rules). Reference R6 inputs read
// `[ragel6BinaryPath, .rl6 source, ...closure]` — the binary the
// node invokes plus the `.rl6` source plus its include closure
// (1009 inputs for util/datetime/parser.rl6 in the M2 closure).
//
// `ragel6Flags` carries the per-module `SET(RAGEL6_FLAGS <value>)`
// override captured by collectStmts (PR-M3-ragel-flags-per-module).
// When the slice is empty, the platform-default fires here: `-CG2` on
// the x86_64 host (release toolchain, mirroring upstream
// `build/ymake_conf.py:2271-2277` where
// `set_default_flags(optimized=True)` appends `-CG2`) and `-CT0` on
// the aarch64 target (debug). The override replaces the default; the
// upstream `_SRC("rl6", ...)` macro at
// `build/ymake.core.conf:3284` expands `$RAGEL6_FLAGS` before
// everything else and `SET` does not concatenate. Empirical M3
// witnesses: `devtools/ymake/lang/makelists/ya.make:6` →
// `makefile_lang.rl6.cpp` cmd_args[1]=`-lF1`;
// `util/_/datetime/parser.rl6.cpp` on x86_64 →
// cmd_args[1]=`-CG2`.
//
// Returns (NodeRef, outputPath) so the caller can wire the R6 node as
// the input of a downstream EmitCC.
func EmitR6(instance ModuleInstance, srcRel string, ragel6LD NodeRef, ragel6BinaryPath string, ragel6Flags []string, closure []VFS, emit Emitter) (NodeRef, string) {
	// PR-M3-A fix: add `_/` infix only when srcRel contains a `/` (i.e.
	// the source is in a subdirectory of the module). Flat .rl6 sources
	// (no path separator) live at the module root and their generated
	// .cpp must land at `$(B)/<module>/<srcRel>.cpp` without
	// the `_/` infix. Reference: util/datetime/parser.rl6 (has `/`) →
	// `util/_/datetime/parser.rl6.cpp` ✓; include_parsers's
	// cpp_includes_parser.rl6 (flat) → `include_parsers/cpp_includes_parser.rl6.cpp`.
	var outputPath string
	if strings.Contains(srcRel, "/") {
		outputPath = "$(B)/" + instance.Path + "/_/" + srcRel + ".cpp"
	} else {
		outputPath = "$(B)/" + instance.Path + "/" + srcRel + ".cpp"
	}
	inputPath := "$(S)/" + instance.Path + "/" + srcRel
	canonicalBinary := canonicalizeRagel6BinaryPath(ragel6BinaryPath)

	// PR-M3-ragel-flags-per-module: pick the effective RAGEL6_FLAGS.
	// Module SET wins; otherwise platform-default (x86_64 → `-CG2`
	// optimized, aarch64 → `-CT0` debug). PR-M3-platform-pair-step4
	// dispatches on `instance.Platform.Target` instead of the implicit
	// "is host" check — these are platform-specific compile flags,
	// not host/target-axis flags.
	effectiveFlags := ragel6Flags
	if len(effectiveFlags) == 0 {
		switch instance.Platform.Target {
		case PlatformDefaultLinuxX8664:
			effectiveFlags = []string{ragel6DefaultFlagOptimized}
		default:
			effectiveFlags = []string{ragel6DefaultFlagDebug}
		}
	}

	cmdArgs := make([]string, 0, 5+len(effectiveFlags)+1)
	cmdArgs = append(cmdArgs, canonicalBinary)
	cmdArgs = append(cmdArgs, effectiveFlags...)
	cmdArgs = append(cmdArgs,
		"-L",
		"-I$(S)",
		"-o",
		outputPath,
		inputPath,
	)

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
	}

	// PR-35z: inputs = [ragel6 binary, .rl6 source, ...transitive
	// header closure]. The binary entry mirrors the reference shape
	// (every reference R6 node lists `$(B)/contrib/tools/
	// ragel6/ragel6` as inputs[0]); the closure carries every header
	// the .rl6 transitively `#include`s, in DFS-discovery order.
	inputs := make([]VFS, 0, 2+len(closure))
	inputs = append(inputs, ParseVFSOrSource(canonicalBinary), ParseVFSOrSource(inputPath))
	inputs = append(inputs, closure...)

	// PR-M3-platform-pair-step4: tags + host_platform are baseline data
	// from `targetP`. Empty `instance.Platform.Tags` keeps the slice non-nil so
	// the JSON serialises as `[]`, not `null`.
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
		Outputs:      []VFS{ParseVFSOrSource(outputPath)},
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
		// PR-L4-C/07: wire ragel6LD into both DepRefs (for the L0 topology
		// fingerprint, which reads only deps) and ForeignDepRefs["tool"]
		// (matching REF's foreign_deps shape for the R6 aarch64 node).
		DepRefs:        []NodeRef{ragel6LD},
		ForeignDepRefs: map[string][]NodeRef{"tool": {ragel6LD}},
	}

	return emit.Emit(node), outputPath
}
