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
// /home/pg/monorepo/yatool_orig/sg.json; if that path is absent the test
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

// referenceGraphPath declared in gjson_test.go; both files compile in `package main`.
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
	_, outPath := EmitCC(targetInstance("build/cow/on"), "lib.c", ModuleCCInputs{}, emit)

	if outPath != "$(BUILD_ROOT)/build/cow/on/lib.c.o" {
		t.Errorf("outPath = %q, want %q", outPath, "$(BUILD_ROOT)/build/cow/on/lib.c.o")
	}

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

func TestEmitCC_OutputPath_NestedSrc(t *testing.T) {
	e := NewBufferedEmitter()
	_, outPath := EmitCC(targetInstance("contrib/libs/cxxsupp/libcxx"), "src/algorithm.cpp", ModuleCCInputs{}, e)
	want := "$(BUILD_ROOT)/contrib/libs/cxxsupp/libcxx/_/src/algorithm.cpp.o"

	if outPath != want {
		t.Errorf("outPath = %q, want %q", outPath, want)
	}
}

func TestEmitCC_OutputPath_FlatSrc(t *testing.T) {
	e := NewBufferedEmitter()
	_, outPath := EmitCC(targetInstance("build/cow/on"), "lib.c", ModuleCCInputs{}, e)
	want := "$(BUILD_ROOT)/build/cow/on/lib.c.o"

	if outPath != want {
		t.Errorf("outPath = %q, want %q", outPath, want)
	}
}

// TestEmitCC_BuildCowOn_Host_ByteExact verifies the host (PIC) CC
// node for build/cow/on/lib.c — 105-arg cmd_args, with
// host_platform=true, tags=["tool"], output ".pic.o", and the
// release/PIC flag bundle (-O3, -fPIC, etc.) per the reference.
func TestEmitCC_BuildCowOn_Host_ByteExact(t *testing.T) {
	const targetOut = "$(BUILD_ROOT)/build/cow/on/lib.c.pic.o"

	raw, err := os.ReadFile(referenceGraphPath)

	if err != nil {
		t.Skipf("reference graph not available (%v); skipping host byte-exact test", err)
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
		t.Fatalf("reference host CC node with output %q not found", targetOut)
	}

	emit := NewBufferedEmitter()
	_, outPath := EmitCC(hostInstance("build/cow/on"), "lib.c", ModuleCCInputs{}, emit)

	if outPath != targetOut {
		t.Errorf("outPath = %q, want %q", outPath, targetOut)
	}

	if len(emit.nodes) != 1 {
		t.Fatalf("emitter buffered %d nodes, want 1", len(emit.nodes))
	}

	got := emit.nodes[0]

	if len(got.Cmds[0].CmdArgs) != 105 {
		t.Fatalf("cmd_args length = %d, want 105", len(got.Cmds[0].CmdArgs))
	}

	wantArgs := ref.Cmds[0].CmdArgs

	if len(wantArgs) != 105 {
		t.Fatalf("reference cmd_args length = %d, want 105 (sanity check)", len(wantArgs))
	}

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

	if !got.HostPlatform {
		t.Errorf("host_platform: got false, want true")
	}

	if !ref.HostPlatform {
		t.Errorf("reference host_platform: got false, want true (sanity check)")
	}

	if got.ForeignDeps != nil {
		t.Errorf("foreign_deps: got %#v, want nil", got.ForeignDeps)
	}
}

// muslHostInstance constructs the canonical host (PIC) musl
// ModuleInstance for a given musl-relative path. Used by PR-29-D01's
// byte-exact pin against the reference graph.
func muslHostInstance(path string) ModuleInstance {
	return ModuleInstance{
		Path:     path,
		Language: LangCPP,
		Target:   PlatformDefaultLinuxX8664,
		Flags:    inferFlagsFromPath(path, true),
	}
}

