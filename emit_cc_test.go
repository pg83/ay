package main

import (
	"testing"
)

const referenceCCOutput = "$(B)/build/cow/on/lib.c.o"

func TestEmitCC_OutputPath_NestedSrc(t *testing.T) {
	e := NewBufferedEmitter()
	_, outPath, _ := EmitCC(targetInstance("contrib/libs/cxxsupp/libcxx"), "src/algorithm.cpp", Intern("$(S)/contrib/libs/cxxsupp/libcxx/src/algorithm.cpp"), ModuleCCInputs{}, testHostP, e)
	want := "$(B)/contrib/libs/cxxsupp/libcxx/_/src/algorithm.cpp.o"

	if outPath.String() != want {
		t.Errorf("outPath = %q, want %q", outPath, want)
	}
}

func TestEmitCC_OutputPath_FlatSrc(t *testing.T) {
	e := NewBufferedEmitter()
	_, outPath, _ := EmitCC(targetInstance("build/cow/on"), "lib.c", Intern("$(S)/build/cow/on/lib.c"), ModuleCCInputs{}, testHostP, e)
	want := "$(B)/build/cow/on/lib.c.o"

	if outPath.String() != want {
		t.Errorf("outPath = %q, want %q", outPath, want)
	}
}

func TestEmitCC_GeneratedSource_BuildRootInput(t *testing.T) {
	emit := NewBufferedEmitter()
	srcVFS := Intern("$(B)/util/_/datetime/parser.rl6.cpp")
	_, outPath, _ := EmitCC(targetInstance("util"), "_/datetime/parser.rl6.cpp", srcVFS, ModuleCCInputs{}, testHostP, emit)

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

	args := got.Cmds[0].CmdArgs

	if args[len(args)-1] != wantInput {
		t.Errorf("cmd_args[last] = %q, want %q", args[len(args)-1], wantInput)
	}
}

