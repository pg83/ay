package main

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// as_test.go — byte-exact regression test for EmitAS against the
// reference graph for contrib/libs/cxxsupp/builtins/aarch64/chkstk.S.
//
// The reference node is located by its output path
// ("$(BUILD_ROOT)/contrib/libs/cxxsupp/builtins/_/aarch64/chkstk.S.o")
// in /home/pg/monorepo/yatool_orig/g.json. If the file is absent the
// test is skipped (per STYLE.md filter pattern), not failed.
//
// Comparison is field-by-field (not a single DeepEqual on the whole
// Node) for the same reasons as cc_test.go: UID/SelfUID/StatsUID are
// excluded (they are Finalize-computed), and per-field diff surfaces the
// first mismatch precisely.

// referenceASOutput is the output path used to locate the target AS node
// in the reference graph.
const referenceASOutput = "$(BUILD_ROOT)/contrib/libs/cxxsupp/builtins/_/aarch64/chkstk.S.o"

// builtinsASIncludes are the module-specific -I flags for the
// contrib/libs/cxxsupp/builtins AS node as observed in the reference
// graph. The first two (-I$(BUILD_ROOT) and -I$(SOURCE_ROOT)) appear in
// every AS node; the remaining six are the musl arch/include paths that
// the builtins module adds via ADDINCL.
//
// These are passed to EmitAS as the `includes` parameter. A future gen
// driver that reads ADDINCL from ya.make will derive them dynamically;
// for the byte-exact test they are pinned here.
var builtinsASIncludes = []string{
	"-I$(BUILD_ROOT)",
	"-I$(SOURCE_ROOT)",
	"-I$(SOURCE_ROOT)/contrib/libs/musl/arch/aarch64",
	"-I$(SOURCE_ROOT)/contrib/libs/musl/arch/generic",
	"-I$(SOURCE_ROOT)/contrib/libs/musl/include",
	"-I$(SOURCE_ROOT)/contrib/libs/musl/extra",
	"-I$(SOURCE_ROOT)/contrib/libs/linux-headers",
	"-I$(SOURCE_ROOT)/contrib/libs/linux-headers/_nf",
}

// loadReferenceASNode reads the on-disk reference graph and returns the
// AS node whose first output is referenceASOutput. Returns nil and a
// reason string when the file is absent (so the caller can t.Skip) or
// when the node is not found.
func loadReferenceASNode(t *testing.T) (*Node, string) {
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
		if len(n.Outputs) > 0 && n.Outputs[0] == referenceASOutput {
			return n, ""
		}
	}

	return nil, "reference graph contains no AS node with output " + referenceASOutput
}

func TestEmitAS_CxxsuppBuiltinsChkstk_ByteExact(t *testing.T) {
	ref, skipReason := loadReferenceASNode(t)

	if ref == nil {
		t.Skip(skipReason)
	}

	emit := NewBufferedEmitter()
	_, outPath := EmitAS(targetInstance("contrib/libs/cxxsupp/builtins"), "aarch64/chkstk.S", builtinsASIncludes, nil, emit)

	if outPath != referenceASOutput {
		t.Errorf("outPath = %q, want %q", outPath, referenceASOutput)
	}

	if len(emit.nodes) != 1 {
		t.Fatalf("emitter buffered %d nodes, want 1", len(emit.nodes))
	}

	got := emit.nodes[0]

	// cmd_args length is the headline acceptance criterion: must be 94.
	if len(got.Cmds) != 1 {
		t.Fatalf("got %d Cmds, want 1", len(got.Cmds))
	}

	if len(got.Cmds[0].CmdArgs) != 94 {
		t.Fatalf("cmd_args length = %d, want 94", len(got.Cmds[0].CmdArgs))
	}

	// Walk cmd_args entry-by-entry so a mismatch reports the offending
	// index instead of dumping the full 94-element slice.
	wantArgs := ref.Cmds[0].CmdArgs

	for i := range wantArgs {
		if i >= len(got.Cmds[0].CmdArgs) {
			t.Errorf("cmd_args[%d]: got (missing), want %q", i, wantArgs[i])
			continue
		}

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

	// host_platform: assembly is target-side, must be false. The
	// reference node omits the field (decodes to false in the Go struct).
	if got.HostPlatform {
		t.Errorf("host_platform: got true, want false")
	}

	if ref.HostPlatform {
		t.Errorf("reference host_platform: got true, want false (sanity check)")
	}

	// foreign_deps: an AS node has no host-tool deps; field must be nil.
	if got.ForeignDeps != nil {
		t.Errorf("foreign_deps: got %#v, want nil", got.ForeignDeps)
	}

	if ref.ForeignDeps != nil {
		t.Errorf("reference foreign_deps: got %#v, want nil (sanity check)", ref.ForeignDeps)
	}

	// DepRefs: leaf assembly, no upstream nodes.
	if len(got.DepRefs) != 0 {
		t.Errorf("DepRefs: got %d entries, want 0", len(got.DepRefs))
	}

	if len(ref.Deps) != 0 {
		t.Errorf("reference deps: got %d entries, want 0 (sanity check)", len(ref.Deps))
	}

	t.Logf("cmd_args length = %d (reference = %d)", len(got.Cmds[0].CmdArgs), len(wantArgs))
}

// TestEmitAS_OutputPath_AlwaysHasUnderscore verifies that the _/ infix
// is unconditional for AS nodes (D29), even for sources with no directory
// component — unlike CC which uses the flat formula for flat sources.
func TestEmitAS_OutputPath_AlwaysHasUnderscore(t *testing.T) {
	e := NewBufferedEmitter()
	_, outPath := EmitAS(targetInstance("some/module"), "flat.S", []string{}, nil, e)
	want := "$(BUILD_ROOT)/some/module/_/flat.S.o"

	if outPath != want {
		t.Errorf("outPath = %q, want %q", outPath, want)
	}
}

// TestEmitAS_OutputPath_NestedSrc verifies the nested-source output path.
func TestEmitAS_OutputPath_NestedSrc(t *testing.T) {
	e := NewBufferedEmitter()
	_, outPath := EmitAS(targetInstance("contrib/libs/cxxsupp/builtins"), "aarch64/chkstk.S", []string{}, nil, e)
	want := "$(BUILD_ROOT)/contrib/libs/cxxsupp/builtins/_/aarch64/chkstk.S.o"

	if outPath != want {
		t.Errorf("outPath = %q, want %q", outPath, want)
	}
}

// TestEmitAS_KV verifies that AS nodes carry the correct kv fields
// (p=AS, pc=light-green, no show_out) as observed in the reference graph.
func TestEmitAS_KV(t *testing.T) {
	e := NewBufferedEmitter()
	EmitAS(targetInstance("some/module"), "aarch64/foo.S", []string{}, nil, e)

	if len(e.nodes) != 1 {
		t.Fatalf("emitter buffered %d nodes, want 1", len(e.nodes))
	}

	got := e.nodes[0]
	want := map[string]string{
		"p":  "AS",
		"pc": "light-green",
	}

	if !reflect.DeepEqual(got.KV, want) {
		t.Errorf("kv:\n  got:  %#v\n  want: %#v", got.KV, want)
	}
}
