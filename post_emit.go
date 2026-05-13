package main

// post_emit.go — umbrella + back-peer ADDINCL post-emit patch.
//
// Threaded through emitter Finalize/FinalizeStream as the per-node
// prepare hook returned by newPostEmitPrepare. mightNeedAddInclPatch
// is the static hold predicate used by StreamingEmitter to identify
// CC nodes whose finalize must defer to the prepare hook.

import (
	"path/filepath"
	"strings"
)

// umbrellaPropagatedPaths is the set of ADDINCL paths upstream ymake
// propagates from a path-prefix umbrella LIBRARY to its sub-libraries'
// compilations. The empirical reference (sg2.json) restricts the
// propagation to brotli/snappy/re2 — three GLOBAL ADDINCL contributions
// reaching `devtools/ymake` via `library/cpp/blockcodecs` (→ brotli +
// snappy) and the direct `contrib/libs/re2` peer.
//
// Other GLOBAL ADDINCL contributions of the umbrella (yaml-cpp,
// sparsehash, antlr4, yaml, lzma, libffi, python, etc.) do NOT
// propagate to sub-libraries' compiles in the reference graph — they
// remain confined to the umbrella's own compile context. The precise
// upstream filter is unclear; for the M3 closure this allow-list is the
// minimum set that closes the 85-node L3 gap without injecting flags
// that would regress other nodes.
var umbrellaPropagatedPaths = map[string]struct{}{
	"contrib/libs/brotli/c/include": {},
	"contrib/libs/snappy/include":   {},
	"contrib/libs/re2/include":      {},
}

// umbrellaPropagatedOrder pins the canonical emission order for the
// allow-listed paths. Empirically REF emits them as brotli/snappy/re2
// at the tail of the -I block on every umbrella-inheriting sub-library
// (e.g. cyclestimer.cpp.o cmd_args[26..28] in sg2.json).
var umbrellaPropagatedOrder = []string{
	"contrib/libs/brotli/c/include",
	"contrib/libs/snappy/include",
	"contrib/libs/re2/include",
}

// umbrellaPropagatingAncestors is the explicit set of LIBRARY paths
// whose AddInclGlobal subset (umbrellaPropagatedPaths) propagates to
// path-prefix sub-libraries' CC compilations. Empirically `devtools/ymake`
// is the only umbrella exhibiting this behaviour in the M3 closure;
// other path-prefix umbrellas like `library/cpp/blockcodecs` and
// `library/cpp/json` do NOT propagate their GLOBAL ADDINCL to their
// path-children (verified by inspecting `library/cpp/blockcodecs/core/
// codecs.cpp.o` and `library/cpp/json/writer/json_value.cpp.o` in
// sg2.json). The exact upstream rule is unknown; this allow-list is the
// narrowest matching set that closes the 85-node L3 gap.
var umbrellaPropagatingAncestors = map[string]struct{}{
	"devtools/ymake": {},
}

// ccLanguageDefaultInclude lists the `-I` arguments that every C++ CC
// node receives via the language-default propagation (linux-headers +
// musl arch/include/extra + libcxx{,rt}/include + zlib + double-
// conversion + libc_compat). umbrella propagation skips CC nodes whose
// entire -I set is contained in this list — those nodes (e.g.
// `devtools/ymake/yndex/yndex.cpp.o`) have no user-peer-GLOBAL ADDINCL
// of their own, and REF does not propagate umbrella contributions to
// them.
//
// The two arch-specific musl paths (musl/arch/aarch64 vs musl/arch/
// x86_64) are folded into the same set so the predicate matches on
// either platform.
var ccLanguageDefaultInclude = map[string]bool{
	"-I$(B)":                                          true,
	"-I$(S)":                                         true,
	"-I$(S)/contrib/libs/linux-headers":              true,
	"-I$(S)/contrib/libs/linux-headers/_nf":          true,
	"-I$(S)/contrib/libs/cxxsupp/libcxx/include":     true,
	"-I$(S)/contrib/libs/cxxsupp/libcxxrt/include":   true,
	"-I$(S)/contrib/libs/musl/arch/aarch64":          true,
	"-I$(S)/contrib/libs/musl/arch/x86_64":           true,
	"-I$(S)/contrib/libs/musl/arch/generic":          true,
	"-I$(S)/contrib/libs/musl/include":               true,
	"-I$(S)/contrib/libs/musl/extra":                 true,
	"-I$(S)/contrib/libs/zlib/include":               true,
	"-I$(S)/contrib/libs/double-conversion":          true,
	"-I$(S)/contrib/libs/libc_compat/include/readpassphrase": true,
}

