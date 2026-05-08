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
// in /home/pg/monorepo/yatool_orig/sg.json. If the file is absent the
// test is skipped (per STYLE.md filter pattern), not failed.
//
// Comparison is field-by-field (not a single DeepEqual on the whole
// Node) for the same reasons as cc_test.go: UID/SelfUID/StatsUID are
// excluded (they are Finalize-computed), and per-field diff surfaces the
// first mismatch precisely.

// referenceASOutput is the output path used to locate the target AS node
// in the reference graph.
const referenceASOutput = "$(BUILD_ROOT)/contrib/libs/cxxsupp/builtins/_/aarch64/chkstk.S.o"

// builtinsASOwnAddIncl is the own-ADDINCL slice cxxsupp/builtins
// declares in its ya.make (the four musl-arch paths added under
// `IF (MUSL)`). PR-35m: the AS composer assembles the full include
// tail from these (own AddIncl) plus `ccIncludesPrefix`/`ccIncludesSuffix`
// (BUILD_ROOT/SOURCE_ROOT + linux-headers pair) so the previously
// pre-baked flat list now derives structurally.
var builtinsASOwnAddIncl = []string{
	"contrib/libs/musl/arch/aarch64",
	"contrib/libs/musl/arch/generic",
	"contrib/libs/musl/include",
	"contrib/libs/musl/extra",
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
	// PR-31 D11: chkstk.S transitively includes assembly.h + int_endianness.h.
	chkstkIncludeInputs := []string{
		"$(SOURCE_ROOT)/contrib/libs/cxxsupp/builtins/assembly.h",
		"$(SOURCE_ROOT)/contrib/libs/cxxsupp/builtins/int_endianness.h",
	}

	// PR-35i: cxxsupp/builtins declares `NO_COMPILER_WARNINGS()`
	// (contrib/libs/cxxsupp/builtins/ya.make:19); set the flag on
	// the test instance so EmitAS picks the `-Wno-everything` branch
	// of `pickWarningFlags`. inferFlagsFromPath does not derive
	// NoCompilerWarnings (only macro parsing does in the real walker),
	// so the synthetic test must inject it.
	chkstkInstance := targetInstance("contrib/libs/cxxsupp/builtins")
	chkstkInstance.Flags.NoCompilerWarnings = true
	chkstkIn := ModuleCCInputs{
		AddIncl:       builtinsASOwnAddIncl,
		IncludeInputs: chkstkIncludeInputs,
	}
	_, outPath := EmitAS(chkstkInstance, "aarch64/chkstk.S", chkstkIn, nil, emit)

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

	if got.Cmds[0].Cwd != ref.Cmds[0].Cwd {
		t.Errorf("Cmds[0].Cwd = %q, want %q", got.Cmds[0].Cwd, ref.Cmds[0].Cwd)
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

// TestEmitAS_OutputPath_FlatSrcRel verifies that a flat srcRel (no "/" component)
// produces a flat output path with no _/ infix (PR-35r cluster 4 fix).
// Empirical reference: contrib/libs/asmglibc/memchr.S.o (flat, no _/).
func TestEmitAS_OutputPath_FlatSrcRel(t *testing.T) {
	e := NewBufferedEmitter()
	_, outPath := EmitAS(targetInstance("some/module"), "flat.S", ModuleCCInputs{}, nil, e)
	want := "$(BUILD_ROOT)/some/module/flat.S.o"

	if outPath != want {
		t.Errorf("outPath = %q, want %q", outPath, want)
	}
}

// TestEmitAS_OutputPath_NestedSrc verifies the nested-source output path.
func TestEmitAS_OutputPath_NestedSrc(t *testing.T) {
	e := NewBufferedEmitter()
	_, outPath := EmitAS(targetInstance("contrib/libs/cxxsupp/builtins"), "aarch64/chkstk.S", ModuleCCInputs{}, nil, e)
	want := "$(BUILD_ROOT)/contrib/libs/cxxsupp/builtins/_/aarch64/chkstk.S.o"

	if outPath != want {
		t.Errorf("outPath = %q, want %q", outPath, want)
	}
}

// TestEmitAS_OutputPath_SrcDir verifies the __/ infix for ancestor-SRCDIR cases
// (PR-35r cluster 5). When in.SrcDir is set and the source does not resolve
// locally, the output path uses composeSrcDirOutputRel (same as CC case 3).
func TestEmitAS_OutputPath_SrcDir(t *testing.T) {
	e := NewBufferedEmitter()
	// tcmalloc/no_percpu_cache: SRCDIR = contrib/libs/tcmalloc (ancestor).
	// srcRel = tcmalloc/internal/percpu_rseq_asm.S
	// Expected: __/tcmalloc/internal/percpu_rseq_asm.S.o
	in := ModuleCCInputs{SrcDir: "contrib/libs/tcmalloc"}
	_, outPath := EmitAS(
		targetInstance("contrib/libs/tcmalloc/no_percpu_cache"),
		"tcmalloc/internal/percpu_rseq_asm.S",
		in,
		nil,
		e,
	)
	want := "$(BUILD_ROOT)/contrib/libs/tcmalloc/no_percpu_cache/__/tcmalloc/internal/percpu_rseq_asm.S.o"

	if outPath != want {
		t.Errorf("outPath = %q, want %q", outPath, want)
	}
}

// TestEmitAS_AsmgLibc_Memchr_ByteExact (PR-35r cluster 4) pins the flat
// output path for asmglibc/memchr.S.o against the reference graph.
// asmglibc is a host-PIC (x86_64) clang AS module with a single-component
// srcRel — the reference output is flat (no _/ infix).
func TestEmitAS_AsmgLibc_Memchr_ByteExact(t *testing.T) {
	const targetOut = "$(BUILD_ROOT)/contrib/libs/asmglibc/memchr.S.o"

	raw, err := os.ReadFile(referenceGraphPath)

	if err != nil {
		t.Skipf("reference graph not available (%v); skipping asmglibc AS byte-exact test", err)
	}

	var g Graph
	Throw(json.Unmarshal(raw, &g))

	var ref *Node

	for _, n := range g.Graph {
		if len(n.Outputs) > 0 && n.Outputs[0] == targetOut {
			ref = n

			break
		}
	}

	if ref == nil {
		t.Fatalf("reference asmglibc AS node with output %q not found", targetOut)
	}

	emit := NewBufferedEmitter()

	// asmglibc: host-PIC (x86_64), no SrcDir, no own AddIncl, no peer
	// AddIncl, NoCompilerWarnings=false (full warning bundle in reference).
	// srcRel = "memchr.S" (flat — PR-35r fix: no _/ infix in output).
	// IncludeInputs from scanner: sysdep.h only.
	asmglibcInst := hostInstance("contrib/libs/asmglibc")
	asmglibcIn := ModuleCCInputs{
		IncludeInputs: []string{
			"$(SOURCE_ROOT)/contrib/libs/asmglibc/sysdep.h",
		},
	}
	_, outPath := EmitAS(asmglibcInst, "memchr.S", asmglibcIn, nil, emit)

	if outPath != targetOut {
		t.Errorf("outPath = %q, want %q", outPath, targetOut)
	}

	if len(emit.nodes) != 1 {
		t.Fatalf("emitter buffered %d nodes, want 1", len(emit.nodes))
	}

	got := emit.nodes[0]
	wantArgs := ref.Cmds[0].CmdArgs

	if len(got.Cmds[0].CmdArgs) != len(wantArgs) {
		t.Fatalf("cmd_args length = %d, want %d", len(got.Cmds[0].CmdArgs), len(wantArgs))
	}

	for i := range wantArgs {
		if got.Cmds[0].CmdArgs[i] != wantArgs[i] {
			t.Errorf("cmd_args[%d]:\n  got:  %q\n  want: %q", i, got.Cmds[0].CmdArgs[i], wantArgs[i])
		}
	}

	if !got.HostPlatform {
		t.Errorf("host_platform: got false, want true")
	}

	fieldEqual(t, "inputs", got.Inputs, ref.Inputs)
	fieldEqual(t, "outputs", got.Outputs, ref.Outputs)

	t.Logf("cmd_args length = %d (reference = %d)", len(got.Cmds[0].CmdArgs), len(wantArgs))
}

// TestEmitAS_TcmallocNopercpu_PercpuRseqAsm_ByteExact (PR-35r cluster 5)
// pins the full cmd_args bundle and output path for
// tcmalloc/no_percpu_cache/__/tcmalloc/internal/percpu_rseq_asm.S.o.
// This module uses SRCDIR(contrib/libs/tcmalloc) (ancestor), so the
// output infix is __/ and the input comes from $(SOURCE_ROOT)/contrib/libs/tcmalloc/...
func TestEmitAS_TcmallocNopercpu_PercpuRseqAsm_ByteExact(t *testing.T) {
	const targetOut = "$(BUILD_ROOT)/contrib/libs/tcmalloc/no_percpu_cache/__/tcmalloc/internal/percpu_rseq_asm.S.o"

	raw, err := os.ReadFile(referenceGraphPath)

	if err != nil {
		t.Skipf("reference graph not available (%v); skipping tcmalloc percpu_rseq AS byte-exact test", err)
	}

	var g Graph
	Throw(json.Unmarshal(raw, &g))

	var ref *Node

	for _, n := range g.Graph {
		if len(n.Outputs) > 0 && n.Outputs[0] == targetOut {
			ref = n

			break
		}
	}

	if ref == nil {
		t.Fatalf("reference tcmalloc AS node with output %q not found", targetOut)
	}

	emit := NewBufferedEmitter()

	// tcmalloc/no_percpu_cache: target-side (PIC=false), SRCDIR=contrib/libs/tcmalloc,
	// NO_COMPILER_WARNINGS=true, own AddIncl=contrib/libs/tcmalloc (ADDINCL GLOBAL),
	// own CFLAGS from ya.make (-DTCMALLOC_INTERNAL_256K_PAGES, -DTCMALLOC_DEPRECATED_PERTHREAD
	// only — -UNDEBUG and -mno-outline-atomics are the noLibcUndebugBlock prefix),
	// AutoPeerCFlags=-D_musl_, PeerAddInclGlobal mirrors reference cmd_args[94..101].
	tcmallocInst := targetInstance("contrib/libs/tcmalloc/no_percpu_cache")
	tcmallocInst.Flags.NoCompilerWarnings = true
	tcmallocIn := ModuleCCInputs{
		SrcDir: "contrib/libs/tcmalloc",
		// own AddIncl: ADDINCL GLOBAL from common.inc
		AddIncl: []string{"contrib/libs/tcmalloc"},
		// own CFLAGS from no_percpu_cache/ya.make CFLAGS() block only.
		// -UNDEBUG/-mno-outline-atomics are part of noLibcUndebugBlock, not own CFlags.
		CFlags: []string{
			"-DTCMALLOC_INTERNAL_256K_PAGES",
			"-DTCMALLOC_DEPRECATED_PERTHREAD",
		},
		AutoPeerCFlags: []string{"-D_musl_"},
		PeerAddInclGlobal: []string{
			"contrib/libs/cxxsupp/libcxx/include",
			"contrib/libs/cxxsupp/libcxxrt/include",
			"contrib/libs/musl/arch/aarch64",
			"contrib/libs/musl/arch/generic",
			"contrib/libs/musl/include",
			"contrib/libs/musl/extra",
			"contrib/restricted/abseil-cpp",
			"contrib/libs/tcmalloc",
		},
		IncludeInputs: ref.Inputs[1:], // all but the primary source
	}
	_, outPath := EmitAS(tcmallocInst, "tcmalloc/internal/percpu_rseq_asm.S", tcmallocIn, nil, emit)

	if outPath != targetOut {
		t.Errorf("outPath = %q, want %q", outPath, targetOut)
	}

	if len(emit.nodes) != 1 {
		t.Fatalf("emitter buffered %d nodes, want 1", len(emit.nodes))
	}

	got := emit.nodes[0]
	wantArgs := ref.Cmds[0].CmdArgs

	if len(got.Cmds[0].CmdArgs) != len(wantArgs) {
		// Print mismatched args for diagnosis.
		for i := 85; i < len(got.Cmds[0].CmdArgs) && i < len(wantArgs)+5; i++ {
			got_a := "(missing)"
			want_a := "(missing)"
			if i < len(got.Cmds[0].CmdArgs) {
				got_a = got.Cmds[0].CmdArgs[i]
			}
			if i < len(wantArgs) {
				want_a = wantArgs[i]
			}
			t.Logf("cmd_args[%d]: got=%q want=%q", i, got_a, want_a)
		}
		t.Fatalf("cmd_args length = %d, want %d", len(got.Cmds[0].CmdArgs), len(wantArgs))
	}

	for i := range wantArgs {
		if got.Cmds[0].CmdArgs[i] != wantArgs[i] {
			t.Errorf("cmd_args[%d]:\n  got:  %q\n  want: %q", i, got.Cmds[0].CmdArgs[i], wantArgs[i])
		}
	}

	fieldEqual(t, "inputs", got.Inputs, ref.Inputs)
	fieldEqual(t, "outputs", got.Outputs, ref.Outputs)
	fieldEqual(t, "target_properties", got.TargetProperties, ref.TargetProperties)

	if got.HostPlatform {
		t.Errorf("host_platform: got true, want false (target-side AS)")
	}

	t.Logf("cmd_args length = %d (reference = %d)", len(got.Cmds[0].CmdArgs), len(wantArgs))
}

// TestEmitAS_YasmLD_PopulatesDepRefs verifies that when yasmLD is non-nil,
// EmitAS wires it into both DepRefs and ForeignDepRefs["tool"] (PR-30 D02).
// The L0 fingerprint reads only deps; the foreign-deps-only shape diverged
// for asmlib's 25 AS nodes in the reference graph.
func TestEmitAS_YasmLD_PopulatesDepRefs(t *testing.T) {
	e := NewBufferedEmitter()

	// Emit a minimal stand-in node to obtain a valid NodeRef for yasmLD.
	// The node's content is irrelevant — we only need its identity.
	yasmLDRef := e.Emit(&Node{
		Cmds:         []Cmd{{CmdArgs: []string{"yasm"}, Env: map[string]string{}}},
		Env:          map[string]string{},
		Inputs:       []string{},
		Outputs:      []string{"$(BUILD_ROOT)/tools/yasm/yasm"},
		KV:           map[string]string{"p": "LD", "pc": "light-cyan"},
		Tags:         []string{"tool"},
		Platform:     string(PlatformDefaultLinuxX8664),
		Requirements: map[string]interface{}{"cpu": float64(1), "network": "restricted", "ram": float64(32)},
		TargetProperties: map[string]string{
			"module_dir": "tools/yasm",
		},
	})

	yasmTestIn := ModuleCCInputs{AddIncl: builtinsASOwnAddIncl}
	ref, _ := EmitAS(targetInstance("contrib/libs/cxxsupp/builtins"), "aarch64/chkstk.S", yasmTestIn, &yasmLDRef, e)

	// The AS node is at index 1 (yasmLD is at index 0).
	if len(e.nodes) != 2 {
		t.Fatalf("emitter buffered %d nodes, want 2", len(e.nodes))
	}

	_ = ref
	got := e.nodes[1]

	// DepRefs must contain exactly the yasmLD ref.
	if len(got.DepRefs) != 1 || got.DepRefs[0] != yasmLDRef {
		t.Errorf("DepRefs = %v, want [%v]", got.DepRefs, yasmLDRef)
	}

	// ForeignDepRefs["tool"] must also contain the yasmLD ref.
	toolRefs := got.ForeignDepRefs["tool"]

	if len(toolRefs) != 1 || toolRefs[0] != yasmLDRef {
		t.Errorf(`ForeignDepRefs["tool"] = %v, want [%v]`, toolRefs, yasmLDRef)
	}
}

// TestEmitAS_KV verifies that AS nodes carry the correct kv fields
// (p=AS, pc=light-green, no show_out) as observed in the reference graph.
func TestEmitAS_KV(t *testing.T) {
	e := NewBufferedEmitter()
	EmitAS(targetInstance("some/module"), "aarch64/foo.S", ModuleCCInputs{}, nil, e)

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

// TestEmitAS_MuslHost_Ceill_ByteExact (PR-35a) pins the cmd_args bundle
// for a host x86_64 musl-self assembly node against the reference graph
// (`$(BUILD_ROOT)/contrib/libs/musl/_/src/math/x86_64/ceill.s.o`). Total
// 109 args: x86_64 toolchain + hostCFlags / hostDefines / muslExtraDefines
// + ndebugPicBlock × 2 with hostSseFeatures between + the tail
// muslCcIncludesX8664 set. Verifies that:
//
//   - target triple is x86_64-linux-gnu (NOT aarch64-linux-gnu).
//   - no `-march=` flag (host is generic x86_64).
//   - `-D_musl_=1` is present (muslExtraDefines).
//   - host_platform=true and tags=["tool"].
func TestEmitAS_MuslHost_Ceill_ByteExact(t *testing.T) {
	const targetOut = "$(BUILD_ROOT)/contrib/libs/musl/_/src/math/x86_64/ceill.s.o"

	raw, err := os.ReadFile(referenceGraphPath)

	if err != nil {
		t.Skipf("reference graph not available (%v); skipping host musl AS byte-exact test", err)
	}

	var g Graph
	Throw(json.Unmarshal(raw, &g))

	var ref *Node

	for _, n := range g.Graph {
		if len(n.Outputs) > 0 && n.Outputs[0] == targetOut {
			ref = n

			break
		}
	}

	if ref == nil {
		t.Fatalf("reference host musl AS node with output %q not found", targetOut)
	}

	emit := NewBufferedEmitter()

	// PR-35i: contrib/libs/musl declares `NO_COMPILER_WARNINGS()`
	// (contrib/libs/musl/ya.make:25); set the flag on the test
	// instance so EmitAS picks the `-Wno-everything` branch of
	// `pickWarningFlags`. inferFlagsFromPath does not derive
	// NoCompilerWarnings (only macro parsing does in the real walker).
	ceillInstance := muslHostInstance("contrib/libs/musl")
	ceillInstance.Flags.NoCompilerWarnings = true
	_, outPath := EmitAS(ceillInstance, "src/math/x86_64/ceill.s", ModuleCCInputs{}, nil, emit)

	if outPath != targetOut {
		t.Errorf("outPath = %q, want %q", outPath, targetOut)
	}

	if len(emit.nodes) != 1 {
		t.Fatalf("emitter buffered %d nodes, want 1", len(emit.nodes))
	}

	got := emit.nodes[0]
	wantArgs := ref.Cmds[0].CmdArgs

	if len(got.Cmds[0].CmdArgs) != len(wantArgs) {
		t.Fatalf("cmd_args length = %d, want %d", len(got.Cmds[0].CmdArgs), len(wantArgs))
	}

	for i := range wantArgs {
		if got.Cmds[0].CmdArgs[i] != wantArgs[i] {
			t.Errorf("cmd_args[%d]:\n  got:  %q\n  want: %q", i, got.Cmds[0].CmdArgs[i], wantArgs[i])
		}
	}

	if !got.HostPlatform {
		t.Errorf("host_platform: got false, want true")
	}

	if len(got.Tags) != 1 || got.Tags[0] != "tool" {
		t.Errorf("tags = %v, want [\"tool\"]", got.Tags)
	}

	if got.Platform != string(PlatformDefaultLinuxX8664) {
		t.Errorf("platform = %q, want %q", got.Platform, PlatformDefaultLinuxX8664)
	}

	t.Logf("cmd_args length = %d (reference = %d)", len(got.Cmds[0].CmdArgs), len(wantArgs))
}

// TestEmitAS_HostNonMusl_X8664Chkstk_ByteExact (PR-35a / PR-35m closure)
// pins the full cmd_args bundle for a host x86_64 non-musl AS node
// (`$(BUILD_ROOT)/contrib/libs/cxxsupp/builtins/_/x86_64/chkstk.S.o`)
// against the reference. PR-35m retired the prologue-only bound by
// threading the include-tail (own AddIncl: musl-arch×4 x86_64 + linux-
// headers via prefix/suffix) through ModuleCCInputs.
func TestEmitAS_HostNonMusl_X8664Chkstk_ByteExact(t *testing.T) {
	const targetOut = "$(BUILD_ROOT)/contrib/libs/cxxsupp/builtins/_/x86_64/chkstk.S.o"

	raw, err := os.ReadFile(referenceGraphPath)

	if err != nil {
		t.Skipf("reference graph not available (%v); skipping host non-musl AS byte-exact test", err)
	}

	var g Graph
	Throw(json.Unmarshal(raw, &g))

	var ref *Node

	for _, n := range g.Graph {
		if len(n.Outputs) > 0 && n.Outputs[0] == targetOut {
			ref = n

			break
		}
	}

	if ref == nil {
		t.Fatalf("reference host non-musl AS node with output %q not found", targetOut)
	}

	emit := NewBufferedEmitter()

	// PR-35i: cxxsupp/builtins declares `NO_COMPILER_WARNINGS()`; set
	// the flag on the test instance so EmitAS picks the
	// `-Wno-everything` branch of `pickWarningFlags`.
	// PR-35m: own AddIncl carries the host-arch musl include set as
	// declared by the IF (ARCH_X86_64) branch of cxxsupp/builtins'
	// ya.make — same shape as the aarch64 byte-exact test but with
	// `arch/x86_64` substituted.
	hostInst := hostInstance("contrib/libs/cxxsupp/builtins")
	hostInst.Flags.NoCompilerWarnings = true
	hostIn := ModuleCCInputs{
		AddIncl: []string{
			"contrib/libs/musl/arch/x86_64",
			"contrib/libs/musl/arch/generic",
			"contrib/libs/musl/include",
			"contrib/libs/musl/extra",
		},
	}
	_, outPath := EmitAS(hostInst, "x86_64/chkstk.S", hostIn, nil, emit)

	if outPath != targetOut {
		t.Errorf("outPath = %q, want %q", outPath, targetOut)
	}

	if len(emit.nodes) != 1 {
		t.Fatalf("emitter buffered %d nodes, want 1", len(emit.nodes))
	}

	got := emit.nodes[0]
	gotArgs := got.Cmds[0].CmdArgs
	wantArgs := ref.Cmds[0].CmdArgs

	if len(gotArgs) != len(wantArgs) {
		t.Fatalf("cmd_args length = %d, want %d", len(gotArgs), len(wantArgs))
	}

	for i := range wantArgs {
		if gotArgs[i] != wantArgs[i] {
			t.Errorf("cmd_args[%d]:\n  got:  %q\n  want: %q", i, gotArgs[i], wantArgs[i])
		}
	}

	// Toolchain identity assertions: x86_64-linux-gnu, no -march,
	// no -D_musl_=1.
	if gotArgs[1] != "--target=x86_64-linux-gnu" {
		t.Errorf("cmd_args[1] = %q, want --target=x86_64-linux-gnu", gotArgs[1])
	}

	for _, a := range gotArgs {
		if a == "-march=armv8-a" {
			t.Errorf("non-musl host AS must not carry -march=armv8-a")
		}

		if a == "-D_musl_=1" {
			t.Errorf("non-musl host AS must not carry -D_musl_=1")
		}
	}

	if !got.HostPlatform {
		t.Errorf("host_platform: got false, want true")
	}

	if len(got.Tags) != 1 || got.Tags[0] != "tool" {
		t.Errorf("tags = %v, want [\"tool\"]", got.Tags)
	}

	t.Logf("cmd_args length = %d (reference = %d)", len(gotArgs), len(wantArgs))
}

// TestEmitAS_UtilContext_ByteExact (PR-35i / PR-33-C2_06 closure;
// PR-35m generic threading) pins the cmd_args bundle for util's only
// AS node (`$(BUILD_ROOT)/util/_/system/context_aarch64.S.o`) against
// the reference graph. Total 106 args. util declares no
// `NO_COMPILER_WARNINGS()` macro, so the warning bundle is the full
// `-Werror`/`-Wall`/`-Wextra` set (NOT `-Wno-everything`); util's own
// non-GLOBAL `CFLAGS(-Wnarrowing)` (util/ya.make:243) sits between
// commonDefines and the first noLibcUndebugBlock copy; the consumer-
// side `-D_musl_` sentinel sits between catboost and the second
// noLibcUndebugBlock copy; the include tail (13 args) carries util's
// linux-headers + runtime-stack + user-PEERDIR ADDINCL contributions.
//
// Verifies that:
//
//   - target triple is aarch64-linux-gnu with -march=armv8-a.
//   - warning bundle is `warningFlags` (6 args, NOT `-Wno-everything`).
//   - own CFLAG `-Wnarrowing` is present at the post-commonDefines slot.
//   - `-D_musl_` (NOT `-D_musl_=1`) is present at the post-catboost slot.
//   - includes tail matches the 13-arg reference set.
//
// PR-35m: the per-module compile knobs are now passed via the same
// `ModuleCCInputs` struct CC consumes (own AddIncl empty for util,
// peer-GLOBAL = libcxx/libcxxrt + musl-arch-aarch64×4 + the user-
// PEERDIR contributions, own CFlags = `-Wnarrowing`, AutoPeerCFlags =
// `-D_musl_`). The util-specific path-sniff stopgap is retired.
func TestEmitAS_UtilContext_ByteExact(t *testing.T) {
	const targetOut = "$(BUILD_ROOT)/util/_/system/context_aarch64.S.o"

	raw, err := os.ReadFile(referenceGraphPath)

	if err != nil {
		t.Skipf("reference graph not available (%v); skipping util AS byte-exact test", err)
	}

	var g Graph
	Throw(json.Unmarshal(raw, &g))

	var ref *Node

	for _, n := range g.Graph {
		if len(n.Outputs) > 0 && n.Outputs[0] == targetOut {
			ref = n

			break
		}
	}

	if ref == nil {
		t.Fatalf("reference util AS node with output %q not found", targetOut)
	}

	emit := NewBufferedEmitter()

	// util declares only NO_UTIL — no NO_COMPILER_WARNINGS, no
	// LibcMusl. inferFlagsFromPath returns a zero-valued FlagSet for
	// `util` (the path-prefix branches are for build/cow/on and
	// contrib/libs/musl only), which is what the real walker presents
	// to EmitAS for util.
	utilInstance := targetInstance("util")

	// PR-35m: thread util's compile knobs through ModuleCCInputs as
	// the production walker does. `-Wnarrowing` (own non-GLOBAL CFLAG
	// from util/ya.make:243's IF (GCC OR CLANG OR CLANG_CL) block);
	// `-D_musl_` (auto peer CFLAG from defaultPeerCFlags); peer-GLOBAL
	// AddIncl in declaration order (libcxx/libcxxrt + musl arch+include
	// + user-PEERDIR zlib/double-conversion/libc_compat).
	utilIn := ModuleCCInputs{
		CFlags:         []string{"-Wnarrowing"},
		AutoPeerCFlags: []string{"-D_musl_"},
		PeerAddInclGlobal: []string{
			"contrib/libs/cxxsupp/libcxx/include",
			"contrib/libs/cxxsupp/libcxxrt/include",
			"contrib/libs/musl/arch/aarch64",
			"contrib/libs/musl/arch/generic",
			"contrib/libs/musl/include",
			"contrib/libs/musl/extra",
			"contrib/libs/zlib/include",
			"contrib/libs/double-conversion",
			"contrib/libs/libc_compat/include/readpassphrase",
		},
	}
	_, outPath := EmitAS(utilInstance, "system/context_aarch64.S", utilIn, nil, emit)

	if outPath != targetOut {
		t.Errorf("outPath = %q, want %q", outPath, targetOut)
	}

	if len(emit.nodes) != 1 {
		t.Fatalf("emitter buffered %d nodes, want 1", len(emit.nodes))
	}

	got := emit.nodes[0]
	wantArgs := ref.Cmds[0].CmdArgs

	if len(got.Cmds[0].CmdArgs) != len(wantArgs) {
		t.Fatalf("cmd_args length = %d, want %d", len(got.Cmds[0].CmdArgs), len(wantArgs))
	}

	for i := range wantArgs {
		if got.Cmds[0].CmdArgs[i] != wantArgs[i] {
			t.Errorf("cmd_args[%d]:\n  got:  %q\n  want: %q", i, got.Cmds[0].CmdArgs[i], wantArgs[i])
		}
	}

	if got.HostPlatform {
		t.Errorf("host_platform: got true, want false (util AS is target-side)")
	}

	if got.Platform != string(PlatformDefaultLinuxAArch64) {
		t.Errorf("platform = %q, want %q", got.Platform, PlatformDefaultLinuxAArch64)
	}

	t.Logf("cmd_args length = %d (reference = %d)", len(got.Cmds[0].CmdArgs), len(wantArgs))
}

// TestEmitAS_AsmlibYasm_Cachesize_ByteExact (PR-35q) pins the yasm-
// toolchain shape for asmlib's host-PIC `.asm` AS nodes against the
// reference graph (`$(BUILD_ROOT)/contrib/libs/asmlib/cachesize64.pic.o`).
//
// Verifies the four shape divergences from the clang AS path:
//
//   - Output path is flat (`<modulePath>/<base>.pic.o`; no `_/` infix,
//     `.asm` suffix stripped).
//   - cmd_args is the 18-arg yasm invocation (NOT a 94/98/106/109-arg
//     clang AS bundle).
//   - Cwd is empty (the reference omits the `cwd` field for all 25
//     asmlib yasm AS nodes; PR-35q must not set `Cwd: $(BUILD_ROOT)`).
//   - Env is `ARCADIA_ROOT_DISTBUILD` + `YASM_TEST_SUITE` (no
//     `DYLD_LIBRARY_PATH`).
//
// Inputs ordering (yasm binary at index 0) and downstream wiring
// (DepRefs + ForeignDepRefs["tool"]) are pinned alongside.
func TestEmitAS_AsmlibYasm_Cachesize_ByteExact(t *testing.T) {
	const targetOut = "$(BUILD_ROOT)/contrib/libs/asmlib/cachesize64.pic.o"

	raw, err := os.ReadFile(referenceGraphPath)

	if err != nil {
		t.Skipf("reference graph not available (%v); skipping asmlib yasm AS byte-exact test", err)
	}

	var g Graph
	Throw(json.Unmarshal(raw, &g))

	var ref *Node

	for _, n := range g.Graph {
		if len(n.Outputs) > 0 && n.Outputs[0] == targetOut {
			ref = n

			break
		}
	}

	if ref == nil {
		t.Fatalf("reference asmlib yasm AS node with output %q not found", targetOut)
	}

	emit := NewBufferedEmitter()

	// Stand-in yasm LD ref. Identity is the only thing that matters —
	// the AS node references it via foreign_deps.tool + deps.
	yasmLDRef := emit.Emit(&Node{
		Cmds:         []Cmd{{CmdArgs: []string{"yasm"}, Env: map[string]string{}}},
		Env:          map[string]string{},
		Inputs:       []string{},
		Outputs:      []string{"$(BUILD_ROOT)/contrib/tools/yasm/yasm"},
		KV:           map[string]string{"p": "LD", "pc": "light-cyan"},
		Tags:         []string{"tool"},
		Platform:     string(PlatformDefaultLinuxX8664),
		Requirements: map[string]interface{}{"cpu": float64(1), "network": "restricted", "ram": float64(32)},
		TargetProperties: map[string]string{
			"module_dir": "contrib/tools/yasm",
		},
	})

	// asmlib host walk: PIC=true (host), instance.Path matches
	// asmlibYasmModules. Includes scanned by gen.go include defs.asm —
	// pre-load it here so the inputs slice the emitter produces is
	// what the production walker would emit.
	asmlibInstance := hostInstance("contrib/libs/asmlib")
	asmlibIn := ModuleCCInputs{
		IncludeInputs: []string{"$(SOURCE_ROOT)/contrib/libs/asmlib/defs.asm"},
	}
	_, outPath := EmitAS(asmlibInstance, "cachesize64.asm", asmlibIn, &yasmLDRef, emit)

	if outPath != targetOut {
		t.Errorf("outPath = %q, want %q", outPath, targetOut)
	}

	// AS node sits at index 1 (yasmLD is at index 0).
	if len(emit.nodes) != 2 {
		t.Fatalf("emitter buffered %d nodes, want 2", len(emit.nodes))
	}

	got := emit.nodes[1]
	wantArgs := ref.Cmds[0].CmdArgs

	if len(got.Cmds[0].CmdArgs) != len(wantArgs) {
		t.Fatalf("cmd_args length = %d, want %d", len(got.Cmds[0].CmdArgs), len(wantArgs))
	}

	for i := range wantArgs {
		if got.Cmds[0].CmdArgs[i] != wantArgs[i] {
			t.Errorf("cmd_args[%d]:\n  got:  %q\n  want: %q", i, got.Cmds[0].CmdArgs[i], wantArgs[i])
		}
	}

	// Cwd must be empty (reference omits the field).
	if got.Cmds[0].Cwd != "" {
		t.Errorf("Cmds[0].Cwd = %q, want empty", got.Cmds[0].Cwd)
	}

	if ref.Cmds[0].Cwd != "" {
		t.Errorf("reference Cmds[0].Cwd = %q, want empty (sanity check)", ref.Cmds[0].Cwd)
	}

	// Env (per-cmd and top-level) must match exactly.
	fieldEqual(t, "cmds[0].env", got.Cmds[0].Env, ref.Cmds[0].Env)
	fieldEqual(t, "env (top-level)", got.Env, ref.Env)
	fieldEqual(t, "outputs", got.Outputs, ref.Outputs)
	fieldEqual(t, "tags", got.Tags, ref.Tags)
	fieldEqual(t, "kv", got.KV, ref.KV)
	fieldEqual(t, "target_properties", got.TargetProperties, ref.TargetProperties)
	fieldEqual(t, "platform", got.Platform, ref.Platform)
	fieldEqual(t, "requirements", got.Requirements, ref.Requirements)

	// host_platform must be true.
	if !got.HostPlatform {
		t.Errorf("host_platform: got false, want true")
	}

	if !ref.HostPlatform {
		t.Errorf("reference host_platform: got false, want true (sanity check)")
	}

	// Inputs: yasm binary at index 0, source at index 1, defs.asm at 2.
	wantInputs := []string{
		"$(BUILD_ROOT)/contrib/tools/yasm/yasm",
		"$(SOURCE_ROOT)/contrib/libs/asmlib/cachesize64.asm",
		"$(SOURCE_ROOT)/contrib/libs/asmlib/defs.asm",
	}
	fieldEqual(t, "inputs", got.Inputs, wantInputs)
	fieldEqual(t, "inputs (vs reference)", got.Inputs, ref.Inputs)

	// DepRefs + ForeignDepRefs["tool"] must contain the yasmLD ref
	// (PR-30 D02).
	if len(got.DepRefs) != 1 || got.DepRefs[0] != yasmLDRef {
		t.Errorf("DepRefs = %v, want [%v]", got.DepRefs, yasmLDRef)
	}

	toolRefs := got.ForeignDepRefs["tool"]
	if len(toolRefs) != 1 || toolRefs[0] != yasmLDRef {
		t.Errorf(`ForeignDepRefs["tool"] = %v, want [%v]`, toolRefs, yasmLDRef)
	}

	t.Logf("cmd_args length = %d (reference = %d)", len(got.Cmds[0].CmdArgs), len(wantArgs))
}

// TestEmitAS_AsmlibYasm_OutputPath_NoUnderscoreInfix (PR-35q) verifies
// that asmlib host-PIC `.asm` AS nodes use the FLAT output path
// (`<base>.pic.o`) without the `_/` infix that the clang AS path
// applies unconditionally. This is the inverse of
// TestEmitAS_OutputPath_AlwaysHasUnderscore — the asmlib yasm branch
// is the documented exception to the clang-AS unconditional infix
// rule.
func TestEmitAS_AsmlibYasm_OutputPath_NoUnderscoreInfix(t *testing.T) {
	e := NewBufferedEmitter()
	_, outPath := EmitAS(hostInstance("contrib/libs/asmlib"), "memset64.asm", ModuleCCInputs{}, nil, e)
	want := "$(BUILD_ROOT)/contrib/libs/asmlib/memset64.pic.o"

	if outPath != want {
		t.Errorf("outPath = %q, want %q", outPath, want)
	}
}

// TestEmitAS_AsmlibYasm_TargetSide_NoYasmBranch (PR-35q) verifies that
// the yasm branch fires ONLY for host-PIC asmlib invocations. A
// hypothetical target-side asmlib AS (PIC=false) must take the clang
// AS path — the `_/<srcRel>.o` output, the clang cmd_args bundle, the
// `Cwd: $(BUILD_ROOT)`. The asmlib reference graph contains no such
// target-side node (asmlib is host-only by construction), but
// defending the predicate against PIC=false is the cheapest way to
// guarantee the branch never accidentally hijacks a future target AS
// node living under a similarly-named module path.
func TestEmitAS_AsmlibYasm_TargetSide_NoYasmBranch(t *testing.T) {
	e := NewBufferedEmitter()
	// PIC=false → target-side. Even though asmlibYasmModules matches
	// instance.Path, the predicate is gated on PIC=true.
	_, outPath := EmitAS(targetInstance("contrib/libs/asmlib"), "memset64.asm", ModuleCCInputs{}, nil, e)
	// PR-35r: flat srcRel → flat output path (no _/ infix). memset64.asm
	// has no "/" so the clang AS path emits a flat output.
	wantClangPath := "$(BUILD_ROOT)/contrib/libs/asmlib/memset64.asm.o"

	if outPath != wantClangPath {
		t.Errorf("outPath = %q, want %q (clang AS path; yasm branch must not fire for target-side)", outPath, wantClangPath)
	}

	if len(e.nodes) != 1 {
		t.Fatalf("emitter buffered %d nodes, want 1", len(e.nodes))
	}

	got := e.nodes[0]

	// Clang path sets Cwd; yasm path leaves it empty. A non-empty Cwd
	// confirms the clang branch ran.
	if got.Cmds[0].Cwd != "$(BUILD_ROOT)" {
		t.Errorf("Cmds[0].Cwd = %q, want $(BUILD_ROOT) (clang AS path)", got.Cmds[0].Cwd)
	}
}
