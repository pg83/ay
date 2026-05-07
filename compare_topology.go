package main

import (
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"sort"
)

// compare_topology.go — L0 topology comparison.
//
// L0 answers: do the two graphs have the same dependency-DAG shape and
// the same per-node "kind" (kv.p), modulo UID renumbering? Two graphs
// produced by independent runs of the same generator may legitimately
// pick different UID strings (different node-content variants will
// hash to different values), but the *shape* of the DAG and the
// distribution of node kinds must match.
//
// Algorithm — Merkle-style topology fingerprint:
//
//	fingerprint(n) = sha1(kv.p || sorted(fingerprint(child) for child in n.Deps))
//
// where:
//   - kv.p is the node's "kind" (CC, AR, …) — the only kv field we
//     attribute to topology. Other kv keys carry per-build metadata
//     (paths, hashes, options) that L1/L2/L3 will compare; they are
//     deliberately excluded from L0 so the topology check stays
//     sensitive to *shape* without drowning in noise.
//   - n.Deps is the slice of UID strings in the post-Finalize graph;
//     each child UID is resolved to its own fingerprint via the byUID
//     index and the children's fingerprints are sorted before hashing
//     so the result is order-independent (defensive: Finalize sorts
//     Deps already per D14, but we re-sort here at fingerprint time so
//     this routine is correct even if fed a graph that wasn't through
//     Finalize).
//
// Comparison: take the multisets {fingerprint(n) for n in want.Graph}
// and {fingerprint(n) for n in got.Graph}, sort both, walk in
// lockstep, count matches. L0 = matches / max(len(want), len(got)) —
// dividing by the larger side guarantees a missing or extra node
// drops L0 below 1.0 even if every other node matches.
//
// Encoding: base64url, 22 chars (matches the UID convention in
// uid.go). Collisions at sha1[:22] are not a concern at our scale
// (~3,730 nodes per graph; birthday bound is ~2^66).

// fingerprintLength is the number of base64url chars we keep from each
// sha1 digest. 22 chars matches the project's UID convention (uid.go)
// — at our scale (~3,730 nodes) the collision probability is
// negligible and there is no operational reason to disagree with the
// established length.
const fingerprintLength = 22

// compareTopology returns (L0, L0Note) for a pair of graphs. See the
// file header for the algorithm; this function is internal — public
// dispatch lives in Compare in compare.go.
func compareTopology(want, got *Graph) (float64, string) {
	wantFPs := topologyFingerprints(want, "want")
	gotFPs := topologyFingerprints(got, "got")

	sort.Strings(wantFPs)
	sort.Strings(gotFPs)

	matched := countMultisetMatches(wantFPs, gotFPs)
	denom := max(len(wantFPs), len(gotFPs))

	if denom == 0 {
		// Both graphs empty (defensive — LoadReference rejects empty
		// graphs and Finalize rejects empty emitters, so this should
		// be unreachable through normal entry points).
		return 1.0, "0 of 0 fingerprints matched"
	}

	l0 := float64(matched) / float64(denom)
	note := fmt.Sprintf("%d of %d fingerprints matched", matched, denom)

	return l0, note
}

// topologyFingerprints computes one fingerprint per node in g, in an
// arbitrary order — callers must sort the returned slice before
// comparing. The label is included in cycle/missing-UID error
// messages so the operator can tell which side of the comparison was
// malformed.
func topologyFingerprints(g *Graph, label string) []string {
	n := len(g.Graph)

	byUID := make(map[string]*Node, n)

	for _, node := range g.Graph {
		if node.UID == "" {
			ThrowFmt("compare: %s graph has node with empty UID", label)
		}

		if _, dup := byUID[node.UID]; dup {
			ThrowFmt("compare: %s graph has duplicate UID %q", label, node.UID)
		}

		byUID[node.UID] = node
	}

	fp := make(map[string]string, n)
	order := topoOrderForFingerprint(g, byUID, label)

	for _, uid := range order {
		node := byUID[uid]

		// Defensive re-sort of children: Finalize already sorts Deps
		// per D14, but if a hand-constructed graph or a stale on-disk
		// file slips in, we still want a stable hash. Sorting the
		// resolved child fingerprints (not the raw UID strings) is
		// what makes the algorithm UID-rename-invariant.
		childFPs := make([]string, 0, len(node.Deps))
		for _, depUID := range node.Deps {
			childFP, ok := fp[depUID]

			if !ok {
				ThrowFmt("compare: %s graph node %q references unknown dep %q", label, uid, depUID)
			}

			childFPs = append(childFPs, childFP)
		}
		sort.Strings(childFPs)

		fp[uid] = hashFingerprint(node.KV["p"], childFPs)
	}

	// Iterate the byUID map in sorted-key order — D14 forbids ranging a
	// map for output. The result slice is sorted again by the caller
	// before comparison, so the order here is purely a determinism
	// hygiene measure (and would matter if a future change ever
	// surfaced this slice without re-sorting).
	uids := make([]string, 0, len(fp))
	for uid := range fp {
		uids = append(uids, uid)
	}
	sort.Strings(uids)

	out := make([]string, 0, len(fp))
	for _, uid := range uids {
		out = append(out, fp[uid])
	}

	return out
}

