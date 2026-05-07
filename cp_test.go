package main

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// cp_test.go — byte-exact regression test for EmitCP against the
// reference graph for the contrib/libs/musl/include/musl.py.pyplugin
// CP node.

const (
	refCPOutput  = "$(BUILD_ROOT)/contrib/libs/musl/include/musl.py.pyplugin"
	cpSrcAbsPath = "$(SOURCE_ROOT)/contrib/libs/musl/include/musl.py"
	cpDstAbsPath = refCPOutput
	cpModuleDir  = "contrib/libs/musl/include"
)

// loadReferenceCPNode reads the on-disk reference graph and returns
// the CP node whose first output matches refCPOutput. Returns nil
// and a reason string when the file is absent or the node is
// missing.
func loadReferenceCPNode(t *testing.T) (*Node, string) {
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
		if len(n.Outputs) > 0 && n.Outputs[0] == refCPOutput {
			return n, ""
		}
	}

	return nil, "reference graph contains no CP node with output " + refCPOutput
}

func TestEmitCP_MuslPyplugin_ByteExact(t *testing.T) {
	ref, skipReason := loadReferenceCPNode(t)

	if ref == nil {
		t.Skip(skipReason)
	}

	e := NewBufferedEmitter()
	EmitCP(targetInstance(cpModuleDir), cpSrcAbsPath, cpDstAbsPath, e)

	if len(e.nodes) != 1 {
		t.Fatalf("emitter buffered %d nodes, want 1", len(e.nodes))
	}

	got := e.nodes[0]

	if len(got.Cmds) != 1 {
		t.Fatalf("cmds len = %d, want 1", len(got.Cmds))
	}

	if len(got.Cmds[0].CmdArgs) != len(ref.Cmds[0].CmdArgs) {
		t.Fatalf("cmd_args length = %d, want %d", len(got.Cmds[0].CmdArgs), len(ref.Cmds[0].CmdArgs))
	}

	for i, want := range ref.Cmds[0].CmdArgs {
		if got.Cmds[0].CmdArgs[i] != want {
			t.Errorf("cmd_args[%d]:\n  got:  %q\n  want: %q", i, got.Cmds[0].CmdArgs[i], want)
		}
	}

	if !reflect.DeepEqual(got.Cmds[0].Env, ref.Cmds[0].Env) {
		t.Errorf("cmds[0].env mismatch:\n  got  %v\n  want %v", got.Cmds[0].Env, ref.Cmds[0].Env)
	}

	if !reflect.DeepEqual(got.Inputs, ref.Inputs) {
		t.Errorf("inputs mismatch:\n  got  %v\n  want %v", got.Inputs, ref.Inputs)
	}

	if !reflect.DeepEqual(got.Outputs, ref.Outputs) {
		t.Errorf("outputs mismatch:\n  got  %v\n  want %v", got.Outputs, ref.Outputs)
	}

	if !reflect.DeepEqual(got.KV, ref.KV) {
		t.Errorf("kv mismatch:\n  got  %v\n  want %v", got.KV, ref.KV)
	}

	if !reflect.DeepEqual(got.Tags, ref.Tags) {
		t.Errorf("tags mismatch:\n  got  %v\n  want %v", got.Tags, ref.Tags)
	}

	if !reflect.DeepEqual(got.TargetProperties, ref.TargetProperties) {
		t.Errorf("target_properties mismatch:\n  got  %v\n  want %v", got.TargetProperties, ref.TargetProperties)
	}

	if got.Platform != ref.Platform {
		t.Errorf("platform mismatch:\n  got  %q\n  want %q", got.Platform, ref.Platform)
	}

	gotReqJSON := Throw2(json.Marshal(got.Requirements))
	refReqJSON := Throw2(json.Marshal(ref.Requirements))

	if string(gotReqJSON) != string(refReqJSON) {
		t.Errorf("requirements mismatch:\n  got  %s\n  want %s", gotReqJSON, refReqJSON)
	}

	if !reflect.DeepEqual(got.Env, ref.Env) {
		t.Errorf("env (top-level) mismatch:\n  got  %v\n  want %v", got.Env, ref.Env)
	}

	if got.HostPlatform {
		t.Errorf("host_platform: got true, want false")
	}

	if ref.HostPlatform {
		t.Errorf("reference host_platform: got true, want false (sanity check)")
	}

	if got.ForeignDeps != nil {
		t.Errorf("foreign_deps: got %#v, want nil", got.ForeignDeps)
	}

	if ref.ForeignDeps != nil {
		t.Errorf("reference foreign_deps: got %#v, want nil (sanity check)", ref.ForeignDeps)
	}

	if len(got.DepRefs) != 0 {
		t.Errorf("DepRefs: got %d entries, want 0", len(got.DepRefs))
	}

	if len(ref.Deps) != 0 {
		t.Errorf("reference deps: got %d entries, want 0 (sanity check)", len(ref.Deps))
	}
}