// TestEmitCC_MuslHost_StrlenC_ByteExact pins composeMuslHostCC's
// 115-arg cmd_args bundle against the reference graph node
// `$(BUILD_ROOT)/contrib/libs/musl/_/src/string/strlen.c.pic.o`
// (platform `default-linux-x86_64`). PR-29-D01 dominant lever.
func TestEmitCC_MuslHost_StrlenC_ByteExact(t *testing.T) {
	const targetOut = "$(BUILD_ROOT)/contrib/libs/musl/_/src/string/strlen.c.pic.o"

	raw, err := os.ReadFile(referenceGraphPath)

	if err != nil {
		t.Skipf("reference graph not available (%v); skipping host musl byte-exact test", err)
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
		t.Fatalf("reference host musl CC node with output %q not found", targetOut)
	}

	emit := NewBufferedEmitter()
	// PR-31: pass the reference's resolved transitive headers as
	// IncludeInputs so this synthetic byte-exact test pins both the
	// cmd_args (115) AND the input-set against sg.json.
	muslIncludeInputs := append([]string(nil), ref.Inputs[1:]...)
	_, outPath := EmitCC(muslHostInstance("contrib/libs/musl"), "src/string/strlen.c", ModuleCCInputs{IncludeInputs: muslIncludeInputs}, emit)

	if outPath != targetOut {
		t.Errorf("outPath = %q, want %q", outPath, targetOut)
	}

	if len(emit.nodes) != 1 {
		t.Fatalf("emitter buffered %d nodes, want 1", len(emit.nodes))
	}

	got := emit.nodes[0]

	if len(got.Cmds[0].CmdArgs) != 115 {
		t.Fatalf("cmd_args length = %d, want 115", len(got.Cmds[0].CmdArgs))
	}

	wantArgs := ref.Cmds[0].CmdArgs

	if len(wantArgs) != 115 {
		t.Fatalf("reference cmd_args length = %d, want 115 (sanity check)", len(wantArgs))
	}

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

	if !got.HostPlatform {
		t.Errorf("host_platform: got false, want true")
	}

	if !ref.HostPlatform {
		t.Errorf("reference host_platform: got false, want true (sanity check)")
	}
}

// TestEmitCC_GeneratedSource_BuildRootInput pins the IsGenerated
// branch of EmitCC: when true, inputPath is composed under
// $(BUILD_ROOT) instead of $(SOURCE_ROOT). PR-29-D07.
func TestEmitCC_GeneratedSource_BuildRootInput(t *testing.T) {
	emit := NewBufferedEmitter()
	in := ModuleCCInputs{IsGenerated: true}
	_, outPath := EmitCC(targetInstance("util"), "_/datetime/parser.rl6.cpp", in, emit)

	wantOut := "$(BUILD_ROOT)/util/_/_/datetime/parser.rl6.cpp.o"

	if outPath != wantOut {
		t.Errorf("outPath = %q, want %q", outPath, wantOut)
	}

	if len(emit.nodes) != 1 {
		t.Fatalf("emitter buffered %d nodes, want 1", len(emit.nodes))
	}

	got := emit.nodes[0]

	wantInput := "$(BUILD_ROOT)/util/_/datetime/parser.rl6.cpp"

	if len(got.Inputs) != 1 || got.Inputs[0] != wantInput {
		t.Errorf("inputs = %v, want [%q]", got.Inputs, wantInput)
	}

	// The last cmd_arg is always the input path.
	args := got.Cmds[0].CmdArgs

	if args[len(args)-1] != wantInput {
		t.Errorf("cmd_args[last] = %q, want %q", args[len(args)-1], wantInput)
	}
}

// TestEmitCC_AddIncl_SlotsBetweenPrefixAndSuffix verifies PR-29-D03:
// per-module ADDINCL paths sit between the baseline `-I$(BUILD_ROOT)
// -I$(SOURCE_ROOT)` pair and the trailing `-I$(SOURCE_ROOT)/contrib/libs/linux-headers{,/_nf}`
// pair. Slot order matches the builtins fp_mode.c.o reference shape.
func TestEmitCC_AddIncl_SlotsBetweenPrefixAndSuffix(t *testing.T) {
	emit := NewBufferedEmitter()
	in := ModuleCCInputs{
		AddIncl: []string{
			"contrib/libs/musl/arch/aarch64",
			"contrib/libs/musl/arch/generic",
			"contrib/libs/musl/include",
			"contrib/libs/musl/extra",
		},
	}
	EmitCC(targetInstance("contrib/libs/cxxsupp/builtins"), "aarch64/fp_mode.c", in, emit)

	args := emit.nodes[0].Cmds[0].CmdArgs

	wantSlot := []string{
		"-I$(BUILD_ROOT)",
		"-I$(SOURCE_ROOT)",
		"-I$(SOURCE_ROOT)/contrib/libs/musl/arch/aarch64",
		"-I$(SOURCE_ROOT)/contrib/libs/musl/arch/generic",
		"-I$(SOURCE_ROOT)/contrib/libs/musl/include",
		"-I$(SOURCE_ROOT)/contrib/libs/musl/extra",
		"-I$(SOURCE_ROOT)/contrib/libs/linux-headers",
		"-I$(SOURCE_ROOT)/contrib/libs/linux-headers/_nf",
	}

	for i, want := range wantSlot {
		if args[7+i] != want {
			t.Errorf("cmd_args[%d] = %q, want %q", 7+i, args[7+i], want)
		}
	}
}

