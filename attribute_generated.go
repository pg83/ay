package main

import "strings"

// overrideGeneratedModuleDir mirrors upstream ymake's Node2Module attribution
// (devtools/ymake/json_visitor.cpp:638-645): when a generated file is first
// encountered by a CC compile's include-scan, the CONSUMER module — not the
// RUN_PROGRAM/COPY producer — gets recorded as its module_dir. We collect
// first-claimer module dirs during the scan pass (scanner.go:
// generatedFirstClaim) and apply them here, after the emitter has all
// producer nodes and before finalize computes content hashes.
//
// Producer nodes we override: KV.p ∈ {"PR", "CF", "CP"} — RUN_PROGRAM,
// CONFIGURE_FILE, COPY_FILE. Their outputs are exactly the entries the
// scanner sees during CC include resolution. Other node kinds keep their
// emit-time module_dir.
//
// Conservative rule: only overwrite when the claim points at a DIFFERENT
// module than the producer. If the first-claimer is the producer itself
// (common — many internal codegen passes have no external consumer in the
// build closure), the producer-time attribution already matches REF.
func overrideGeneratedModuleDir(e *BufferedEmitter) {
	if e == nil {
		return
	}

	overrideENSubmoduleModuleDir(e)

	if len(e.generatedFirstClaim) == 0 && len(e.generatedNodeClaim) == 0 {
		return
	}

	for i, node := range e.nodes {
		kind := node.KV.P

		switch kind {
		case pkPR, pkCF, pkCP:
		default:
			continue
		}

		if len(node.Outputs) == 0 {
			continue
		}

		current := node.TargetProperties.ModuleDir

		// A self-owned producer — its own module compiles a sibling output, so
		// markGeneratedProducerOwned recorded generatedFirstClaim[out]==current at
		// registration — is the first DFS-leaver of its outputs (its module is a
		// PEERDIR dependency of any module that merely OUTPUT_INCLUDES the output,
		// hence fully visited first). Upstream's Node2Module first-leave-wins keeps
		// it attributed to its producer; a node-level OUTPUT_INCLUDES claim from an
		// external consumer must NOT pre-empt it (yabs/server/libs/constant/generated
		// sys_const.h, named in the parent constant module's OUTPUT_INCLUDES).
		selfOwned := false

		for _, out := range node.Outputs {
			if e.generatedFirstClaim[out] == current {
				selfOwned = true

				break
			}
		}

		// A node-level OUTPUT_INCLUDES claim (the structural consumer that names this
		// producer's output) is authoritative for a NON-self-owned producer: it
		// attributes the whole node at once, matching upstream's single Node2Module
		// entry. It wins over the per-output generatedFirstClaim consensus, which an
		// incidental far peer's include-resolve of one sibling output would otherwise
		// split into a no-op conflict.
		if claim := e.generatedNodeClaim[NodeRef(i)]; claim != "" && !selfOwned {
			if claim != current {
				node.TargetProperties.ModuleDir = claim
			}

			continue
		}

		var claim string

		for _, out := range node.Outputs {
			c, ok := e.generatedFirstClaim[out]

			if !ok {
				continue
			}

			if claim == "" {
				claim = c

				continue
			}

			if c != claim {
				claim = ""

				break
			}
		}

		if claim == "" || claim == current {
			continue
		}

		node.TargetProperties.ModuleDir = claim
	}
}

// overrideENSubmoduleModuleDir reproduces ymake's Node2Module directory-ownership
// for generated serialized-enum (EN) nodes. A generated *_serialized.h declared
// by module D but #included through a NESTED submodule's directory-owned header
// is attributed to that submodule: the nested peerdir submodule leaves the
// generated node before its enclosing parent D completes (submodule-before-parent
// DFS post-order), so FindModule on the visitor stack returns the submodule.
//
// The discriminator vs the consumer-claim path (PR/CF/CP, OwnerModuleDir) is the
// directory ownership of the INCLUDER, not the compiling module: the includer's
// nearest enclosing module must be a real module strictly nested under D. An EN
// header reached only through a non-module subdir of D (no nested ya.make), or
// through an unrelated module not nested under D (the yabs/server/libs/enums
// family), keeps its declaring owner.
func overrideENSubmoduleModuleDir(e *BufferedEmitter) {
	if len(e.generatedENIncluderDirs) == 0 {
		return
	}

	// Every distinct module_dir is a real module directory (generated nodes carry
	// their attributed module's dir), so the set of module_dir values across all
	// nodes is exactly the set of module directories — the basis for resolving an
	// includer directory to its nearest enclosing module.
	moduleDirs := make(map[string]struct{}, len(e.nodes))

	for _, node := range e.nodes {
		if md := node.TargetProperties.ModuleDir; md != "" {
			moduleDirs[md] = struct{}{}
		}
	}

	nearestModuleDir := func(dir string) string {
		for d := dir; d != ""; d = pathDir(d) {
			if _, ok := moduleDirs[d]; ok {
				return d
			}
		}

		return ""
	}

	for _, node := range e.nodes {
		if node.KV.P != pkEN || len(node.Outputs) == 0 {
			continue
		}

		declaring := node.TargetProperties.ModuleDir
		prefix := declaring + "/"

		var best string

		for _, out := range node.Outputs {
			for _, incDir := range e.generatedENIncluderDirs[out] {
				m := nearestModuleDir(incDir)

				// Only a real module strictly nested under the declaring module
				// pre-empts D in post-order. Among several, the deepest wins (the
				// most-nested submodule leaves first).
				if m == "" || !strings.HasPrefix(m, prefix) {
					continue
				}

				if len(m) > len(best) {
					best = m
				}
			}
		}

		if best != "" {
			node.TargetProperties.ModuleDir = best
		}
	}
}