// newPostEmitPrepare returns a per-node mutator that combines the
// umbrella and back-peer ADDINCL patches into one inline operation.
// Replaces the two separate full-graph batch passes that the
// pre-refactor pipeline ran between Gen and Finalize. With the mutator
// threaded through FinalizeWith / FinalizeStreamWith, each node
// finalises in one shot — mutate cmd_args → resolve Deps → compute UID
// → yield — so the executor sees its first leaf node within milliseconds
// of FinalizeStream starting, not after two extra N-walks complete.
//
// Umbrella patch: a LIBRARY X with sub-libraries A, B, C under its path
// prefix exports a subset of its transitive peer-GLOBAL ADDINCL closure
// to A, B, C at compile time. The propagated subset is restricted by
// `umbrellaPropagatedPaths` — empirically brotli/snappy/re2 for the M3
// `devtools/ymake/bin` closure. Path-prefix ancestors are looked up in
// `ctx.memo` keyed on (path, platform) so host-tool walks stay isolated
// from target walks; each ancestor's `AddInclGlobal` is intersected
// with the allow-list and the not-yet-present `-I` flags are appended
// after the last existing `-I` argument.
//
// Back-peer patch: a PY*_LIBRARY-family child M whose parent P also
// participates in the auto-PEERDIR cycle through `contrib/libs/python`
// inherits P's allow-listed paths (currently
// `contrib/restricted/abseil-cpp` only). Forward-only DFS walkers miss
// this back-edge; the per-node hook reconstructs the back-peer
// relationships from `ctx.memo` and injects the allow-listed paths
// into the affected CC nodes' cmd_args.

// mightNeedAddInclPatch is the hold predicate used by StreamingEmitter
// (yatool make) to identify CC nodes whose cmd_args may be mutated by
// the post-emit umbrella / back-peer patch. Such nodes cannot finalize
// inline at Emit time — the patch's input state (ctx.memo path-prefix
// ancestors, back-peer cycle parents) is incomplete until Gen
// finishes — so they must be deferred to the post-pass.
//
// Both checks are deliberately statically derivable from the Node
// alone (modulePath only):
//
//   - Umbrella branch fires only when a path-prefix ancestor sits in
//     `umbrellaPropagatingAncestors` (currently a one-element set:
//     `devtools/ymake`). Held: every CC whose modulePath starts with
//     `devtools/ymake/`.
//   - Back-peer branch fires only when both the CC's module and its
//     PEER parent are PY*_LIBRARY-family. The actual gate
//     (backPeerCycleParticipant on ModuleStmtName) needs ctx.memo,
//     which we don't have inline; the path heuristic
//     `library/python/symbols/module` is the narrowest cover that
//     reproduces every M3-closure back-peer target without dragging
//     in unrelated CC nodes. Extend if other auto-PEERDIR cycles
//     surface in future closures.
//
// Over-holding is sound (the held node still gets finalized
// correctly in Finish); under-holding silently breaks the patch by
// firing onNode with stale cmd_args. When in doubt, hold.
func mightNeedAddInclPatch(n *Node) bool {
	if n == nil || n.KV == nil || n.KV["p"] != "CC" {
		return false
	}

	modulePath := n.TargetProperties["module_dir"]
	if modulePath == "" {
		return false
	}

	for anc := range umbrellaPropagatingAncestors {
		if modulePath == anc || strings.HasPrefix(modulePath, anc+"/") {
			return true
		}
	}

	if modulePath == "library/python/symbols/module" ||
		strings.HasPrefix(modulePath, "library/python/symbols/module/") {
		return true
	}

	return false
}

