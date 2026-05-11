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
// Reference R6 node: `$(BUILD_ROOT)/util/_/datetime/parser.rl6.cpp`.
// 7 cmd_args, kv={p:R6, pc:yellow}, tags=[],
// requirements={cpu:1,network:restricted,ram:32},
// deps=[<ragel6 host LD UID>].

// canonicalRagel6BinaryPath is the reference-shaped invocation path
// for the host ragel6 binary. Reference R6 nodes invoke ragel6 at
// `$(BUILD_ROOT)/contrib/tools/ragel6/ragel6` even though our walker
// builds the binary one level deeper — at
// `$(BUILD_ROOT)/contrib/tools/ragel6/bin/ragel6` — because the
// upstream `contrib/tools/ragel6/ya.make` declares
// `INCLUDE(${ARCADIA_ROOT}/contrib/tools/ragel6/bin/ya.make)` which
// our parser does not yet expand. The semantic intent of that INCLUDE
// is "the PROGRAM lives under contrib/tools/ragel6"; we walk
// `contrib/tools/ragel6/bin` as a stopgap, and `EmitR6` rewrites the
// invocation path back to the canonical parent location so the R6
// node's cmd_args[0] matches the reference graph byte-exact (PR-35j,
// closure of PR-33-C2_07).
const (
	ragel6BinSubpath   = "$(BUILD_ROOT)/contrib/tools/ragel6/bin/"
	ragel6CanonicalDir = "$(BUILD_ROOT)/contrib/tools/ragel6/"
)

// canonicalizeRagel6BinaryPath maps the host walker's
// `$(BUILD_ROOT)/contrib/tools/ragel6/bin/<basename>` output back to
// the reference-shaped `$(BUILD_ROOT)/contrib/tools/ragel6/<basename>`.
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
// `ragel6BinaryPath` is the absolute `$(BUILD_ROOT)/...` path of the
// ragel6 binary as emitted by our own host LD walker. PR-35j applies
// `canonicalizeRagel6BinaryPath` to it: the upstream `ya.make`
// `INCLUDE`s `bin/ya.make` so the reference graph's R6 cmd_args[0]
// uses the parent path `$(BUILD_ROOT)/contrib/tools/ragel6/ragel6`
// while our walker (which doesn't expand INCLUDE yet) emits the
// host LD at `$(BUILD_ROOT)/contrib/tools/ragel6/bin/ragel6`. The
// rewrite restores byte-exact reference parity for the R6 node
// itself; the host LD node is unaffected (it remains at the walked
// `/bin/` path and continues not to pair with the reference's host
// LD, but R6 is the L3 lever for the util ragel6 closure). When the
// host walk failed (parse gap; ragel6LD is the zero NodeRef), the
// caller passes the canonical literal fallback string and the
// rewrite is a no-op.
//
// Output path: `$(BUILD_ROOT)/<instance.Path>/_/<srcRel>.cpp`. Note
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
// Returns (NodeRef, outputPath) so the caller can wire the R6 node as
// the input of a downstream EmitCC.
func EmitR6(instance ModuleInstance, srcRel string, ragel6LD NodeRef, ragel6BinaryPath string, closure []string, emit Emitter) (NodeRef, string) {
	outputPath := "$(BUILD_ROOT)/" + instance.Path + "/_/" + srcRel + ".cpp"
	inputPath := "$(SOURCE_ROOT)/" + instance.Path + "/" + srcRel
	canonicalBinary := canonicalizeRagel6BinaryPath(ragel6BinaryPath)

	cmdArgs := []string{
		canonicalBinary,
		"-CT0",
		"-L",
		"-I$(SOURCE_ROOT)",
		"-o",
		outputPath,
		inputPath,
	}

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)",
	}

	// PR-35z: inputs = [ragel6 binary, .rl6 source, ...transitive
	// header closure]. The binary entry mirrors the reference shape
	// (every reference R6 node lists `$(BUILD_ROOT)/contrib/tools/
	// ragel6/ragel6` as inputs[0]); the closure carries every header
	// the .rl6 transitively `#include`s, in DFS-discovery order.
	inputs := make([]string, 0, 2+len(closure))
	inputs = append(inputs, canonicalBinary, inputPath)
	inputs = append(inputs, closure...)

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
			},
		},
		Env:     env,
		Inputs:  inputs,
		Outputs: []string{outputPath},
		KV: map[string]string{
			"p":  "R6",
			"pc": "yellow",
		},
		Tags: []string{},
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		Platform: string(instance.Target),
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
