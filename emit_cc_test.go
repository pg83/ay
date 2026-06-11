package main

import (
	"testing"
)

const referenceCCOutput = "$(B)/build/cow/on/lib.c.o"

func TestEmitCC_OutputPath_NestedSrc(t *testing.T) {
	e := NewBufferedEmitter()
	_, outPath, _ := EmitCC(targetInstance("contrib/libs/cxxsupp/libcxx"), "src/algorithm.cpp", Intern("$(S)/contrib/libs/cxxsupp/libcxx/src/algorithm.cpp"), withCCBlocks(targetInstance("contrib/libs/cxxsupp/libcxx").Platform, ModuleCCInputs{}), testHostP, e)
	want := "$(B)/contrib/libs/cxxsupp/libcxx/_/src/algorithm.cpp.o"

	if outPath.String() != want {
		t.Errorf("outPath = %q, want %q", outPath, want)
	}
}

func TestEmitCC_OutputPath_FlatSrc(t *testing.T) {
	e := NewBufferedEmitter()
	_, outPath, _ := EmitCC(targetInstance("build/cow/on"), "lib.c", Intern("$(S)/build/cow/on/lib.c"), withCCBlocks(targetInstance("build/cow/on").Platform, ModuleCCInputs{}), testHostP, e)
	want := "$(B)/build/cow/on/lib.c.o"

	if outPath.String() != want {
		t.Errorf("outPath = %q, want %q", outPath, want)
	}
}

