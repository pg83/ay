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

// TestEmitAS_OutputPath_AlwaysHasUnderscore verifies that the _/ infix
// is unconditional for AS nodes (D29), even for sources with no directory
// component — unlike CC which uses the flat formula for flat sources.
func TestEmitAS_OutputPath_AlwaysHasUnderscore(t *testing.T) {
	e := NewBufferedEmitter()
	_, outPath := EmitAS(targetInstance("some/module"), "flat.S", ModuleCCInputs{}, nil, e)
	want := "$(BUILD_ROOT)/some/module/_/flat.S.o"

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
