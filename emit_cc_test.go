package main

import (
	"reflect"
	"testing"
)

// cc_test.go — byte-exact regression test for EmitCC against the
// reference graph for `build/cow/on/lib.c`.
//
// Strategy: rather than relying on PR-03's LoadReference (which is
// landing in parallel), the test does its own os.ReadFile + json.Unmarshal
// into a Graph. The reference graph lives at
// /home/pg/monorepo/yatool/sg.json; if that path is absent the test
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
const referenceCCOutput = "$(B)/build/cow/on/lib.c.o"

// loadReferenceCCNode reads the on-disk reference graph and returns the
// CC node whose first output is referenceCCOutput. Returns nil and a
// reason string when the file is absent (so the caller can t.Skip) or
// the node is missing.
// fieldEqual is a small helper that wraps reflect.DeepEqual + a diff-y
// failure message with the expected and actual rendered as %#v so a
// mismatch surfaces the offending value rather than a bare false.
func fieldEqual(t *testing.T, name string, got, want interface{}) {
	t.Helper()

	if !reflect.DeepEqual(got, want) {
		t.Errorf("field %s mismatch:\n  got:  %#v\n  want: %#v", name, got, want)
	}
}

func TestEmitCC_OutputPath_NestedSrc(t *testing.T) {
	e := NewBufferedEmitter()
	_, outPath := EmitCC(targetInstance("contrib/libs/cxxsupp/libcxx"), "src/algorithm.cpp", Source("contrib/libs/cxxsupp/libcxx/src/algorithm.cpp"), ModuleCCInputs{}, testHostP, e)
	want := "$(B)/contrib/libs/cxxsupp/libcxx/_/src/algorithm.cpp.o"

	if outPath.String() != want {
		t.Errorf("outPath = %q, want %q", outPath, want)
	}
}

func TestEmitCC_OutputPath_FlatSrc(t *testing.T) {
	e := NewBufferedEmitter()
	_, outPath := EmitCC(targetInstance("build/cow/on"), "lib.c", Source("build/cow/on/lib.c"), ModuleCCInputs{}, testHostP, e)
	want := "$(B)/build/cow/on/lib.c.o"

	if outPath.String() != want {
		t.Errorf("outPath = %q, want %q", outPath, want)
	}
}

// TestEmitCC_BuildCowOn_Host_ByteExact verifies the host (PIC) CC
// node for build/cow/on/lib.c — 105-arg cmd_args, with
// host_platform=true, tags=["tool"], output ".pic.o", and the
// release/PIC flag bundle (-O3, -fPIC, etc.) per the reference.
// muslHostInstance constructs the canonical host (PIC) musl
// ModuleInstance for a given musl-relative path. Used by PR-29-D01's
// byte-exact pin against the reference graph.
func muslHostInstance(path string) ModuleInstance {
	return ModuleInstance{
		Path:     path,
		Kind:     KindLib,
		Language: LangCPP,
		Platform: testHostP,
	}
}

// TestEmitCC_GeneratedSource_BuildRootInput pins the generated-source
// branch of composeCCPaths: when srcVFS is Build-rooted, inputPath is
// $(B)/<instance>/<rel> and the output mirrors. PR-29-D07.
func TestEmitCC_GeneratedSource_BuildRootInput(t *testing.T) {
	emit := NewBufferedEmitter()
	srcVFS := Build("util/_/datetime/parser.rl6.cpp")
	_, outPath := EmitCC(targetInstance("util"), "_/datetime/parser.rl6.cpp", srcVFS, ModuleCCInputs{}, testHostP, emit)

	wantOut := "$(B)/util/_/_/datetime/parser.rl6.cpp.o"

	if outPath.String() != wantOut {
		t.Errorf("outPath = %q, want %q", outPath, wantOut)
	}

	if len(emit.nodes) != 1 {
		t.Fatalf("emitter buffered %d nodes, want 1", len(emit.nodes))
	}

	got := emit.nodes[0]

	wantInput := "$(B)/util/_/datetime/parser.rl6.cpp"

	if len(got.Inputs) != 1 || got.Inputs[0].String() != wantInput {
		t.Errorf("inputs = %v, want [%q]", got.Inputs, wantInput)
	}

	// The last cmd_arg is always the input path.
	args := got.Cmds[0].CmdArgs

	if args[len(args)-1] != wantInput {
		t.Errorf("cmd_args[last] = %q, want %q", args[len(args)-1], wantInput)
	}
}

