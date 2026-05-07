package main

// compare.go — driver for the multi-level graph comparator.
//
// The comparator answers "how close is the candidate graph to the
// reference graph?" at a sequence of progressively stricter levels:
//
//   - L0 (PR-04, this file's siblings): topology — does the dependency
//     DAG have the same shape, modulo UID renumbering? Implemented by
//     compare_topology.go via Merkle-style fingerprints over (kv.p,
//     sorted child fingerprints).
//   - L1 (PR-05, compare_props.go): per-pair match on kv["p"],
//     target_properties, outputs. Pairing is keyed by
//     (outputs[0], platform) — see compare_props.go for the rationale.
//   - L2 (PR-05, compare_props.go): per-pair match on inputs, tags,
//     requirements. Reuses the same pairing as L1.
//   - L3 (PR-06, compare_cmd.go): per-pair byte-exact match on cmds
//     (cmd_args + per-cmd env, ordered) and top-level node env.
//     Reuses the same pairing as L1/L2.
//
// The L0 result is a percentage, not a yes/no. A value of 1.0 means
// the multisets of fingerprints are identical; anything below 1.0
// counts mismatched entries on the larger side. The comparator is
// observational — it never returns a non-zero exit code on its own. A
// future --strict flag (out of scope for PR-04) may flip that.
//
// Internal errors (nil graph, cycles in input, etc.) throw via the
// exception machinery in throw.go. Each level's mismatch is data in
// the report, not an exception.

// CompareReport is the result of comparing two graphs at one or more
// levels. Each implemented level reports a percentage in [0.0, 1.0]
// and an optional one-line diagnostic note. PR-04 shipped L0; PR-05
// added L1 and L2; PR-06 adds L3.
//
// Per-level fields are populated only when the caller's maxLevel
// reached that level — e.g. Compare(_, _, 0) leaves L1/L2/L3 at zero
// values, with L1Note/L2Note/L3Note empty. Callers that need to know
// which levels actually ran should consult Skipped (levels requested
// but not yet implemented) and the maxLevel they passed in.
type CompareReport struct {
	L0       float64  // topology match (modulo UID renumbering); 1.0 == identical multisets
	L0Note   string   // human-readable summary, e.g. "3729 of 3730 fingerprints matched"
	L1       float64  // per-pair match on kv.p, target_properties, outputs (PR-05)
	L1Note   string   // human-readable summary, e.g. "3729 of 3730 pairs match"
	L2       float64  // per-pair match on inputs, tags, requirements (PR-05)
	L2Note   string   // human-readable summary
	L3       float64  // per-pair byte-exact match on cmds + top-level env (PR-06)
	L3Note   string   // human-readable summary
	WantOnly []string // UIDs in want that have no pair in got (sorted)
	GotOnly  []string // UIDs in got that have no pair in want (sorted)
	Skipped  []int    // levels requested but not yet implemented
}

// highestImplementedLevel is the largest level value Compare currently
// computes. PR-05 bumped it to 2; PR-06 bumps it to 3.
const highestImplementedLevel = 3

// Compare runs the requested levels (currently only level 0; later
// levels are added in PR-05/PR-06) on two *Graph instances and returns
// the report. Throws on internal errors (nil graph, cycle in input,
// etc.); each level's mismatch is reported as a percentage in the
// report rather than raised.
//
// maxLevel is the inclusive upper bound on which levels to compute.
// Levels above highestImplementedLevel are recorded in Skipped instead
// of being silently ignored.
func Compare(want, got *Graph, maxLevel int) *CompareReport {
	if want == nil {
		ThrowFmt("compare: want graph is nil")
	}

	if got == nil {
		ThrowFmt("compare: got graph is nil")
	}

	if maxLevel < 0 {
		ThrowFmt("compare: maxLevel must be >= 0, got %d", maxLevel)
	}

	report := &CompareReport{}

	l0, note := compareTopology(want, got)
	report.L0 = l0
	report.L0Note = note

	// L1, L2 and L3 share the same pairing (computed once). Building
	// it is O(N) over both graphs and dominates none of the per-level
	// work, but doing it three times would still be wasteful and
	// would risk drift if the pairing logic ever evolved between
	// calls.
	if maxLevel >= 1 {
		pairs, wantOnly, gotOnly := pairByOutput(want, got)
		report.WantOnly = wantOnly
		report.GotOnly = gotOnly

		l1, l1Note := compareL1(want, got, pairs, wantOnly, gotOnly)
		report.L1 = l1
		report.L1Note = l1Note

		if maxLevel >= 2 {
			l2, l2Note := compareL2(want, got, pairs, wantOnly, gotOnly)
			report.L2 = l2
			report.L2Note = l2Note
		}

		if maxLevel >= 3 {
			l3, l3Note := compareL3(want, got, pairs, wantOnly, gotOnly)
			report.L3 = l3
			report.L3Note = l3Note
		}
	}

	for lvl := highestImplementedLevel + 1; lvl <= maxLevel; lvl++ {
		report.Skipped = append(report.Skipped, lvl)
	}

	return report
}
