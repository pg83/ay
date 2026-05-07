package main

import (
	"fmt"
	"reflect"
	"sort"
)

// compare_props.go — L1 and L2 per-pair property comparison.
//
// L0 (compare_topology.go) answers a multiset question: do the two
// graphs share the same Merkle fingerprint distribution? It does not
// pair individual nodes. L1 and L2 need a pairing so per-node fields
// can be diff'd.
//
// Pairing strategy: keyed by (outputs[0], platform). Rationale:
//
//   - The brief originally specified "pair by outputs[0]" on the
//     assumption that the primary output path is unique across the
//     graph. Empirically that is *almost* true on the reference
//     /home/pg/monorepo/yatool_orig/sg.json (3,719 unique outputs[0]
//     values out of 3,730 nodes), but 11 outputs collide, each
//     produced by exactly two nodes — one per build platform
//     (default-linux-aarch64 vs default-linux-x86_64). Pairing on
//     outputs[0] alone would leave those 22 nodes unpaired and force
//     the mandated real-graph self-match acceptance test below 100%.
//   - Adding `platform` to the key restores full uniqueness on the
//     real graph (3,730 unique pairs). Platform is exactly the field
//     ymake uses to disambiguate "same artifact, different toolchain"
//     and is the natural extension of the brief's intent. It is NOT
//     itself one of the compared fields at L1 or L2 — it is purely a
//     pairing discriminator.
//
// Defensive behaviour: a node with empty outputs[] cannot be paired
// (none observed in the reference graph). Such nodes are reported via
// wantOnly/gotOnly and count against the L1/L2 percentage like any
// other unpaired node.
//
// Denominator: max(len(want.Graph), len(got.Graph)) — the same as L0.
// Counting matched pairs against the larger side guarantees that an
// extra or missing node drops L1/L2 below 1.0 even when every paired
// node matches.

// pairingKey identifies a node for cross-graph pairing. See file
// header for why platform is included.
type pairingKey struct {
	output   string
	platform string
}

// pairByOutput builds a map[wantUID]gotUID by primary-output pairing
// key. Nodes whose key does not appear on the other side are reported
// in wantOnly / gotOnly. Nodes with empty Outputs are unpairable and
// land in the corresponding *Only slice.
//
// Returned slices are sorted by UID for deterministic output (D14).
func pairByOutput(want, got *Graph) (pairs map[string]string, wantOnly, gotOnly []string) {
	wantByKey := indexByPairingKey(want, "want")
	gotByKey := indexByPairingKey(got, "got")

	pairs = make(map[string]string, len(wantByKey))
	wantOnly = []string{}
	gotOnly = []string{}

	for _, n := range want.Graph {
		if len(n.Outputs) == 0 {
			wantOnly = append(wantOnly, n.UID)

			continue
		}

		key := pairingKey{output: n.Outputs[0], platform: n.Platform}
		if gotUID, ok := gotByKey[key]; ok {
			pairs[n.UID] = gotUID
		} else {
			wantOnly = append(wantOnly, n.UID)
		}
	}

	for _, n := range got.Graph {
		if len(n.Outputs) == 0 {
			gotOnly = append(gotOnly, n.UID)

			continue
		}

		key := pairingKey{output: n.Outputs[0], platform: n.Platform}
		if _, ok := wantByKey[key]; !ok {
			gotOnly = append(gotOnly, n.UID)
		}
	}

	sort.Strings(wantOnly)
	sort.Strings(gotOnly)

	return pairs, wantOnly, gotOnly
}

// indexByPairingKey builds a key→UID map and throws on collision.
// Collisions on the chosen key would silently lose nodes, so we
// surface them as an internal error rather than guessing which side
// of the duplicate to keep — see PR-04's defensive UID-uniqueness
// check in topologyFingerprints for the same discipline.
func indexByPairingKey(g *Graph, label string) map[pairingKey]string {
	out := make(map[pairingKey]string, len(g.Graph))

	for _, n := range g.Graph {
		if len(n.Outputs) == 0 {
			continue
		}

		key := pairingKey{output: n.Outputs[0], platform: n.Platform}
		if existing, dup := out[key]; dup {
			ThrowFmt("compare: %s graph has duplicate (outputs[0], platform) key (output=%q platform=%q) at UIDs %q and %q", label, key.output, key.platform, existing, n.UID)
		}

		out[key] = n.UID
	}

	return out
}

