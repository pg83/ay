package main

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// cc_test.go — byte-exact regression test for EmitCC against the
// reference graph for `build/cow/on/lib.c`.
//
// Strategy: rather than relying on PR-03's LoadReference (which is
// landing in parallel), the test does its own os.ReadFile + json.Unmarshal
// into a Graph. The reference graph lives at
// /home/pg/monorepo/yatool_orig/g.json; if that path is absent the test
// is skipped per the STYLE.md / D11 "filter" guidance — no per-host
// test failure.
//
// Comparison is field-by-field, NOT a single reflect.DeepEqual on the
// whole Node. Three reasons:
//   1. UID/SelfUID/StatsUID are computed by Finalize from a Merkle hash
//      and tied to the *whole* graph; for a one-node emit the values
//      drift away from the reference. We exclude them.
//   2. DepRefs/ForeignDepRefs are the unserialised, internal scaffolding
//      that ReadFile-parsed nodes never have; we exclude them too.
//   3. Per-field comparison surfaces the first mismatch with a
//      precise diff, which beats reflect.DeepEqual on a 100+ element
//      Cmd struct returning a single boolean.

const referenceGraphPath = "/home/pg/monorepo/yatool_orig/g.json"
const referenceCCOutput = "$(BUILD_ROOT)/build/cow/on/lib.c.o"

// loadReferenceCCNode reads the on-disk reference graph and returns the
// CC node whose first output is referenceCCOutput. Returns nil and a
// reason string when the file is absent (so the caller can t.Skip) or
// the node is missing.
func loadReferenceCCNode(t *testing.T) (*Node, string) {
	t.Helper()
	raw, err := os.ReadFile(referenceGraphPath)

	if err != nil {
		if os.IsNotExist(err) {
			return nil, "reference graph " + referenceGraphPath + " not present on this host"
		}

		t.Fatalf("read %s: %v", referenceGraphPath, err)
	}

	var g Graph
	Throw(json.Unmarshal(raw, &g))

	for _, n := range g.Graph {
		if len(n.Outputs) > 0 && n.Outputs[0] == referenceCCOutput {
			return n, ""
		}
	}

	return nil, "reference graph contains no CC node for " + referenceCCOutput
}

// fieldEqual is a small helper that wraps reflect.DeepEqual + a diff-y
// failure message with the expected and actual rendered as %#v so a
// mismatch surfaces the offending value rather than a bare false.
func fieldEqual(t *testing.T, name string, got, want interface{}) {
	t.Helper()

	if !reflect.DeepEqual(got, want) {
		t.Errorf("field %s mismatch:\n  got:  %#v\n  want: %#v", name, got, want)
	}
}

func TestEmitCC_BuildCowOnLibC_ByteExact(t *testing.T) {
	ref, skipReason := loadReferenceCCNode(t)

	if ref == nil {
		t.Skip(skipReason)
	}

	emit := NewBufferedEmitter()
	EmitCC(TargetCfg, "build/cow/on", "lib.c", emit)

	if len(emit.nodes) != 1 {
		t.Fatalf("emitter buffered %d nodes, want 1", len(emit.nodes))
	}

	got := emit.nodes[0]

	// cmd_args length is the headline acceptance criterion: PR-08 must
	// produce exactly 101 entries to match the reference.
	if len(got.Cmds) != 1 {
		t.Fatalf("got %d Cmds, want 1", len(got.Cmds))
	}

	if len(got.Cmds[0].CmdArgs) != 101 {
		t.Fatalf("cmd_args length = %d, want 101", len(got.Cmds[0].CmdArgs))
	}

	// Walk cmd_args entry-by-entry so a mismatch reports the offending
	// index instead of dumping a 100-element slice.
	wantArgs := ref.Cmds[0].CmdArgs

	for i := range wantArgs {
		if got.Cmds[0].CmdArgs[i] != wantArgs[i] {
			t.Errorf("cmd_args[%d]:\n  got:  %q\n  want: %q", i, got.Cmds[0].CmdArgs[i], wantArgs[i])
		}
	}

	fieldEqual(t, "cmds[0].env", got.Cmds[0].Env, ref.Cmds[0].Env)
	fieldEqual(t, "inputs", got.Inputs, ref.Inputs)
	fieldEqual(t, "outputs", got.Outputs, ref.Outputs)
	fieldEqual(t, "kv", got.KV, ref.KV)
	fieldEqual(t, "tags", got.Tags, ref.Tags)
	fieldEqual(t, "target_properties", got.TargetProperties, ref.TargetProperties)
	fieldEqual(t, "platform", got.Platform, ref.Platform)
	fieldEqual(t, "requirements", got.Requirements, ref.Requirements)
	fieldEqual(t, "env (top-level)", got.Env, ref.Env)

	// host_platform: leaf compile is target-side, must be false. The
	// reference node has the field omitted (which decodes to false in
	// the Go struct).
	if got.HostPlatform {
		t.Errorf("host_platform: got true, want false")
	}

	if ref.HostPlatform {
		t.Errorf("reference host_platform: got true, want false (sanity check)")
	}

	// foreign_deps: a CC node has no host-tool deps, so the field is
	// nil. Both got and ref must match.
	if got.ForeignDeps != nil {
		t.Errorf("foreign_deps: got %#v, want nil", got.ForeignDeps)
	}

	if ref.ForeignDeps != nil {
		t.Errorf("reference foreign_deps: got %#v, want nil (sanity check)", ref.ForeignDeps)
	}

	// deps: leaf source compile, no upstream nodes. The reference node
	// has `"deps": []` which decodes to an empty (possibly nil) slice;
	// our emitted node has nil DepRefs which Finalize would later turn
	// into []. Pre-finalize we accept either nil or empty.
	if len(got.DepRefs) != 0 {
		t.Errorf("DepRefs: got %d entries, want 0", len(got.DepRefs))
	}

	if len(ref.Deps) != 0 {
		t.Errorf("reference deps: got %d entries, want 0 (sanity check)", len(ref.Deps))
	}
}
