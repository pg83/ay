package main

import (
	"fmt"
)

// compare_cmd.go — L3 per-pair byte-exact comparison of cmds + env.
//
// L3 is the strictest level shipped so far. A pair matches iff:
//
//   - len(want.Cmds) == len(got.Cmds), AND
//   - for every index i: want.Cmds[i].CmdArgs equals got.Cmds[i].CmdArgs
//     as ordered slices (a permutation is a mismatch — argv order is
//     load-bearing for any real toolchain invocation), AND every cmd's
//     Env map is equal (nil treated as empty), AND
//   - the top-level node Env maps are equal (nil treated as empty).
//
// Everything weaker than byte-exact (whitespace tolerance, env-key
// canonicalisation, cmd-list reordering) is intentionally out of
// scope: if two graphs differ even at this level, we want to *see*
// the difference, not hide it. A future L4 may add structured argv
// diffing on top of this raw equality check; for now the report is a
// percentage and a one-line note in the same shape as L1/L2.
//
// Pairing is reused from compare_props.go (pairByOutput), so the
// (outputs[0], platform) discriminator and the wantOnly/gotOnly
// bookkeeping stay consistent across L1/L2/L3.
//
// Denominator: max(len(want.Graph), len(got.Graph)) — same as L0/L1/L2.
//
// Determinism (D14): the matched count is iteration-order
// independent. Iterating the `pairs` map and incrementing an integer
// is safe even though Go map iteration is randomised.

// compareL3 walks the pairing and counts pairs whose cmds (per-cmd
// CmdArgs + per-cmd Env, ordered) and top-level Env all match
// byte-exactly. Returns the percentage and a one-line note in the
// same shape as compareL1 / compareL2.
func compareL3(want, got *Graph, pairs map[string]string, wantOnly, gotOnly []string) (l3 float64, note string) {
	wantByUID := indexByUID(want)
	gotByUID := indexByUID(got)

	matched := 0

	for wantUID, gotUID := range pairs {
		if l3Match(wantByUID[wantUID], gotByUID[gotUID]) {
			matched++
		}
	}

	denom := max(len(want.Graph), len(got.Graph))

	if denom == 0 {
		return 1.0, "0 matched / 0 pairs / 0 unpaired-want / 0 unpaired-got"
	}

	l3 = float64(matched) / float64(denom)
	note = fmt.Sprintf("%d matched / %d pairs / %d unpaired-want / %d unpaired-got", matched, len(pairs), len(wantOnly), len(gotOnly))

	return l3, note
}

// l3Match returns true iff want and got agree on every L3 field:
// per-cmd CmdArgs (ordered slice equality), per-cmd Env (map
// equality), and the top-level node Env (map equality). Order matters
// for both the Cmds slice itself and the CmdArgs within each Cmd —
// a reordering would change toolchain semantics in the real graph,
// so a "permutation = match" relaxation would be wrong.
func l3Match(want, got *Node) bool {
	if len(want.Cmds) != len(got.Cmds) {
		return false
	}

	for i := range want.Cmds {
		if !stringSliceEqual(want.Cmds[i].CmdArgs, got.Cmds[i].CmdArgs) {
			return false
		}

		if !stringMapEqual(want.Cmds[i].Env, got.Cmds[i].Env) {
			return false
		}
	}

	if !stringMapEqual(want.Env, got.Env) {
		return false
	}

	return true
}

// stringMapEqual treats nil and empty maps as equal.
//
// L3 compares emitter-produced node Envs against JSON-decoded reference
// Envs. The emitter may populate Env as nil when a rule has no env vars,
// while the reference's "env": {} decodes to a non-nil empty map.
// reflect.DeepEqual would call these unequal — stringMapEqual treats
// them as equal, matching the user's intent.
func stringMapEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}

	for k, v := range a {
		if v2, ok := b[k]; !ok || v2 != v {
			return false
		}
	}

	return true
}
