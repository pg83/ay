package main

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

// TestEmitAR_BuildCowOn_ByteExact verifies that EmitAR produces a node
// that is field-for-field identical to the reference AR node in
// /home/pg/monorepo/yatool_orig/g.json for the build/cow/on module.
//
// The test loads the reference graph from disk. If the file is absent
// (e.g. in CI without the reference dataset) it is skipped. All fields
// that the reference node carries are compared exactly; any mismatch
// prints expected vs actual for the failing field.
func TestEmitAR_BuildCowOn_ByteExact(t *testing.T) {
	const refPath = "/home/pg/monorepo/yatool_orig/g.json"
	const targetOutput = "$(BUILD_ROOT)/build/cow/on/libbuild-cow-on.a"

	raw, err := os.ReadFile(refPath)
	if err != nil {
		t.Skipf("reference graph not available (%v); skipping byte-exact test", err)
	}

	var g Graph
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatalf("unmarshal reference graph: %v", err)
	}

	// Locate the reference AR node by its output path.
	var ref *Node

	for _, n := range g.Graph {
		for _, o := range n.Outputs {
			if o == targetOutput {
				ref = n

				break
			}
		}

		if ref != nil {
			break
		}
	}

	if ref == nil {
		t.Fatalf("reference AR node with output %q not found in %s", targetOutput, refPath)
	}

	// Build a BufferedEmitter and emit a placeholder leaf node so we
	// have a valid NodeRef to pass as objRefs[0].
	e := NewBufferedEmitter()

	leafRef := e.Emit(&Node{
		Cmds:             []Cmd{{CmdArgs: []string{"cc"}, Env: map[string]string{}}},
		Env:              map[string]string{},
		Inputs:           []string{},
		KV:               map[string]string{},
		Outputs:          []string{"$(BUILD_ROOT)/build/cow/on/lib.c.o"},
		Platform:         "default-linux-aarch64",
		Requirements:     map[string]interface{}{},
		Tags:             []string{},
		TargetProperties: map[string]string{},
	})

	arRef := EmitAR(
		"default-linux-aarch64",
		"build/cow/on",
		[]NodeRef{leafRef},
		[]string{"$(BUILD_ROOT)/build/cow/on/lib.c.o"},
		e,
	)

	// Retrieve the emitted AR node directly from the emitter's buffer
	// (not via Finalize — UIDs would differ from the reference). The AR
	// node is index arRef.id in the internal buffer.
	got := e.nodes[arRef.id]

	// --- Field-by-field assertions ---

	// cmds[0].cmd_args
	if len(got.Cmds) != 1 {
		t.Fatalf("cmds len = %d, want 1", len(got.Cmds))
	}

	if !reflect.DeepEqual(got.Cmds[0].CmdArgs, ref.Cmds[0].CmdArgs) {
		t.Errorf("cmds[0].cmd_args mismatch:\n  want %v\n  got  %v", ref.Cmds[0].CmdArgs, got.Cmds[0].CmdArgs)
	}

	// cmds[0].env
	if !reflect.DeepEqual(got.Cmds[0].Env, ref.Cmds[0].Env) {
		t.Errorf("cmds[0].env mismatch:\n  want %v\n  got  %v", ref.Cmds[0].Env, got.Cmds[0].Env)
	}

	// inputs
	if !reflect.DeepEqual(got.Inputs, ref.Inputs) {
		t.Errorf("inputs mismatch:\n  want %v\n  got  %v", ref.Inputs, got.Inputs)
	}

	// outputs
	if !reflect.DeepEqual(got.Outputs, ref.Outputs) {
		t.Errorf("outputs mismatch:\n  want %v\n  got  %v", ref.Outputs, got.Outputs)
	}

	// kv
	if !reflect.DeepEqual(got.KV, ref.KV) {
		t.Errorf("kv mismatch:\n  want %v\n  got  %v", ref.KV, got.KV)
	}

	// tags
	if !reflect.DeepEqual(got.Tags, ref.Tags) {
		t.Errorf("tags mismatch:\n  want %v\n  got  %v", ref.Tags, got.Tags)
	}

	// target_properties
	if !reflect.DeepEqual(got.TargetProperties, ref.TargetProperties) {
		t.Errorf("target_properties mismatch:\n  want %v\n  got  %v", ref.TargetProperties, got.TargetProperties)
	}

	// requirements — reference comes from JSON so values are float64; our
	// node uses float64 too. Deep-compare using JSON round-trip to handle
	// any numeric type difference cleanly.
	gotReqJSON := Throw2(json.Marshal(got.Requirements))
	refReqJSON := Throw2(json.Marshal(ref.Requirements))

	if string(gotReqJSON) != string(refReqJSON) {
		t.Errorf("requirements mismatch:\n  want %s\n  got  %s", refReqJSON, gotReqJSON)
	}

	// env (top-level)
	if !reflect.DeepEqual(got.Env, ref.Env) {
		t.Errorf("env mismatch:\n  want %v\n  got  %v", ref.Env, got.Env)
	}

	// platform
	if got.Platform != ref.Platform {
		t.Errorf("platform mismatch:\n  want %q\n  got  %q", ref.Platform, got.Platform)
	}

	// host_platform — reference node omits the field, so it defaults to false.
	if got.HostPlatform != false {
		t.Errorf("host_platform = %v, want false", got.HostPlatform)
	}

	if ref.HostPlatform != false {
		t.Errorf("reference host_platform = %v, want false (sanity check)", ref.HostPlatform)
	}

	// foreign_deps — reference node omits the field, so it must be nil.
	if got.ForeignDeps != nil {
		t.Errorf("foreign_deps = %v, want nil", got.ForeignDeps)
	}

	if ref.ForeignDeps != nil {
		t.Errorf("reference foreign_deps = %v, want nil (sanity check)", ref.ForeignDeps)
	}

	// DepRefs count — pins that EmitAR populates DepRefs in 1:1 correspondence
	// with objRefs. Bypassing Finalize means DepRefs is still []NodeRef, not []string.
	if len(got.DepRefs) != len(ref.Deps) {
		t.Errorf("DepRefs len = %d, want %d (ref Deps count)", len(got.DepRefs), len(ref.Deps))
	}

	// Report cmd_args length for the deliverable check.
	t.Logf("cmd_args length = %d", len(got.Cmds[0].CmdArgs))

	// Report archive naming for the deliverable check.
	t.Logf("archive name convention: moduleDir=%q → outputs[0]=%q", "build/cow/on", got.Outputs[0])

	if t.Failed() {
		t.Logf("full emitted node (JSON):\n%s", func() string {
			b, e2 := json.MarshalIndent(got, "", "  ")
			if e2 != nil {
				return fmt.Sprintf("<marshal error: %v>", e2)
			}

			return string(b)
		}())
	}
}

// TestEmitAR_LengthMismatchPanics verifies that EmitAR throws when
// objRefs and objPaths have different lengths.
func TestEmitAR_LengthMismatchPanics(t *testing.T) {
	e := NewBufferedEmitter()

	objRefs := []NodeRef{e.Emit(&Node{
		Cmds:             []Cmd{{CmdArgs: []string{"cc"}, Env: map[string]string{}}},
		Env:              map[string]string{},
		Inputs:           []string{},
		KV:               map[string]string{},
		Outputs:          []string{"$(BUILD_ROOT)/build/cow/on/lib.c.o"},
		Platform:         "default-linux-aarch64",
		Requirements:     map[string]interface{}{},
		Tags:             []string{},
		TargetProperties: map[string]string{},
	})}
	objPaths := []string{"$(BUILD_ROOT)/o1.o", "$(BUILD_ROOT)/o2.o"} // length 2 vs refs length 1

	exc := Try(func() {
		EmitAR("default-linux-aarch64", "build/cow/on", objRefs, objPaths, e)
	})

	if exc == nil {
		t.Fatal("expected exception for length mismatch")
	}

	if !strings.Contains(exc.Error(), "length mismatch") {
		t.Errorf("unexpected error: %v", exc)
	}
}
