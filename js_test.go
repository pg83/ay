package main

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// js_test.go — byte-exact regression test for EmitJS against the
// reference graph for the util/charset JOIN_SRCS node.

const refJSOutput = "$(BUILD_ROOT)/util/charset/all_charset.cpp"

// loadReferenceJSNode reads the on-disk reference graph and returns
// the JS node whose first output matches refJSOutput.
func loadReferenceJSNode(t *testing.T) (*Node, string) {
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
		if len(n.Outputs) > 0 && n.Outputs[0] == refJSOutput {
			return n, ""
		}
	}

	return nil, "reference graph contains no JS node with output " + refJSOutput
}

func TestEmitJS_UtilCharsetAllCharset_ByteExact(t *testing.T) {
	ref, skipReason := loadReferenceJSNode(t)

	if ref == nil {
		t.Skip(skipReason)
	}

	// PR-28-D11: sources are bare module-relative names; EmitJS now
	// prepends instance.Path to form $(SOURCE_ROOT)/<path>/<src>.
	sources := []string{
		"generated/unidata.cpp",
		"recode_result.cpp",
		"unicode_table.cpp",
		"unidata.cpp",
		"utf8.cpp",
		"wide.cpp",
	}

	// PR-35d: feed EmitJS the reference's actual per-source include
	// closure (everything past index 7: the 8-entry scripts+sources
	// prefix). This exercises the closure-threading path and pins
	// the JS node to the reference's exact 941 inputs byte-for-byte.
	closure := append([]string(nil), ref.Inputs[8:]...)

	e := NewBufferedEmitter()
	_, outPath := EmitJS(targetInstance("util/charset"), "all_charset.cpp", sources, closure, e)

	if outPath != refJSOutput {
		t.Errorf("outPath = %q, want %q", outPath, refJSOutput)
	}

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

	// PR-35d: with the reference's closure threaded in, EmitJS now
	// produces byte-exact Inputs against the reference (941 entries).
	// Closure of length 8 (the previous K=8 pin) corresponded to an
	// empty closure under the old EmitJS signature; the new pin
	// covers the full reference shape end-to-end.
	if len(got.Inputs) != len(ref.Inputs) {
		t.Errorf("inputs count = %d, want %d", len(got.Inputs), len(ref.Inputs))
	}

	for i, gotIn := range got.Inputs {
		if i >= len(ref.Inputs) {
			break
		}

		if gotIn != ref.Inputs[i] {
			t.Errorf("inputs[%d]:\n  got  %q\n  want %q", i, gotIn, ref.Inputs[i])
		}
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

	if got.ForeignDeps != nil {
		t.Errorf("foreign_deps: got %#v, want nil", got.ForeignDeps)
	}

	if len(got.DepRefs) != 0 {
		t.Errorf("DepRefs: got %d entries, want 0", len(got.DepRefs))
	}
}
