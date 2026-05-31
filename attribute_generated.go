package main

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
	if e == nil || len(e.generatedFirstClaim) == 0 {
		return
	}

	for _, node := range e.nodes {
		kind, _ := node.KV["p"].(string)

		switch kind {
		case "PR", "CF", "CP":
		default:
			continue
		}

		if len(node.Outputs) == 0 {
			continue
		}

		current := node.TargetProperties["module_dir"]
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

		if node.TargetProperties == nil {
			node.TargetProperties = map[string]string{}
		}

		node.TargetProperties["module_dir"] = claim
	}
}