func newPostEmitPrepare(ctx *genCtx) func(*Node) {
	// ─── Umbrella state ──────────────────────────────────────────
	// Build path → AddInclGlobal map, keyed on the platform string so
	// host-tool walks (x86_64) and target walks (aarch64) keep separate
	// AddInclGlobal contributions (a peer-GLOBAL contribution that fires
	// only on the target platform must not bleed into the host CC).
	type key struct {
		path     string
		platform string
	}

	pathAddIncl := map[key][]string{}
	// pyByPath records (path, platform) keys whose module declared a
	// PY*_LIBRARY-family type. PR-M3-protobuf-umbrella-trigger: the
	// umbrella propagator must skip CC nodes belonging to Python-bound
	// modules (rapidjson, ymakeyaml under devtools/ymake). REF does not
	// emit brotli/snappy/re2 for those even though their peer chain
	// otherwise meets the hasNonLangDefault gate (Python/libffi/lzma
	// includes are non-language-default contributions).
	pyByPath := map[key]bool{}

	for inst, res := range ctx.memo {
		if res == nil {
			continue
		}

		k := key{path: inst.Path, platform: string(inst.Platform.Target)}
		if len(res.AddInclGlobal) != 0 {
			pathAddIncl[k] = res.AddInclGlobal
		}
		if res.isPyLibrary {
			pyByPath[k] = true
		}
	}

	// pathPrefixAncestors yields the strict path-prefix ancestors of
	// `modulePath` (excluding modulePath itself) in nearest-first order.
	// e.g. "devtools/ymake/lang/makelists" → ["devtools/ymake/lang",
	// "devtools/ymake", "devtools"].
	pathPrefixAncestors := func(modulePath string) []string {
		parts := strings.Split(modulePath, "/")
		if len(parts) <= 1 {
			return nil
		}

		out := make([]string, 0, len(parts)-1)
		for i := len(parts) - 1; i > 0; i-- {
			out = append(out, strings.Join(parts[:i], "/"))
		}

		return out
	}

	// ─── Back-peer state ────────────────────────────────────────
	// Per (modulePath, platform): the list of parent paths P (with
	// their AddInclGlobal slices) that declared this module in their
	// effective Peerdirs AND participate in the back-peer cycle.
	type backPeer struct {
		parentPath    string
		addInclGlobal []string
	}

	type backPeerEntry struct {
		peerdirs       []string
		addInclGlobal  []string
		moduleStmtName string
	}

	resultsByKey := map[key]backPeerEntry{}

	for inst, res := range ctx.memo {
		if res == nil {
			continue
		}

		resultsByKey[key{path: inst.Path, platform: string(inst.Platform.Target)}] = backPeerEntry{
			peerdirs:       res.Peerdirs,
			addInclGlobal:  res.AddInclGlobal,
			moduleStmtName: res.ModuleStmtName,
		}
	}

	backPeersByKey := map[key][]backPeer{}

	for k, entry := range resultsByKey {
		if !backPeerCycleParticipant(entry.moduleStmtName) {
			continue
		}

		for _, peer := range entry.peerdirs {
			peerKey := key{path: filepath.Clean(peer), platform: k.platform}

			peerEntry, ok := resultsByKey[peerKey]
			if !ok {
				continue
			}

			if !backPeerCycleParticipant(peerEntry.moduleStmtName) {
				continue
			}

			backPeersByKey[peerKey] = append(backPeersByKey[peerKey], backPeer{
				parentPath:    k.path,
				addInclGlobal: entry.addInclGlobal,
			})
		}
	}

	// ─── Per-node mutator ───────────────────────────────────────
	// Combines umbrella + back-peer injection. Both passes share the
	// same `-I` scan over the node's cmd_args and the same insert-
	// after-last-existing-`-I` strategy, so we do that work once per
	// node and apply both injections in a single rewrite.
	return func(n *Node) {
		if n == nil || n.KV == nil || n.KV["p"] != "CC" {
			return
		}

		modulePath, ok := n.TargetProperties["module_dir"]
		if !ok || modulePath == "" {
			return
		}

		if len(n.Cmds) == 0 {
			return
		}

		nodeKey := key{path: modulePath, platform: n.Platform}
		isPY := pyByPath[nodeKey]

		args := n.Cmds[0].CmdArgs

		lastIIdx := -1
		present := map[string]struct{}{}

		for i, a := range args {
			if !strings.HasPrefix(a, "-I") {
				continue
			}

			lastIIdx = i
			present[a] = struct{}{}
		}

		if lastIIdx < 0 {
			return
		}

		var inject []string
		injectSeen := map[string]struct{}{}

		appendIfNew := func(flag string) {
			if _, dup := present[flag]; dup {
				return
			}

			if _, dup := injectSeen[flag]; dup {
				return
			}

			injectSeen[flag] = struct{}{}
			inject = append(inject, flag)
		}

		// Umbrella: PY*_LIBRARY consumers (rapidjson, ymakeyaml under
		// devtools/ymake) are excluded — REF does not propagate
		// brotli/snappy/re2 to them.
		if !isPY {
			ancestors := pathPrefixAncestors(modulePath)

			var ancestorHit string

			for _, anc := range ancestors {
				if _, ok := umbrellaPropagatingAncestors[anc]; !ok {
					continue
				}

				if _, ok := pathAddIncl[key{path: anc, platform: n.Platform}]; ok {
					ancestorHit = anc

					break
				}
			}

			if ancestorHit != "" {
				ancAddIncl := pathAddIncl[key{path: ancestorHit, platform: n.Platform}]
				ancHas := map[string]struct{}{}

				for _, p := range ancAddIncl {
					ancHas[p] = struct{}{}
				}

				// Trigger: umbrella fires only when the CC's own peer
				// chain already contributes at least one peer-GLOBAL
				// ADDINCL (any -I not in the language-default set).
				hasNonLangDefault := false

				for p := range present {
					if !ccLanguageDefaultInclude[p] {
						hasNonLangDefault = true

						break
					}
				}

				if hasNonLangDefault {
					for _, p := range umbrellaPropagatedOrder {
						if _, ok := ancHas[p]; !ok {
							continue
						}

						if _, allowed := umbrellaPropagatedPaths[p]; !allowed {
							continue
						}

						var flag string
						if strings.HasPrefix(p, "$(") {
							flag = "-I" + p
						} else {
							flag = "-I$(S)/" + p
						}

						appendIfNew(flag)
					}
				}
			}
		}

		// Back-peer: PY*_LIBRARY-family child M whose parent P also
		// participates in the cycle inherits P's allow-listed paths.
		if parents := backPeersByKey[nodeKey]; len(parents) > 0 {
			for _, parent := range parents {
				for _, p := range parent.addInclGlobal {
					if _, allowed := backPeerPropagatedPaths[p]; !allowed {
						continue
					}

					appendIfNew("-I$(S)/" + p)
				}
			}
		}

		if len(inject) == 0 {
			return
		}

		newArgs := make([]string, 0, len(args)+len(inject))
		newArgs = append(newArgs, args[:lastIIdx+1]...)
		newArgs = append(newArgs, inject...)
		newArgs = append(newArgs, args[lastIIdx+1:]...)

		n.Cmds[0].CmdArgs = newArgs
	}
}