func TestEmitCC_GeneratedSource_BuildRootInput(t *testing.T) {
	emit := NewBufferedEmitter()
	srcVFS := Intern("$(B)/util/_/datetime/parser.rl6.cpp")
	// IncludeInputs is the full input window — the source leads it.
	_, outPath, _ := EmitCC(targetInstance("util"), "_/datetime/parser.rl6.cpp", srcVFS, withCCBlocks(targetInstance("util").Platform, ModuleCCInputs{IncludeInputs: []VFS{srcVFS}}), testHostP, emit)

	wantOut := "$(B)/util/_/_/datetime/parser.rl6.cpp.o"

	if outPath.String() != wantOut {
		t.Errorf("outPath = %q, want %q", outPath, wantOut)
	}

	if len(emit.nodes) != 1 {
		t.Fatalf("emitter buffered %d nodes, want 1", len(emit.nodes))
	}

	got := emit.nodes[0]

	wantInput := "$(B)/util/_/datetime/parser.rl6.cpp"

	if len(got.flatInputs()) != 1 || got.flatInputs()[0].String() != wantInput {
		t.Errorf("inputs = %v, want [%q]", got.flatInputs(), wantInput)
	}

	args := got.Cmds[0].CmdArgs.flat()

	if args[len(args)-1].String() != wantInput {
		t.Errorf("cmd_args[last] = %q, want %q", args[len(args)-1].String(), wantInput)
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
	EmitCC(targetInstance("contrib/libs/cxxsupp/builtins"), "aarch64/fp_mode.c", Intern("$(S)/contrib/libs/cxxsupp/builtins/aarch64/fp_mode.c"), withCCBlocks(targetInstance("contrib/libs/cxxsupp/builtins").Platform, in), testHostP, emit)

	args := emit.nodes[0].Cmds[0].CmdArgs.flat()

	wantSlot := []string{
		"-I$(B)",
		"-I$(S)",
		"-I$(S)/contrib/libs/foolib/arch/aarch64",
		"-I$(S)/contrib/libs/foolib/arch/generic",
		"-I$(S)/contrib/libs/foolib/include",
		"-I$(S)/contrib/libs/foolib/extra",
	}

	// Prefix (aarch64, opensource test contour — bare -B/usr/bin, no sysroot):
	// compiler, --target, -march, -B, -c, -o, output → block starts at index 7.
	for i, want := range wantSlot {
		if args[7+i].String() != want {
			t.Errorf("cmd_args[%d] = %q, want %q", 7+i, args[7+i].String(), want)
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
	EmitCC(inst, "src/string/strlen.c", Intern("$(S)/contrib/libs/foolib/src/string/strlen.c"), withCCBlocks(inst.Platform, in), testHostP, emit)

	args := emit.nodes[0].Cmds[0].CmdArgs.flat()
	wantSlot := []string{
		"-I$(B)",
		"-I$(S)",
		"-I$(S)/custom/foolib/arch/x86_64",
		"-I$(S)/custom/foolib/include",
	}

	// Prefix (x86_64, no -march): compiler, --target, --sysroot, -B<sdk>/usr/bin, -c, -o,
	// output → the include block starts at index 7.
	for i, want := range wantSlot {
		if args[6+i].String() != want {
			t.Fatalf("cmd_args[%d] = %q, want %q; args=%v", 6+i, args[6+i].String(), want, args)
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
	EmitCC(targetInstance("contrib/libs/cxxsupp/libcxx"), "src/algorithm.cpp", Intern("$(S)/contrib/libs/cxxsupp/libcxx/src/algorithm.cpp"), withCCBlocks(targetInstance("contrib/libs/cxxsupp/libcxx").Platform, ModuleCCInputs{TC: testToolchain()}), testHostP, emit)

	args := emit.nodes[0].Cmds[0].CmdArgs.flat()

	wantCxx := testToolchain().CXX.String()
	if args[0].String() != wantCxx {
		t.Errorf("compiler = %q, want %q", args[0].String(), wantCxx)
	}

	found := false

	for _, a := range args {
		if a.String() == cxxStandardFlag.String() {
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
	EmitCC(targetInstance("build/cow/on"), "lib.c", Intern("$(S)/build/cow/on/lib.c"), withCCBlocks(targetInstance("build/cow/on").Platform, ModuleCCInputs{TC: testToolchain()}), testHostP, emit)

	args := emit.nodes[0].Cmds[0].CmdArgs.flat()

	wantCC := testToolchain().CC.String()
	if args[0].String() != wantCC {
		t.Errorf("compiler = %q, want %q", args[0].String(), wantCC)
	}

	for _, a := range args {
		if a.String() == cxxStandardFlag.String() {
			t.Errorf("cmd_args contains %q for a .c source", cxxStandardFlag)

			break
		}
	}
}

func TestEmitCC_NoCompilerWarnings_SelectsWarningSuppressionFlags(t *testing.T) {
	emit := NewBufferedEmitter()
	inst := targetInstance("contrib/libs/cxxsupp/libcxxrt")
	EmitCC(inst, "exception.cc", Intern("$(S)/contrib/libs/cxxsupp/libcxxrt/exception.cc"), withCCBlocks(inst.Platform, ModuleCCInputs{Flags: FlagSet{NoCompilerWarnings: true}}), testHostP, emit)

	args := emit.nodes[0].Cmds[0].CmdArgs.flat()

	for _, a := range args {
		if a.String() == "-Werror" {
			t.Errorf("cmd_args contains -Werror despite NoCompilerWarnings=true")
		}
	}

	wnoCount := 0

	for _, a := range args {
		if a.String() == "-Wno-everything" {
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
		CXXFlags: internArgs([]string{"-D_LIBCPP_BUILDING_LIBRARY"}),
	}
	inst := targetInstance("contrib/libs/cxxsupp/libcxx")
	EmitCC(inst, "src/algorithm.cpp", Intern("$(S)/contrib/libs/cxxsupp/libcxx/src/algorithm.cpp"), withCCBlocks(inst.Platform, in), testHostP, emit)

	args := emit.nodes[0].Cmds[0].CmdArgs.flat()

	idxOwn := -1
	idxLastSuppression := -1
	idxBuiltinDate := -1

	for i, a := range args {
		switch a.String() {
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
	in := ModuleCCInputs{COnlyFlags: internArgs([]string{"-Wno-narrowing"})}

	emitC := NewBufferedEmitter()
	EmitCC(targetInstance("build/cow/on"), "lib.c", Intern("$(S)/build/cow/on/lib.c"), withCCBlocks(targetInstance("build/cow/on").Platform, in), testHostP, emitC)

	if !contains(emitC.nodes[0].Cmds[0].CmdArgs.flat(), "-Wno-narrowing") {
		t.Errorf(".c source missing CONLYFLAG -Wno-narrowing; got %v", emitC.nodes[0].Cmds[0].CmdArgs.flat())
	}

	emitCpp := NewBufferedEmitter()
	EmitCC(targetInstance("build/cow/on"), "lib.cpp", Intern("$(S)/build/cow/on/lib.cpp"), withCCBlocks(targetInstance("build/cow/on").Platform, in), testHostP, emitCpp)

	if contains(emitCpp.nodes[0].Cmds[0].CmdArgs.flat(), "-Wno-narrowing") {
		t.Errorf(".cpp source got CONLYFLAG -Wno-narrowing (should be CXXFlags-only); got %v", emitCpp.nodes[0].Cmds[0].CmdArgs.flat())
	}
}

func TestEmitCC_PlatformEnvFlags_TargetOnly(t *testing.T) {
	flags := make(map[string]string, len(testToolchainFlags)+1)
	for k, v := range testToolchainFlags {
		flags[k] = v
	}
	flags["PIC"] = "no"

	target := NewPlatform(newMemFS(nil), OSLinux, ISAAArch64, flags, nil, "-DENV_C=1", "-DENV_CXX=1")
	instance := ModuleInstance{
		Path:     Source("build/cow/on"),
		Kind:     KindLib,
		Language: LangCPP,
		Platform: target,
	}

	e := NewBufferedEmitter()
	EmitCC(instance, "lib.c", Intern("$(S)/build/cow/on/lib.c"), withCCBlocks(instance.Platform, ModuleCCInputs{Flags: FlagSet{NoLibc: true, NoUtil: true, NoRuntime: true}}), testHostP, e)
	cArgs := e.nodes[0].Cmds[0].CmdArgs.flat()

	if !contains(cArgs, "-DENV_C=1") {
		t.Fatalf("C cmd_args missing env CFLAGS: %v", cArgs)
	}

	if contains(cArgs, "-DENV_CXX=1") {
		t.Fatalf("C cmd_args unexpectedly contain env CXXFLAGS: %v", cArgs)
	}

	e = NewBufferedEmitter()
	EmitCC(instance, "lib.cpp", Intern("$(S)/build/cow/on/lib.cpp"), withCCBlocks(instance.Platform, ModuleCCInputs{}), testHostP, e)
	cxxArgs := e.nodes[0].Cmds[0].CmdArgs.flat()

	if !contains(cxxArgs, "-DENV_C=1") {
		t.Fatalf("C++ cmd_args missing env CFLAGS: %v", cxxArgs)
	}

	if !contains(cxxArgs, "-DENV_CXX=1") {
		t.Fatalf("C++ cmd_args missing env CXXFLAGS: %v", cxxArgs)
	}
}

// nonOpensourcePlatform builds an internal-contour target platform (OPENSOURCE unset),
// the contour under which the wrapcc.py compile wrapper is active. The shared
// testTargetP/testHostP model the opensource (sg2–5) contour and do not wrap.
func nonOpensourcePlatform() *Platform {
	flags := make(map[string]string, len(testToolchainFlags)+1)
	for k, v := range testToolchainFlags {
		flags[k] = v
	}
	delete(flags, "OPENSOURCE")
	flags["PIC"] = "no"

	return NewPlatform(newMemFS(nil), OSLinux, ISAAArch64, flags, nil, "", "")
}

func TestEmitCC_WrapccPrefix_NonOpensource(t *testing.T) {
	emit := NewBufferedEmitter()
	inst := ModuleInstance{Path: Source("mod"), Kind: KindLib, Language: LangCPP, Platform: nonOpensourcePlatform()}
	srcVFS := Intern("$(S)/mod/lib.cpp")

	EmitCC(inst, "lib.cpp", srcVFS, withCCBlocks(inst.Platform, ModuleCCInputs{TC: testToolchain(), IncludeInputs: []VFS{srcVFS}}), testHostP, emit)

	node := emit.nodes[0]
	args := strStrs(node.Cmds[0].CmdArgs.flat())

	wantPrefix := []string{
		"$(B)/resources/YMAKE_PYTHON3/bin/python3",
		"$(S)/build/scripts/wrapcc.py",
		"--source-file",
		"$(S)/mod/lib.cpp",
		"--source-root",
		"$(S)",
		"--build-root",
		"$(B)",
		"--wrapcc-end",
		testToolchain().CXX.String(),
	}

	if len(args) < len(wantPrefix) {
		t.Fatalf("CC cmd_args shorter than wrapcc prefix: %v", args)
	}

	for i, w := range wantPrefix {
		if args[i] != w {
			t.Fatalf("cmd_args[%d] = %q, want %q\nfull: %v", i, args[i], w, args[:len(wantPrefix)])
		}
	}

	// wrapcc.py is an input of the wrapped node.
	if !slicesContains(vfsStrings(node.flatInputs()), "$(S)/build/scripts/wrapcc.py") {
		t.Errorf("wrapped CC node inputs missing wrapcc.py: %v", vfsStrings(node.flatInputs()))
	}

	// The source stays first; wrapcc.py is appended after.
	if node.flatInputs()[0].String() != "$(S)/mod/lib.cpp" {
		t.Errorf("inputs[0] = %q, want the source $(S)/mod/lib.cpp", node.flatInputs()[0].String())
	}

	// YMAKE_PYTHON3 joins the CC deps (the wrapper runs under it).
	if !slicesContains(node.usesResources, resourcePatternYMakePython3) {
		t.Errorf("wrapped CC usesResources missing YMAKE_PYTHON3: %v", node.usesResources)
	}
}

func TestEmitCC_NoWrapcc_Opensource(t *testing.T) {
	emit := NewBufferedEmitter()
	// targetInstance uses testTargetP, which is OPENSOURCE=yes → no wrapper.
	EmitCC(targetInstance("mod"), "lib.cpp", Intern("$(S)/mod/lib.cpp"), withCCBlocks(targetInstance("mod").Platform, ModuleCCInputs{TC: testToolchain()}), testHostP, emit)

	node := emit.nodes[0]
	args := strStrs(node.Cmds[0].CmdArgs.flat())

	if args[0] != testToolchain().CXX.String() {
		t.Errorf("opensource CC cmd_args[0] = %q, want the compiler (no wrapcc prefix)", args[0])
	}

	if slicesContains(vfsStrings(node.flatInputs()), "$(S)/build/scripts/wrapcc.py") {
		t.Errorf("opensource CC node must not list wrapcc.py as input: %v", vfsStrings(node.flatInputs()))
	}

	if slicesContains(node.usesResources, resourcePatternYMakePython3) {
		t.Errorf("opensource CC node must not depend on YMAKE_PYTHON3: %v", node.usesResources)
	}
}

func contains(xs []STR, target string) bool {
	for _, x := range xs {
		if x.String() == target {
			return true
		}
	}

	return false
}

func TestEmitCC_OutputPath_YqlUdfSuffix(t *testing.T) {
	e := NewBufferedEmitter()
	in := ModuleCCInputs{ObjectSuffixStem: stringPtr("udfs")}

	_, outPath, _ := EmitCC(targetInstance("udfmod"), "lib.cpp", Intern("$(S)/udfmod/lib.cpp"), withCCBlocks(targetInstance("udfmod").Platform, in), testHostP, e)

	want := "$(B)/udfmod/lib.cpp.udfs.o"
	if outPath.String() != want {
		t.Fatalf("outPath = %q, want %q", outPath, want)
	}
}

func TestEmitCC_OutputPath_YqlUdfSuffixPIC(t *testing.T) {
	e := NewBufferedEmitter()
	in := ModuleCCInputs{ObjectSuffixStem: stringPtr("udfs")}
	instance := ModuleInstance{
		Path:     Source("udfmod"),
		Kind:     KindLib,
		Language: LangCPP,
		Platform: testHostP,
	}

	_, outPath, _ := EmitCC(instance, "lib.cpp", Intern("$(S)/udfmod/lib.cpp"), withCCBlocks(instance.Platform, in), testHostP, e)

	want := "$(B)/udfmod/lib.cpp.udfs.pic.o"
	if outPath.String() != want {
		t.Fatalf("outPath = %q, want %q", outPath, want)
	}
}

func TestEmitCC_NoWShadowAddsWarningFlag(t *testing.T) {
	e := NewBufferedEmitter()
	in := ModuleCCInputs{Flags: FlagSet{NoWShadow: true}}

	EmitCC(targetInstance("build/cow/on"), "lib.cpp", Intern("$(S)/build/cow/on/lib.cpp"), withCCBlocks(targetInstance("build/cow/on").Platform, in), testHostP, e)

	if !contains(e.nodes[0].Cmds[0].CmdArgs.flat(), "-Wno-shadow") {
		t.Fatalf("cmd_args missing -Wno-shadow: %v", e.nodes[0].Cmds[0].CmdArgs.flat())
	}
}

func TestComposeSrcDirOutputRel_FlatSrcInModuleDir(t *testing.T) {

	got := composeSrcDirOutputRel(
		"contrib/libs/ngtcp2/crypto/quictls",
		"contrib/libs/ngtcp2/crypto/quictls/quictls.c",
	)
	want := "quictls.c"
	if got != want {
		t.Errorf("composeSrcDirOutputRel = %q, want %q", got, want)
	}
}

func TestComposeSrcDirOutputRel_SubdirInModuleDir(t *testing.T) {

	got := composeSrcDirOutputRel("foo/bar", "foo/bar/sub/file.cpp")
	want := "_/sub/file.cpp"
	if got != want {
		t.Errorf("composeSrcDirOutputRel = %q, want %q", got, want)
	}
}

func TestComposeCCPaths_DotDotSrc(t *testing.T) {
	e := NewBufferedEmitter()

	instance := targetInstance("ydb/public/lib/ydb_cli/commands/command_base")
	srcRel := "../ydb_command.cpp"
	srcVFS := Source(instance.Path.Rel() + "/" + srcRel)

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

// withCCBlocks builds the module-stable arg blocks for an ad-hoc test input —
// production code builds them where the module's ModuleCCInputs is assembled.
func withCCBlocks(p *Platform, in ModuleCCInputs) ModuleCCInputs {
	in.CCBlocks = composeCCModuleArgBlocks(p, &in)

	return in
}