// compareL1 walks the pairing and counts pairs whose kv["p"],
// target_properties, and outputs (full slice, ordered) all match. The
// denominator is max(len(want.Graph), len(got.Graph)) so unpaired
// nodes count against the percentage just like at L0.
//
// Returns the percentage and a one-line note summarising matched pairs,
// total pairs built, and unpaired nodes on either side.
func compareL1(want, got *Graph, pairs map[string]string, wantOnly, gotOnly []string) (l1 float64, note string) {
	wantByUID := indexByUID(want)
	gotByUID := indexByUID(got)

	matched := 0

	// Iteration over `pairs` for COUNTING is fine (D14: only output
	// strings need sorted iteration; this is a pure count).
	for wantUID, gotUID := range pairs {
		if l1Match(wantByUID[wantUID], gotByUID[gotUID]) {
			matched++
		}
	}

	denom := max(len(want.Graph), len(got.Graph))

	if denom == 0 {
		return 1.0, "0 matched / 0 pairs / 0 unpaired-want / 0 unpaired-got"
	}

	l1 = float64(matched) / float64(denom)
	note = fmt.Sprintf("%d matched / %d pairs / %d unpaired-want / %d unpaired-got", matched, len(pairs), len(wantOnly), len(gotOnly))

	return l1, note
}

// compareL2 walks the pairing and counts pairs whose inputs, tags,
// and requirements all match. Same denominator and structure as
// compareL1.
func compareL2(want, got *Graph, pairs map[string]string, wantOnly, gotOnly []string) (l2 float64, note string) {
	wantByUID := indexByUID(want)
	gotByUID := indexByUID(got)

	matched := 0

	for wantUID, gotUID := range pairs {
		if l2Match(wantByUID[wantUID], gotByUID[gotUID]) {
			matched++
		}
	}

	denom := max(len(want.Graph), len(got.Graph))

	if denom == 0 {
		return 1.0, "0 matched / 0 pairs / 0 unpaired-want / 0 unpaired-got"
	}

	l2 = float64(matched) / float64(denom)
	note = fmt.Sprintf("%d matched / %d pairs / %d unpaired-want / %d unpaired-got", matched, len(pairs), len(wantOnly), len(gotOnly))

	return l2, note
}

// l1Match returns true iff want and got agree on every L1 field:
// kv["p"], target_properties (full map), outputs (ordered slice).
// Outputs ARE ordered in g.json (alphabetical, per ymake convention),
// so order-sensitive comparison here is correct: a permutation would
// indicate genuine drift, not a comparator bug.
func l1Match(want, got *Node) bool {
	if want.KV["p"] != got.KV["p"] {
		return false
	}

	if !reflect.DeepEqual(want.TargetProperties, got.TargetProperties) {
		return false
	}

	if !stringSliceEqual(want.Outputs, got.Outputs) {
		return false
	}

	return true
}

// l2Match returns true iff want and got agree on every L2 field:
// inputs (multiset), tags (ordered slice), requirements (full map).
//
// Inputs are compared as a MULTISET (PR-31 D14): the upstream
// ymake scanner emits inputs in BFS-discovery order, but matching
// that order byte-for-byte requires replicating ymake's exact
// search-path precedence + per-record sysincl-resolution sequencing,
// which is M5+ polish. Inputs-as-multiset captures the SET of
// transitive-include resolutions the scanner produced regardless of
// which traversal order discovered them. Tags remain ordered
// because the per-node tag list is short (0-3 entries) and
// declaration-order is the upstream invariant we DO match.
func l2Match(want, got *Node) bool {
	if !stringMultisetEqual(want.Inputs, got.Inputs) {
		return false
	}

	if !stringSliceEqual(want.Tags, got.Tags) {
		return false
	}

	if !reflect.DeepEqual(want.Requirements, got.Requirements) {
		return false
	}

	return true
}

// stringMultisetEqual returns true when a and b contain the same
// elements with the same multiplicities, ignoring order. Used for
// L2 input comparison (PR-31 D14).
func stringMultisetEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	counts := make(map[string]int, len(a))

	for _, s := range a {
		counts[s]++
	}

	for _, s := range b {
		counts[s]--

		if counts[s] < 0 {
			return false
		}
	}

	for _, c := range counts {
		if c != 0 {
			return false
		}
	}

	return true
}

// indexByUID returns a UID→*Node map. Throws on duplicate UIDs (same
// defensive posture as compare_topology.go's byUID build).
func indexByUID(g *Graph) map[string]*Node {
	out := make(map[string]*Node, len(g.Graph))

	for _, n := range g.Graph {
		if _, dup := out[n.UID]; dup {
			ThrowFmt("compare: graph has duplicate UID %q", n.UID)
		}

		out[n.UID] = n
	}

	return out
}

// stringSliceEqual is a small inlineable equality test that avoids
// the reflect.DeepEqual overhead on the hot path (3,730 pairs × 3
// slice fields per level adds up). nil and empty are treated as
// equal — g.json never serialises nil here, but defensive.
func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}