func TestEmitCC_AddIncl_SlotsBetweenPrefixAndSuffix(t *testing.T) {
	emit := NewBufferedEmitter()
	in := ModuleCCInputs{
		InclArgs: newInclArgMemo(),
		AddIncl: []VFS{
			Intern("$(S)/contrib/libs/foolib/arch/aarch64"),
			Intern("$(S)/contrib/libs/foolib/arch/generic"),
			Intern("$(S)/contrib/libs/foolib/include"),
			Intern("$(S)/contrib/libs/foolib/extra"),
		},
	}
	EmitCC(targetInstance("contrib/libs/cxxsupp/builtins"), "aarch64/fp_mode.c", Intern("$(S)/contrib/libs/cxxsupp/builtins/aarch64/fp_mode.c"), in, testHostP, emit)

	args := emit.nodes[0].Cmds[0].CmdArgs

	wantSlot := []string{
		"-I$(B)",
		"-I$(S)",
		"-I$(S)/contrib/libs/foolib/arch/aarch64",
		"-I$(S)/contrib/libs/foolib/arch/generic",
		"-I$(S)/contrib/libs/foolib/include",
		"-I$(S)/contrib/libs/foolib/extra",
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
	inst := hostInstance("contrib/libs/foolib")
	in := ModuleCCInputs{
		InclArgs: newInclArgMemo(),
		AddIncl: []VFS{
			Intern("$(S)/custom/foolib/arch/x86_64"),
			Intern("$(S)/custom/foolib/include"),
		},
	}
	EmitCC(inst, "src/string/strlen.c", Intern("$(S)/contrib/libs/foolib/src/string/strlen.c"), in, testHostP, emit)

	args := emit.nodes[0].Cmds[0].CmdArgs
	wantSlot := []string{
		"-I$(B)",
		"-I$(S)",
		"-I$(S)/custom/foolib/arch/x86_64",
		"-I$(S)/custom/foolib/include",
		"-I$(S)/contrib/libs/linux-headers",
		"-I$(S)/contrib/libs/linux-headers/_nf",
	}

	for i, want := range wantSlot {
		if args[6+i] != want {
			t.Fatalf("cmd_args[%d] = %q, want %q; args=%v", 6+i, args[6+i], want, args)
		}
	}

	for _, banned := range []string{
		"-I$(S)/contrib/libs/foolib/arch/x86_64",
		"-I$(S)/contrib/libs/foolib/arch/generic",
		"-I$(S)/contrib/libs/foolib/src/include",
		"-I$(S)/contrib/libs/foolib/src/internal",
		"-I$(S)/contrib/libs/foolib/include",
		"-I$(S)/contrib/libs/foolib/extra",
	} {
		if contains(args, banned) {
			t.Fatalf("cmd_args unexpectedly contain hardcoded foolib include %q: %v", banned, args)
		}
	}
}

func TestEmitCC_CxxSource_UsesClangPlusPlus(t *testing.T) {
	emit := NewBufferedEmitter()
	EmitCC(targetInstance("contrib/libs/cxxsupp/libcxx"), "src/algorithm.cpp", Intern("$(S)/contrib/libs/cxxsupp/libcxx/src/algorithm.cpp"), ModuleCCInputs{}, testHostP, emit)

	args := emit.nodes[0].Cmds[0].CmdArgs

	wantCxx := testTargetP.Tools.CXX
	if args[0] != wantCxx {
		t.Errorf("compiler = %q, want %q", args[0], wantCxx)
	}

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

func TestEmitCC_CSource_UsesClang(t *testing.T) {
	emit := NewBufferedEmitter()
	EmitCC(targetInstance("build/cow/on"), "lib.c", Intern("$(S)/build/cow/on/lib.c"), ModuleCCInputs{}, testHostP, emit)

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

func TestEmitCC_NoCompilerWarnings_SelectsWarningSuppressionFlags(t *testing.T) {
	emit := NewBufferedEmitter()
	inst := targetInstance("contrib/libs/cxxsupp/libcxxrt")
	EmitCC(inst, "exception.cc", Intern("$(S)/contrib/libs/cxxsupp/libcxxrt/exception.cc"), ModuleCCInputs{Flags: FlagSet{NoCompilerWarnings: true}}, testHostP, emit)

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

func TestEmitCC_OwnCXXFlags_SlotsAfterSuppressionBlock(t *testing.T) {
	emit := NewBufferedEmitter()
	in := ModuleCCInputs{
		Flags:    FlagSet{NoCompilerWarnings: true},
		CXXFlags: []string{"-D_LIBCPP_BUILDING_LIBRARY"},
	}
	inst := targetInstance("contrib/libs/cxxsupp/libcxx")
	EmitCC(inst, "src/algorithm.cpp", Intern("$(S)/contrib/libs/cxxsupp/libcxx/src/algorithm.cpp"), in, testHostP, emit)

	args := emit.nodes[0].Cmds[0].CmdArgs

	idxOwn := -1
	idxLastSuppression := -1
	idxBuiltinDate := -1

	for i, a := range args {
		switch a {
		case "-D_LIBCPP_BUILDING_LIBRARY":
			idxOwn = i
		case "-Wno-strict-primary-template-shadow":

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

	if !contains(args, "-D_LIBCPP_BUILDING_LIBRARY") {
		t.Errorf("expected own CXXFLAGS in args")
	}
}

func TestEmitCC_COnlyFlags_AppliesOnlyToCSources(t *testing.T) {
	in := ModuleCCInputs{COnlyFlags: []string{"-Wno-narrowing"}}

	emitC := NewBufferedEmitter()
	EmitCC(targetInstance("build/cow/on"), "lib.c", Intern("$(S)/build/cow/on/lib.c"), in, testHostP, emitC)

	if !contains(emitC.nodes[0].Cmds[0].CmdArgs, "-Wno-narrowing") {
		t.Errorf(".c source missing CONLYFLAG -Wno-narrowing; got %v", emitC.nodes[0].Cmds[0].CmdArgs)
	}

	emitCpp := NewBufferedEmitter()
	EmitCC(targetInstance("build/cow/on"), "lib.cpp", Intern("$(S)/build/cow/on/lib.cpp"), in, testHostP, emitCpp)

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

	target := NewPlatform(OSLinux, ISAAArch64, flags, nil, "-DENV_C=1", "-DENV_CXX=1", nil)
	instance := ModuleInstance{
		Path:     "build/cow/on",
		Kind:     KindLib,
		Language: LangCPP,
		Platform: target,
	}

	e := NewBufferedEmitter()
	EmitCC(instance, "lib.c", Intern("$(S)/build/cow/on/lib.c"), ModuleCCInputs{Flags: FlagSet{NoLibc: true, NoUtil: true, NoRuntime: true}}, testHostP, e)
	cArgs := e.nodes[0].Cmds[0].CmdArgs

	if !contains(cArgs, "-DENV_C=1") {
		t.Fatalf("C cmd_args missing env CFLAGS: %v", cArgs)
	}

	if contains(cArgs, "-DENV_CXX=1") {
		t.Fatalf("C cmd_args unexpectedly contain env CXXFLAGS: %v", cArgs)
	}

	e = NewBufferedEmitter()
	EmitCC(instance, "lib.cpp", Intern("$(S)/build/cow/on/lib.cpp"), ModuleCCInputs{}, testHostP, e)
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

func TestEmitCC_OutputPath_YqlUdfSuffix(t *testing.T) {
	e := NewBufferedEmitter()
	in := ModuleCCInputs{ObjectSuffixStem: stringPtr("udfs")}

	_, outPath, _ := EmitCC(targetInstance("udfmod"), "lib.cpp", Intern("$(S)/udfmod/lib.cpp"), in, testHostP, e)

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

	_, outPath, _ := EmitCC(instance, "lib.cpp", Intern("$(S)/udfmod/lib.cpp"), in, testHostP, e)

	want := "$(B)/udfmod/lib.cpp.udfs.pic.o"
	if outPath.String() != want {
		t.Fatalf("outPath = %q, want %q", outPath, want)
	}
}

func TestEmitCC_NoWShadowAddsWarningFlag(t *testing.T) {
	e := NewBufferedEmitter()
	in := ModuleCCInputs{Flags: FlagSet{NoWShadow: true}}

	EmitCC(targetInstance("build/cow/on"), "lib.cpp", Intern("$(S)/build/cow/on/lib.cpp"), in, testHostP, e)

	if !contains(e.nodes[0].Cmds[0].CmdArgs, "-Wno-shadow") {
		t.Fatalf("cmd_args missing -Wno-shadow: %v", e.nodes[0].Cmds[0].CmdArgs)
	}
}

func TestComposeSrcDirOutputRel_FlatSrcInModuleDir(t *testing.T) {

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

func TestComposeSrcDirOutputRel_SubdirInModuleDir(t *testing.T) {

	got := composeSrcDirOutputRel("foo/bar", "foo/bar", "sub/file.cpp")
	want := "_/sub/file.cpp"
	if got != want {
		t.Errorf("composeSrcDirOutputRel = %q, want %q", got, want)
	}
}

func TestComposeCCPaths_DotDotSrc(t *testing.T) {
	e := NewBufferedEmitter()

	instance := targetInstance("ydb/public/lib/ydb_cli/commands/command_base")
	srcRel := "../ydb_command.cpp"
	srcVFS := Source(instance.Path + "/" + srcRel)

	_ = e
	_ = srcVFS

	got := normalizeDotDotSegments(srcRel)
	want := "__/ydb_command.cpp"
	if got != want {
		t.Errorf("normalizeDotDotSegments(%q) = %q, want %q", srcRel, got, want)
	}
}

func TestNormalizeDotDotSegments_Subdir(t *testing.T) {
	got := normalizeDotDotSegments("subdir/file.cpp")
	want := "_/subdir/file.cpp"
	if got != want {
		t.Errorf("normalizeDotDotSegments = %q, want %q", got, want)
	}
}