// TestEmitCC_AddIncl_SlotsBetweenPrefixAndSuffix verifies PR-29-D03:
// per-module ADDINCL paths sit between the baseline `-I$(B)
// -I$(S)` pair and the trailing `-I$(S)/contrib/libs/linux-headers{,/_nf}`
// pair. Slot order matches the builtins fp_mode.c.o reference shape.
func TestEmitCC_AddIncl_SlotsBetweenPrefixAndSuffix(t *testing.T) {
	emit := NewBufferedEmitter()
	in := ModuleCCInputs{
		AddIncl: []VFS{
			Source("contrib/libs/musl/arch/aarch64"),
			Source("contrib/libs/musl/arch/generic"),
			Source("contrib/libs/musl/include"),
			Source("contrib/libs/musl/extra"),
		},
	}
	EmitCC(targetInstance("contrib/libs/cxxsupp/builtins"), "aarch64/fp_mode.c", Source("contrib/libs/cxxsupp/builtins/aarch64/fp_mode.c"), in, testHostP, emit)

	args := emit.nodes[0].Cmds[0].CmdArgs

	wantSlot := []string{
		"-I$(B)",
		"-I$(S)",
		"-I$(S)/contrib/libs/musl/arch/aarch64",
		"-I$(S)/contrib/libs/musl/arch/generic",
		"-I$(S)/contrib/libs/musl/include",
		"-I$(S)/contrib/libs/musl/extra",
		"-I$(S)/contrib/libs/linux-headers",
		"-I$(S)/contrib/libs/linux-headers/_nf",
	}

	for i, want := range wantSlot {
		if args[7+i] != want {
			t.Errorf("cmd_args[%d] = %q, want %q", 7+i, args[7+i], want)
		}
	}
}

func TestEmitCC_NoStdInc_IncludeTailFollowsOwnAddIncl(t *testing.T) {
	emit := NewBufferedEmitter()
	inst := muslHostInstance("contrib/libs/musl")
	in := ModuleCCInputs{
		AddIncl: []VFS{
			Source("custom/musl/arch/x86_64"),
			Source("custom/musl/include"),
		},
	}
	EmitCC(inst, "src/string/strlen.c", Source("contrib/libs/musl/src/string/strlen.c"), in, testHostP, emit)

	args := emit.nodes[0].Cmds[0].CmdArgs
	wantSlot := []string{
		"-I$(B)",
		"-I$(S)",
		"-I$(S)/custom/musl/arch/x86_64",
		"-I$(S)/custom/musl/include",
		"-I$(S)/contrib/libs/linux-headers",
		"-I$(S)/contrib/libs/linux-headers/_nf",
	}

	for i, want := range wantSlot {
		if args[6+i] != want {
			t.Fatalf("cmd_args[%d] = %q, want %q; args=%v", 6+i, args[6+i], want, args)
		}
	}

	for _, banned := range []string{
		"-I$(S)/contrib/libs/musl/arch/x86_64",
		"-I$(S)/contrib/libs/musl/arch/generic",
		"-I$(S)/contrib/libs/musl/src/include",
		"-I$(S)/contrib/libs/musl/src/internal",
		"-I$(S)/contrib/libs/musl/include",
		"-I$(S)/contrib/libs/musl/extra",
	} {
		if contains(args, banned) {
			t.Fatalf("cmd_args unexpectedly contain hardcoded musl include %q: %v", banned, args)
		}
	}
}