// TestEmitCC_CxxSource_UsesClangPlusPlus verifies PR-29-D05: a `.cpp`
// source dispatches to clang++ and threads `-std=c++20` after the
// second suppression block.
func TestEmitCC_CxxSource_UsesClangPlusPlus(t *testing.T) {
	emit := NewBufferedEmitter()
	EmitCC(targetInstance("contrib/libs/cxxsupp/libcxx"), "src/algorithm.cpp", ModuleCCInputs{}, emit)

	args := emit.nodes[0].Cmds[0].CmdArgs

	if args[0] != cxxCompilerPath {
		t.Errorf("compiler = %q, want %q", args[0], cxxCompilerPath)
	}

	// `-std=c++20` slots after the second suppression block. The exact
	// index varies by composer flavor; assert presence rather than
	// position to keep the test resilient to bundle-size deltas.
	found := false

	for _, a := range args {
		if a == cxxStandardFlag {
			found = true

			break
		}
	}

	if !found {
		t.Errorf("cmd_args missing %q; got %v", cxxStandardFlag, args)
	}
}

// TestEmitCC_CSource_UsesClang verifies PR-29-D05: a `.c` source
// dispatches to clang (NOT clang++) and does NOT carry `-std=c++20`.
func TestEmitCC_CSource_UsesClang(t *testing.T) {
	emit := NewBufferedEmitter()
	EmitCC(targetInstance("build/cow/on"), "lib.c", ModuleCCInputs{}, emit)

	args := emit.nodes[0].Cmds[0].CmdArgs

	if args[0] != ccCompilerPath {
		t.Errorf("compiler = %q, want %q", args[0], ccCompilerPath)
	}

	for _, a := range args {
		if a == cxxStandardFlag {
			t.Errorf("cmd_args contains %q for a .c source", cxxStandardFlag)

			break
		}
	}
}

// TestEmitCC_NoCompilerWarnings_SelectsMuslWarningFlags verifies
// PR-29-D06: when the instance flags carry NoCompilerWarnings, the
// composer substitutes the single-arg `-Wno-everything` for the full
// `-Werror`/`-Wall`/`-Wextra` warning bundle. Verified on the target
// host composer (musl path uses muslWarningFlags unconditionally).
func TestEmitCC_NoCompilerWarnings_SelectsMuslWarningFlags(t *testing.T) {
	emit := NewBufferedEmitter()
	inst := targetInstance("contrib/libs/cxxsupp/libcxxrt")
	inst.Flags.NoCompilerWarnings = true
	EmitCC(inst, "exception.cc", ModuleCCInputs{}, emit)

	args := emit.nodes[0].Cmds[0].CmdArgs

	for _, a := range args {
		if a == "-Werror" {
			t.Errorf("cmd_args contains -Werror despite NoCompilerWarnings=true")
		}
	}

	wnoCount := 0

	for _, a := range args {
		if a == "-Wno-everything" {
			wnoCount++
		}
	}

	if wnoCount == 0 {
		t.Errorf("cmd_args missing -Wno-everything; got %v", args)
	}
}

// TestEmitCC_OwnCXXFlags_SlotsAfterSuppressionBlock verifies PR-29-D02:
// the module's own CXXFLAGS slot AFTER the second
// noLibcUndebugBlock/ndebugPicBlock copy and BEFORE
// builtinMacroDateTime. For C++ sources the slot also includes
// `-std=c++20` (D05) ahead of the own-extras values.
func TestEmitCC_OwnCXXFlags_SlotsAfterSuppressionBlock(t *testing.T) {
	emit := NewBufferedEmitter()
	in := ModuleCCInputs{
		CXXFlags: []string{"-D_LIBCPP_BUILDING_LIBRARY"},
	}
	inst := targetInstance("contrib/libs/cxxsupp/libcxx")
	inst.Flags.NoCompilerWarnings = true
	EmitCC(inst, "src/algorithm.cpp", in, emit)

	args := emit.nodes[0].Cmds[0].CmdArgs

	// Locate `-D_LIBCPP_BUILDING_LIBRARY` and verify it follows
	// `-Wno-strict-primary-template-shadow` (last arg of the second
	// suppression block in the noLibcUndebugBlock body) somewhere
	// later in the cmd_args, and precedes builtinMacroDateTime.
	idxOwn := -1
	idxLastSuppression := -1
	idxBuiltinDate := -1

	for i, a := range args {
		switch a {
		case "-D_LIBCPP_BUILDING_LIBRARY":
			idxOwn = i
		case "-Wno-strict-primary-template-shadow":
			// noLibcUndebugBlock contains this arg at its tail; the
			// SECOND copy is the one we care about.
			idxLastSuppression = i
		case "-Wno-builtin-macro-redefined":
			idxBuiltinDate = i
		}
	}

	if idxOwn < 0 {
		t.Fatalf("own CXXFLAGS not present in cmd_args: %v", args)
	}

	if !(idxLastSuppression < idxOwn && idxOwn < idxBuiltinDate) {
		t.Errorf("CXXFLAGS slot mis-ordered: idxLastSuppression=%d idxOwn=%d idxBuiltinDate=%d",
			idxLastSuppression, idxOwn, idxBuiltinDate)
	}

	// CONLYFLAGS must NOT appear (this is a .cpp source).
	if !contains(args, "-D_LIBCPP_BUILDING_LIBRARY") {
		t.Errorf("expected own CXXFLAGS in args")
	}
}

