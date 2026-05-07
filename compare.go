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
//   - L1/L2/L3 (PR-05/PR-06): added later. Stubs in CompareReport leave
//     space for them.
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
// and an optional one-line diagnostic note. PR-04 ships only the L0
// fields; PR-05/PR-06 add L1/L2/L3 alongside.
//
// Skipped lists levels that were requested by the caller but are not
// yet implemented. PR-04 fills it whenever maxLevel > 0.
type CompareReport struct {
	L0      float64 // topology match (modulo UID renumbering); 1.0 == identical multisets
	L0Note  string  // human-readable summary, e.g. "3729 of 3730 fingerprints matched"
	Skipped []int   // levels requested but not yet implemented
}

// highestImplementedLevel is the largest level value Compare currently
// computes. Bumped by PR-05 (to 1 or 2) and PR-06 (to 3).
const highestImplementedLevel = 0

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

	for lvl := highestImplementedLevel + 1; lvl <= maxLevel; lvl++ {
		report.Skipped = append(report.Skipped, lvl)
	}

	return report
}