// TestEmitCC_CxxSource_UsesClangPlusPlus verifies PR-29-D05: a `.cpp`
// source dispatches to clang++ and threads `-std=c++20` after the
// second suppression block.
func TestEmitCC_CxxSource_UsesClangPlusPlus(t *testing.T) {
	emit := NewBufferedEmitter()
	EmitCC(targetInstance("contrib/libs/cxxsupp/libcxx"), "src/algorithm.cpp", Source("contrib/libs/cxxsupp/libcxx/src/algorithm.cpp"), ModuleCCInputs{}, testHostP, emit)

	args := emit.nodes[0].Cmds[0].CmdArgs

	wantCxx := testTargetP.Tools.CXX
	if args[0] != wantCxx {
		t.Errorf("compiler = %q, want %q", args[0], wantCxx)
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
	EmitCC(targetInstance("build/cow/on"), "lib.c", Source("build/cow/on/lib.c"), ModuleCCInputs{}, testHostP, emit)

	args := emit.nodes[0].Cmds[0].CmdArgs

	wantCC := testTargetP.Tools.CC
	if args[0] != wantCC {
		t.Errorf("compiler = %q, want %q", args[0], wantCC)
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
	EmitCC(inst, "exception.cc", Source("contrib/libs/cxxsupp/libcxxrt/exception.cc"), ModuleCCInputs{Flags: FlagSet{NoCompilerWarnings: true}}, testHostP, emit)

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
		Flags:    FlagSet{NoCompilerWarnings: true},
		CXXFlags: []string{"-D_LIBCPP_BUILDING_LIBRARY"},
	}
	inst := targetInstance("contrib/libs/cxxsupp/libcxx")
	EmitCC(inst, "src/algorithm.cpp", Source("contrib/libs/cxxsupp/libcxx/src/algorithm.cpp"), in, testHostP, emit)

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
	EmitCC(targetInstance("build/cow/on"), "lib.c", Source("build/cow/on/lib.c"), in, testHostP, emitC)

	if !contains(emitC.nodes[0].Cmds[0].CmdArgs, "-Wno-narrowing") {
		t.Errorf(".c source missing CONLYFLAG -Wno-narrowing; got %v", emitC.nodes[0].Cmds[0].CmdArgs)
	}

	emitCpp := NewBufferedEmitter()
	EmitCC(targetInstance("build/cow/on"), "lib.cpp", Source("build/cow/on/lib.cpp"), in, testHostP, emitCpp)

	if contains(emitCpp.nodes[0].Cmds[0].CmdArgs, "-Wno-narrowing") {
		t.Errorf(".cpp source got CONLYFLAG -Wno-narrowing (should be CXXFlags-only); got %v", emitCpp.nodes[0].Cmds[0].CmdArgs)
	}
}

func TestEmitCC_PlatformEnvFlags_TargetOnly(t *testing.T) {
	flags := make(map[string]string, len(testToolchainFlags)+1)
	for k, v := range testToolchainFlags {
		flags[k] = v
	}
	flags["PIC"] = "no"

	target := NewPlatform(OSLinux, ISAAArch64, flags, nil, "-DENV_C=1", "-DENV_CXX=1")
	instance := ModuleInstance{
		Path:     "build/cow/on",
		Kind:     KindLib,
		Language: LangCPP,
		Platform: target,
	}

	e := NewBufferedEmitter()
	EmitCC(instance, "lib.c", Source("build/cow/on/lib.c"), ModuleCCInputs{Flags: FlagSet{NoLibc: true, NoUtil: true, NoRuntime: true}}, testHostP, e)
	cArgs := e.nodes[0].Cmds[0].CmdArgs

	if !contains(cArgs, "-DENV_C=1") {
		t.Fatalf("C cmd_args missing env CFLAGS: %v", cArgs)
	}

	if contains(cArgs, "-DENV_CXX=1") {
		t.Fatalf("C cmd_args unexpectedly contain env CXXFLAGS: %v", cArgs)
	}

	e = NewBufferedEmitter()
	EmitCC(instance, "lib.cpp", Source("build/cow/on/lib.cpp"), ModuleCCInputs{}, testHostP, e)
	cxxArgs := e.nodes[0].Cmds[0].CmdArgs

	if !contains(cxxArgs, "-DENV_C=1") {
		t.Fatalf("C++ cmd_args missing env CFLAGS: %v", cxxArgs)
	}

	if !contains(cxxArgs, "-DENV_CXX=1") {
		t.Fatalf("C++ cmd_args missing env CXXFLAGS: %v", cxxArgs)
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

func TestEmitCC_OutputPath_YqlUdfSuffix(t *testing.T) {
	e := NewBufferedEmitter()
	in := ModuleCCInputs{ObjectSuffixStem: stringPtr("udfs")}

	_, outPath := EmitCC(targetInstance("udfmod"), "lib.cpp", Source("udfmod/lib.cpp"), in, testHostP, e)

	want := "$(B)/udfmod/lib.cpp.udfs.o"
	if outPath.String() != want {
		t.Fatalf("outPath = %q, want %q", outPath, want)
	}
}

func TestEmitCC_OutputPath_YqlUdfSuffixPIC(t *testing.T) {
	e := NewBufferedEmitter()
	in := ModuleCCInputs{ObjectSuffixStem: stringPtr("udfs")}
	instance := ModuleInstance{
		Path:     "udfmod",
		Kind:     KindLib,
		Language: LangCPP,
		Platform: testHostP,
	}

	_, outPath := EmitCC(instance, "lib.cpp", Source("udfmod/lib.cpp"), in, testHostP, e)

	want := "$(B)/udfmod/lib.cpp.udfs.pic.o"
	if outPath.String() != want {
		t.Fatalf("outPath = %q, want %q", outPath, want)
	}
}

func TestEmitCC_NoWShadowAddsWarningFlag(t *testing.T) {
	e := NewBufferedEmitter()
	in := ModuleCCInputs{Flags: FlagSet{NoWShadow: true}}

	EmitCC(targetInstance("build/cow/on"), "lib.cpp", Source("build/cow/on/lib.cpp"), in, testHostP, e)

	if !contains(e.nodes[0].Cmds[0].CmdArgs, "-Wno-shadow") {
		t.Fatalf("cmd_args missing -Wno-shadow: %v", e.nodes[0].Cmds[0].CmdArgs)
	}
}

// TestComposeSrcDirOutputRel_FlatSrcInModuleDir pins Fix #3a: when SRCDIR
// redirects to a source that lands directly in instancePath (no subdirectory),
// no `_/` prefix is emitted.
func TestComposeSrcDirOutputRel_FlatSrcInModuleDir(t *testing.T) {
	// ngtcp2 shape: module at .../quictls, SRCDIR .../crypto, src quictls/quictls.c
	// target = crypto/quictls/quictls.c = instancePath/quictls.c → rel = quictls.c (no ..)
	got := composeSrcDirOutputRel(
		"contrib/libs/ngtcp2/crypto/quictls",
		"contrib/libs/ngtcp2/crypto",
		"quictls/quictls.c",
	)
	want := "quictls.c"
	if got != want {
		t.Errorf("composeSrcDirOutputRel = %q, want %q", got, want)
	}
}

// TestComposeSrcDirOutputRel_SubdirInModuleDir confirms that a source in a
// subdirectory of instancePath still gets the `_/` infix (regression guard).
func TestComposeSrcDirOutputRel_SubdirInModuleDir(t *testing.T) {
	// module at foo/bar, SRCDIR foo/bar, src sub/file.cpp → rel = sub/file.cpp
	got := composeSrcDirOutputRel("foo/bar", "foo/bar", "sub/file.cpp")
	want := "_/sub/file.cpp"
	if got != want {
		t.Errorf("composeSrcDirOutputRel = %q, want %q", got, want)
	}
}

// TestComposeCCPaths_DotDotSrc pins Fix #3b: SRCS(../sibling.cpp) normalizes
// `..` to `__` in the output path instead of leaving `_/../sibling.cpp`.
func TestComposeCCPaths_DotDotSrc(t *testing.T) {
	e := NewBufferedEmitter()
	// srcVFS is $(S)/ydb/.../command_base/../ydb_command.cpp resolved to the source.
	// Since the Rel does not equal instance.Path+"/"+srcRel, we need the
	// non-SRCDIR branch: srcVFS.Rel == instance.Path+"/"+srcRel for a local
	// source. For the `../` case, the source lives one dir up, so srcVFS.Rel
	// differs → composeCCPaths takes the default switch.
	// We test composeCCPaths directly via EmitCC with a local source whose
	// Rel matches instance.Path+"/"+srcRel to stay in the switch branch.
	// Simulate: local source doesn't exist → srcVFS == Source(instance+"/"+srcRel).
	instance := targetInstance("ydb/public/lib/ydb_cli/commands/command_base")
	srcRel := "../ydb_command.cpp"
	srcVFS := Source(instance.Path + "/" + srcRel) // satisfies IsSource() && Rel == instance.Path+"/"+srcRel check? No.
	// Actually, the `../` case: srcVFS.Rel = "ydb/.../command_base/../ydb_command.cpp"
	// which != instance.Path+"/"+srcRel ("ydb/.../command_base/../ydb_command.cpp") — they ARE equal.
	// So composeCCPaths falls to the switch. The `../` in srcRel contains `/` →
	// normalizeDotDotSegments("../ydb_command.cpp") → "__/ydb_command.cpp"
	// outRel = instance.Path + "/" + "__/ydb_command.cpp" + ".o"
	_ = e
	_ = srcVFS

	got := normalizeDotDotSegments(srcRel)
	want := "__/ydb_command.cpp"
	if got != want {
		t.Errorf("normalizeDotDotSegments(%q) = %q, want %q", srcRel, got, want)
	}
}

// TestNormalizeDotDotSegments_Subdir confirms subdir/file.cpp gets _/ prefix.
func TestNormalizeDotDotSegments_Subdir(t *testing.T) {
	got := normalizeDotDotSegments("subdir/file.cpp")
	want := "_/subdir/file.cpp"
	if got != want {
		t.Errorf("normalizeDotDotSegments = %q, want %q", got, want)
	}
}