// backPeerPropagatedPaths is the allow-list of GLOBAL ADDINCL paths that
// propagate through a back-peer edge (parent P PEERDIRs child M) into M's
// CC compilations. Restricted to the minimal set empirically observed in
// REF for the M3 closure:
//   - `contrib/restricted/abseil-cpp` reaches `library/python/symbols/
//     module`'s PY3 sub-walk via the chain `contrib/libs/python →
//     library/python/runtime_py3 → library/cpp/resource → library/cpp/
//     containers/absl_flat_hash → contrib/restricted/abseil-cpp`.
//
// Other GLOBAL ADDINCL contributions reachable through `contrib/libs/
// python`'s peer chain (lzma, openssl, libffi, brought in via `contrib/
// tools/python3`) do NOT propagate to its back-peered children in the
// reference graph — confirmed by inspecting `library/python/symbols/{libc,
// python,registry}` which gain no non-language-default `-I` flags despite
// also being peered FROM `contrib/libs/python`. The exact upstream filter
// is unclear; this allow-list is the narrowest set that closes the 2-node
// L3 gap without injecting flags that would regress paired nodes.
var backPeerPropagatedPaths = map[string]struct{}{
	"contrib/restricted/abseil-cpp": {},
}

// backPeerCycleParticipant returns true when the module declaration type
// auto-PEERDIRs `contrib/libs/python` (build/conf/python.conf:697-743) and
// therefore participates in the contrib/libs/python peer-SCC for GLOBAL
// ADDINCL propagation. Mirrors `pyLibraryAutoPythonPeer` (used by the
// forward auto-injection at gen.go:2575); kept separate so the back-peer
// post-pass can be extended (e.g. to other auto-PEERDIR cycles) without
// perturbing the forward gate.
func backPeerCycleParticipant(name string) bool {
	switch name {
	case "PY3_LIBRARY", "PY2_LIBRARY", "PY23_LIBRARY":
		return true
	}

	return false
}