// hashFingerprint computes the sha1-based fingerprint of one node from
// its kv.p value and its children's already-computed fingerprints.
// Inputs are joined by NUL bytes so a malicious or pathological node
// kind cannot collide with a different (kind, children) split. Caller
// must pass childFPs already sorted.
func hashFingerprint(kindP string, childFPs []string) string {
	h := sha1.New()
	h.Write([]byte(kindP))
	h.Write([]byte{0})

	for _, c := range childFPs {
		h.Write([]byte(c))
		h.Write([]byte{0})
	}

	sum := h.Sum(nil)

	return base64.RawURLEncoding.EncodeToString(sum)[:fingerprintLength]
}

// topoOrderForFingerprint runs Kahn's algorithm over g.Deps (child →
// parent edges) and returns UIDs in dependency-first order. Cycles
// throw — real ymake graphs are DAGs by construction, so a cycle here
// means we got bad input (a hand-built test graph, a corrupted on-disk
// file, etc.), not a comparator bug.
func topoOrderForFingerprint(g *Graph, byUID map[string]*Node, label string) []string {
	n := len(g.Graph)

	indeg := make(map[string]int, n)
	for uid := range byUID {
		indeg[uid] = 0
	}

	// children[uid] = parents that depend on uid. When uid is finalised,
	// each parent's indegree drops by one.
	children := make(map[string][]string, n)
	for _, node := range g.Graph {
		// Dedupe within a single node's Deps so the same edge is not
		// counted twice (D14 sorts but does not mandate dedupe at the
		// dependency-list level for hand-crafted inputs).
		seen := make(map[string]struct{}, len(node.Deps))
		for _, depUID := range node.Deps {
			if _, dup := seen[depUID]; dup {
				continue
			}

			seen[depUID] = struct{}{}

			if _, ok := byUID[depUID]; !ok {
				ThrowFmt("compare: %s graph node %q references unknown dep %q", label, node.UID, depUID)
			}

			children[depUID] = append(children[depUID], node.UID)
			indeg[node.UID]++
		}
	}

	// Seed the queue with every zero-indegree node, in sorted UID
	// order so the topo result is deterministic. Determinism does not
	// matter for the multiset comparison itself (we sort the
	// fingerprints before comparing), but a deterministic walk makes
	// debugging much easier — print the order, diff two runs, etc.
	queue := make([]string, 0, n)
	for uid, d := range indeg {
		if d == 0 {
			queue = append(queue, uid)
		}
	}
	sort.Strings(queue)

	order := make([]string, 0, n)

	for len(queue) > 0 {
		uid := queue[0]
		queue = queue[1:]
		order = append(order, uid)

		// Sort the children before enqueueing so subsequent dequeues
		// remain deterministic. The sort cost is small relative to
		// the sha1 work below.
		kids := children[uid]
		sort.Strings(kids)

		for _, c := range kids {
			indeg[c]--

			if indeg[c] == 0 {
				queue = append(queue, c)
			}
		}
	}

	if len(order) != n {
		// Find one node still with indeg > 0 to name in the error.
		// Iterate sorted for stable error messages.
		stuck := make([]string, 0)
		for uid, d := range indeg {
			if d > 0 {
				stuck = append(stuck, uid)
			}
		}
		sort.Strings(stuck)

		if len(stuck) > 0 {
			ThrowFmt("compare: cycle detected in %s graph involving node %q (and %d others)", label, stuck[0], len(stuck)-1)
		}

		ThrowFmt("compare: cycle detected in %s graph (ordered %d of %d nodes)", label, len(order), n)
	}

	return order
}

// countMultisetMatches walks two pre-sorted slices in lockstep,
// counting elements that appear in both with multiplicity. Identical
// slices produce len(a) == len(b) matches; a missing or extra element
// in one slice skips that side without consuming from the other.
func countMultisetMatches(a, b []string) int {
	i, j, matched := 0, 0, 0

	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			matched++
			i++
			j++
		case a[i] < b[j]:
			i++
		default:
			j++
		}
	}

	return matched
}