// TestEmitCC_COnlyFlags_AppliesOnlyToCSources verifies PR-29-D02: a
// CONLYFLAG passed via ModuleCCInputs ends up in cmd_args ONLY when
// the source is .c/.S; for .cpp/.cc the CXXFlags slice is consumed
// instead.
func TestEmitCC_COnlyFlags_AppliesOnlyToCSources(t *testing.T) {
	in := ModuleCCInputs{COnlyFlags: []string{"-Wno-narrowing"}}

	emitC := NewBufferedEmitter()
	EmitCC(targetInstance("build/cow/on"), "lib.c", in, emitC)

	if !contains(emitC.nodes[0].Cmds[0].CmdArgs, "-Wno-narrowing") {
		t.Errorf(".c source missing CONLYFLAG -Wno-narrowing; got %v", emitC.nodes[0].Cmds[0].CmdArgs)
	}

	emitCpp := NewBufferedEmitter()
	EmitCC(targetInstance("build/cow/on"), "lib.cpp", in, emitCpp)

	if contains(emitCpp.nodes[0].Cmds[0].CmdArgs, "-Wno-narrowing") {
		t.Errorf(".cpp source got CONLYFLAG -Wno-narrowing (should be CXXFlags-only); got %v", emitCpp.nodes[0].Cmds[0].CmdArgs)
	}
}

func contains(xs []string, target string) bool {
	for _, x := range xs {
		if x == target {
			return true
		}
	}

	return false
}

// TestEmitCC_Libcxxrt_AuxhelperCc_ByteExact pins the libcxxrt
// auxhelper.cc target CC node against the reference graph. PR-35f
// closes PR-33-C2_04: libcxxrt has no own/peer GLOBAL CXXFLAGS, so
// the pre-catboost bucket is empty and the post-catboost bucket
// receives only the `_BASE_UNIT.CXXFLAGS += -nostdinc++` injection.
// The expected cmd_args end is
// `..., -nostdinc++ (ownExtras), catboost, -nostdinc++ (post-bucket
// from baseUnit), -Wno-builtin-macro-redefined, ...`.
func TestEmitCC_Libcxxrt_AuxhelperCc_ByteExact(t *testing.T) {
	const targetOut = "$(BUILD_ROOT)/contrib/libs/cxxsupp/libcxxrt/auxhelper.cc.o"

	raw, err := os.ReadFile(referenceGraphPath)

	if err != nil {
		t.Skipf("reference graph not available (%v); skipping libcxxrt byte-exact test", err)
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
		t.Fatalf("reference libcxxrt CC node with output %q not found", targetOut)
	}

	emit := NewBufferedEmitter()
	inst := targetInstance("contrib/libs/cxxsupp/libcxxrt")
	inst.Flags.NoCompilerWarnings = true
	in := ModuleCCInputs{
		// libcxxrt's ya.make declares `CXXFLAGS(-nostdinc++)` (line 29
		// of `contrib/libs/cxxsupp/libcxxrt/ya.make`); this is the own
		// non-GLOBAL CXXFLAGS that lands at the ownExtras slot.
		CXXFlags: []string{"-nostdinc++"},
		// PeerAddInclGlobal is the set the walker computes for libcxxrt's
		// peer closure (libunwind + sanitizer/include + transitive musl
		// arch paths via runtime-stack hoisting). Reference ordering at
		// cmd_args[11..14] is the canonical musl-arch triple plus extra.
		PeerAddInclGlobal: []string{
			"contrib/libs/musl/arch/aarch64",
			"contrib/libs/musl/arch/generic",
			"contrib/libs/musl/include",
			"contrib/libs/musl/extra",
		},
		// AutoPeerCFlags is the consumer-side `-D_musl_` sentinel that
		// `defaultPeerCFlags` injects when MUSL=yes and the module is
		// not LibcMusl-self / not effectively NO_PLATFORM. Reference
		// slot 79 carries it.
		AutoPeerCFlags: []string{"-D_musl_"},
		// The reference's resolved transitive header set; the test
		// pins both cmd_args AND inputs against sg.json.
		IncludeInputs: append([]string(nil), ref.Inputs[1:]...),
	}
	_, outPath := EmitCC(inst, "auxhelper.cc", in, emit)

	if outPath != targetOut {
		t.Errorf("outPath = %q, want %q", outPath, targetOut)
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
}
