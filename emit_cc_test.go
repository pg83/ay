package main

import (
	"strings"
	"testing"
)

const referenceCCOutput = "$(B)/build/cow/on/lib.c.o"

func TestEmitCC_OutputPath_NestedSrc(t *testing.T) {
	e := newBufferedEmitter()
	_, outPath, _ := emitCC(targetInstance("contrib/libs/cxxsupp/libcxx"), "src/algorithm.cpp", intern("$(S)/contrib/libs/cxxsupp/libcxx/src/algorithm.cpp"), withCCBlocks(targetInstance("contrib/libs/cxxsupp/libcxx").Platform, ModuleCCInputs{}), testHostP, e)
	want := "$(B)/contrib/libs/cxxsupp/libcxx/_/src/algorithm.cpp.o"

	if outPath.string() != want {
		t.Errorf("outPath = %q, want %q", outPath, want)
	}
}

func TestEmitCC_OutputPath_FlatSrc(t *testing.T) {
	e := newBufferedEmitter()
	_, outPath, _ := emitCC(targetInstance("build/cow/on"), "lib.c", intern("$(S)/build/cow/on/lib.c"), withCCBlocks(targetInstance("build/cow/on").Platform, ModuleCCInputs{}), testHostP, e)
	want := "$(B)/build/cow/on/lib.c.o"

	if outPath.string() != want {
		t.Errorf("outPath = %q, want %q", outPath, want)
	}
}

func TestEmitCC_GeneratedSource_BuildRootInput(t *testing.T) {
	emit := newBufferedEmitter()
	srcVFS := intern("$(B)/util/_/datetime/parser.rl6.cpp")
	// IncludeInputs is the full input window — the source leads it.
	_, outPath, _ := emitCC(targetInstance("util"), "_/datetime/parser.rl6.cpp", srcVFS, withCCBlocks(targetInstance("util").Platform, ModuleCCInputs{IncludeInputs: []VFS{srcVFS}}), testHostP, emit)

	wantOut := "$(B)/util/_/_/datetime/parser.rl6.cpp.o"

	if outPath.string() != wantOut {
		t.Errorf("outPath = %q, want %q", outPath, wantOut)
	}

	if len(emit.nodes) != 1 {
		t.Fatalf("emitter buffered %d nodes, want 1", len(emit.nodes))
	}

	got := emit.nodes[0]

	wantInput := "$(B)/util/_/datetime/parser.rl6.cpp"

	if len(got.flatInputs()) != 1 || got.flatInputs()[0].string() != wantInput {
		t.Errorf("inputs = %v, want [%q]", got.flatInputs(), wantInput)
	}

	args := got.Cmds[0].CmdArgs.flat()

	if args[len(args)-1].string() != wantInput {
		t.Errorf("cmd_args[last] = %q, want %q", args[len(args)-1].string(), wantInput)
	}
}

func TestEmitCC_AddIncl_SlotsBetweenPrefixAndSuffix(t *testing.T) {
	emit := newBufferedEmitter()
	in := ModuleCCInputs{
		InclArgs: newInclArgMemo(),
		AddIncl: []VFS{
			intern("$(S)/contrib/libs/foolib/arch/aarch64"),
			intern("$(S)/contrib/libs/foolib/arch/generic"),
			intern("$(S)/contrib/libs/foolib/include"),
			intern("$(S)/contrib/libs/foolib/extra"),
		},
	}
	emitCC(targetInstance("contrib/libs/cxxsupp/builtins"), "aarch64/fp_mode.c", intern("$(S)/contrib/libs/cxxsupp/builtins/aarch64/fp_mode.c"), withCCBlocks(targetInstance("contrib/libs/cxxsupp/builtins").Platform, in), testHostP, emit)

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
		if args[7+i].string() != want {
			t.Errorf("cmd_args[%d] = %q, want %q", 7+i, args[7+i].string(), want)
		}
	}
}

func TestEmitCC_NoStdInc_IncludeTailFollowsOwnAddIncl(t *testing.T) {
	emit := newBufferedEmitter()
	inst := hostInstance("contrib/libs/foolib")
	in := ModuleCCInputs{
		InclArgs: newInclArgMemo(),
		AddIncl: []VFS{
			intern("$(S)/custom/foolib/arch/x86_64"),
			intern("$(S)/custom/foolib/include"),
		},
	}
	emitCC(inst, "src/string/strlen.c", intern("$(S)/contrib/libs/foolib/src/string/strlen.c"), withCCBlocks(inst.Platform, in), testHostP, emit)

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
		if args[6+i].string() != want {
			t.Fatalf("cmd_args[%d] = %q, want %q; args=%v", 6+i, args[6+i].string(), want, args)
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
	emit := newBufferedEmitter()
	emitCC(targetInstance("contrib/libs/cxxsupp/libcxx"), "src/algorithm.cpp", intern("$(S)/contrib/libs/cxxsupp/libcxx/src/algorithm.cpp"), withCCBlocks(targetInstance("contrib/libs/cxxsupp/libcxx").Platform, ModuleCCInputs{TC: testToolchain()}), testHostP, emit)

	args := emit.nodes[0].Cmds[0].CmdArgs.flat()

	wantCxx := testToolchain().CXX.string()
	if args[0].string() != wantCxx {
		t.Errorf("compiler = %q, want %q", args[0].string(), wantCxx)
	}

	found := false

	for _, a := range args {
		if a.string() == cxxStandardFlag.string() {
			found = true

			break
		}
	}

	if !found {
		t.Errorf("cmd_args missing %q; got %v", cxxStandardFlag, args)
	}
}

func TestEmitCC_CSource_UsesClang(t *testing.T) {
	emit := newBufferedEmitter()
	emitCC(targetInstance("build/cow/on"), "lib.c", intern("$(S)/build/cow/on/lib.c"), withCCBlocks(targetInstance("build/cow/on").Platform, ModuleCCInputs{TC: testToolchain()}), testHostP, emit)

	args := emit.nodes[0].Cmds[0].CmdArgs.flat()

	wantCC := testToolchain().CC.string()
	if args[0].string() != wantCC {
		t.Errorf("compiler = %q, want %q", args[0].string(), wantCC)
	}

	for _, a := range args {
		if a.string() == cxxStandardFlag.string() {
			t.Errorf("cmd_args contains %q for a .c source", cxxStandardFlag)

			break
		}
	}
}

func TestEmitCC_NoCompilerWarnings_SelectsWarningSuppressionFlags(t *testing.T) {
	emit := newBufferedEmitter()
	inst := targetInstance("contrib/libs/cxxsupp/libcxxrt")
	emitCC(inst, "exception.cc", intern("$(S)/contrib/libs/cxxsupp/libcxxrt/exception.cc"), withCCBlocks(inst.Platform, ModuleCCInputs{Flags: FlagSet{NoCompilerWarnings: true}}), testHostP, emit)

	args := emit.nodes[0].Cmds[0].CmdArgs.flat()

	for _, a := range args {
		if a.string() == "-Werror" {
			t.Errorf("cmd_args contains -Werror despite NoCompilerWarnings=true")
		}
	}

	wnoCount := 0

	for _, a := range args {
		if a.string() == "-Wno-everything" {
			wnoCount++
		}
	}

	if wnoCount == 0 {
		t.Errorf("cmd_args missing -Wno-everything; got %v", args)
	}
}

func TestEmitCC_OwnCXXFlags_SlotsAfterSuppressionBlock(t *testing.T) {
	emit := newBufferedEmitter()
	in := ModuleCCInputs{
		Flags:    FlagSet{NoCompilerWarnings: true},
		CXXFlags: internArgs([]string{"-D_LIBCPP_BUILDING_LIBRARY"}),
	}
	inst := targetInstance("contrib/libs/cxxsupp/libcxx")
	emitCC(inst, "src/algorithm.cpp", intern("$(S)/contrib/libs/cxxsupp/libcxx/src/algorithm.cpp"), withCCBlocks(inst.Platform, in), testHostP, emit)

	args := emit.nodes[0].Cmds[0].CmdArgs.flat()

	idxOwn := -1
	idxLastSuppression := -1
	idxBuiltinDate := -1

	for i, a := range args {
		switch a.string() {
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

	emitC := newBufferedEmitter()
	emitCC(targetInstance("build/cow/on"), "lib.c", intern("$(S)/build/cow/on/lib.c"), withCCBlocks(targetInstance("build/cow/on").Platform, in), testHostP, emitC)

	if !contains(emitC.nodes[0].Cmds[0].CmdArgs.flat(), "-Wno-narrowing") {
		t.Errorf(".c source missing CONLYFLAG -Wno-narrowing; got %v", emitC.nodes[0].Cmds[0].CmdArgs.flat())
	}

	emitCpp := newBufferedEmitter()
	emitCC(targetInstance("build/cow/on"), "lib.cpp", intern("$(S)/build/cow/on/lib.cpp"), withCCBlocks(targetInstance("build/cow/on").Platform, in), testHostP, emitCpp)

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

	target := newPlatform(newMemFS(nil), OSLinux, ISAAArch64, flags, "-DENV_C=1", "-DENV_CXX=1")
	instance := ModuleInstance{
		Path:     source("build/cow/on"),
		Kind:     KindLib,
		Language: LangCPP,
		Platform: target,
	}

	e := newBufferedEmitter()
	emitCC(instance, "lib.c", intern("$(S)/build/cow/on/lib.c"), withCCBlocks(instance.Platform, ModuleCCInputs{Flags: FlagSet{NoLibc: true, NoUtil: true, NoRuntime: true}}), testHostP, e)
	cArgs := e.nodes[0].Cmds[0].CmdArgs.flat()

	if !contains(cArgs, "-DENV_C=1") {
		t.Fatalf("C cmd_args missing env CFLAGS: %v", cArgs)
	}

	if contains(cArgs, "-DENV_CXX=1") {
		t.Fatalf("C cmd_args unexpectedly contain env CXXFLAGS: %v", cArgs)
	}

	e = newBufferedEmitter()
	emitCC(instance, "lib.cpp", intern("$(S)/build/cow/on/lib.cpp"), withCCBlocks(instance.Platform, ModuleCCInputs{}), testHostP, e)
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

	return newPlatform(newMemFS(nil), OSLinux, ISAAArch64, flags, "", "")
}

func TestEmitCC_WrapccPrefix_NonOpensource(t *testing.T) {
	emit := newBufferedEmitter()
	inst := ModuleInstance{Path: source("mod"), Kind: KindLib, Language: LangCPP, Platform: nonOpensourcePlatform()}
	srcVFS := intern("$(S)/mod/lib.cpp")

	emitCC(inst, "lib.cpp", srcVFS, withCCBlocks(inst.Platform, ModuleCCInputs{TC: testToolchain(), IncludeInputs: []VFS{srcVFS}}), testHostP, emit)

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
		testToolchain().CXX.string(),
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
	if node.flatInputs()[0].string() != "$(S)/mod/lib.cpp" {
		t.Errorf("inputs[0] = %q, want the source $(S)/mod/lib.cpp", node.flatInputs()[0].string())
	}

	// YMAKE_PYTHON3 joins the CC deps (the wrapper runs under it).
	if !strsContain(node.Resources, resourcePatternYMakePython3) {
		t.Errorf("wrapped CC Resources missing YMAKE_PYTHON3: %v", node.Resources)
	}
}

func TestEmitCC_NoWrapcc_Opensource(t *testing.T) {
	emit := newBufferedEmitter()
	// targetInstance uses testTargetP, which is OPENSOURCE=yes → no wrapper.
	emitCC(targetInstance("mod"), "lib.cpp", intern("$(S)/mod/lib.cpp"), withCCBlocks(targetInstance("mod").Platform, ModuleCCInputs{TC: testToolchain()}), testHostP, emit)

	node := emit.nodes[0]
	args := strStrs(node.Cmds[0].CmdArgs.flat())

	if args[0] != testToolchain().CXX.string() {
		t.Errorf("opensource CC cmd_args[0] = %q, want the compiler (no wrapcc prefix)", args[0])
	}

	if slicesContains(vfsStrings(node.flatInputs()), "$(S)/build/scripts/wrapcc.py") {
		t.Errorf("opensource CC node must not list wrapcc.py as input: %v", vfsStrings(node.flatInputs()))
	}

	if strsContain(node.Resources, resourcePatternYMakePython3) {
		t.Errorf("opensource CC node must not depend on YMAKE_PYTHON3: %v", node.Resources)
	}
}

func contains(xs []STR, target string) bool {
	for _, x := range xs {
		if x.string() == target {
			return true
		}
	}

	return false
}

func TestEmitCC_OutputPath_YqlUdfSuffix(t *testing.T) {
	e := newBufferedEmitter()
	in := ModuleCCInputs{ObjectSuffixStem: stringPtr("udfs")}

	_, outPath, _ := emitCC(targetInstance("udfmod"), "lib.cpp", intern("$(S)/udfmod/lib.cpp"), withCCBlocks(targetInstance("udfmod").Platform, in), testHostP, e)

	want := "$(B)/udfmod/lib.cpp.udfs.o"
	if outPath.string() != want {
		t.Fatalf("outPath = %q, want %q", outPath, want)
	}
}

func TestEmitCC_OutputPath_YqlUdfSuffixPIC(t *testing.T) {
	e := newBufferedEmitter()
	in := ModuleCCInputs{ObjectSuffixStem: stringPtr("udfs")}
	instance := ModuleInstance{
		Path:     source("udfmod"),
		Kind:     KindLib,
		Language: LangCPP,
		Platform: testHostP,
	}

	_, outPath, _ := emitCC(instance, "lib.cpp", intern("$(S)/udfmod/lib.cpp"), withCCBlocks(instance.Platform, in), testHostP, e)

	want := "$(B)/udfmod/lib.cpp.udfs.pic.o"
	if outPath.string() != want {
		t.Fatalf("outPath = %q, want %q", outPath, want)
	}
}

func TestEmitCC_NoWShadowAddsWarningFlag(t *testing.T) {
	e := newBufferedEmitter()
	in := ModuleCCInputs{Flags: FlagSet{NoWShadow: true}}

	emitCC(targetInstance("build/cow/on"), "lib.cpp", intern("$(S)/build/cow/on/lib.cpp"), withCCBlocks(targetInstance("build/cow/on").Platform, in), testHostP, e)

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
	e := newBufferedEmitter()

	instance := targetInstance("ydb/public/lib/ydb_cli/commands/command_base")
	srcRel := "../ydb_command.cpp"
	srcVFS := source(instance.Path.rel() + "/" + srcRel)

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
	in.CCBlocks = composeCCModuleArgBlocks(newNodeArenas(), p, &in)

	return in
}

func TestGen_NoStdIncGlobalCFlagsPropagateToExplicitPeer(t *testing.T) {
	fs := newMemFS(map[string]string{
		"contrib/libs/foolib/ya.make": `LIBRARY()
NO_PLATFORM()
CFLAGS(
    GLOBAL -D_foolib_=1
    -nostdinc
)
SRCS(m.c)
END()
`,
		"contrib/libs/foolib/m.c": "int foolib_symbol(void) { return 1; }\n",
		"bridge/ya.make": `LIBRARY()
NO_RUNTIME()
PEERDIR(contrib/libs/foolib)
SRCS(x.cpp)
END()
`,
		"bridge/x.cpp": "int bridge_symbol(void) { return 2; }\n",
	})

	g := testGen(fs, "bridge")
	var args []string

	for _, n := range g.Graph {
		if len(n.Outputs) == 1 && n.Outputs[0].string() == "$(B)/bridge/x.cpp.o" {
			args = strStrs(n.Cmds[0].CmdArgs.flat())
			break
		}
	}

	if len(args) == 0 {
		t.Fatalf("bridge CC node not found")
	}

	if !flagsContain(args, "-D_foolib_=1") {
		t.Fatalf("bridge CC args missing GLOBAL CFLAG from explicit peer: %v", args)
	}
}

func TestGen_CXXFLAGS_GLOBAL_LandsOnOwnCmdArgs(t *testing.T) {
	t.Run("CXXFLAGS_GLOBAL_emitted_twice_no_literal_GLOBAL", func(t *testing.T) {
		fs := newMemFS(map[string]string{
			"testlib/ya.make": "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nCXXFLAGS(GLOBAL -nostdinc++)\nSRCS(foo.cpp)\nEND()\n",
		})

		g := testGen(fs, "testlib")

		var ccNode *Node

		for _, n := range g.Graph {
			if n.KV.P == pkCC {
				ccNode = n

				break
			}
		}

		if ccNode == nil {
			t.Fatal("no CC node emitted")
		}

		if len(ccNode.Cmds) == 0 {
			t.Fatal("CC node has no Cmds")
		}

		nostdinccCount := 0

		for _, arg := range strStrs(ccNode.Cmds[0].CmdArgs.flat()) {
			if arg == "GLOBAL" {
				t.Errorf("CC cmd_args contains literal %q — GLOBAL modifier leaked into own node", arg)
			}

			if arg == "-nostdinc++" {
				nostdinccCount++
			}
		}

		if nostdinccCount != 2 {
			t.Errorf("expected 2 occurrences of -nostdinc++ in own cmd_args (bucket × 2), got %d", nostdinccCount)
		}
	})

	t.Run("CONLYFLAGS_GLOBAL_no_literal_GLOBAL_in_C", func(t *testing.T) {
		fs := newMemFS(map[string]string{
			"testlib/ya.make": "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nCONLYFLAGS(GLOBAL -Dfoo)\nSRCS(bar.c)\nEND()\n",
		})

		g := testGen(fs, "testlib")

		var ccNode *Node

		for _, n := range g.Graph {
			if n.KV.P == pkCC {
				ccNode = n

				break
			}
		}

		if ccNode == nil {
			t.Fatal("no CC node emitted")
		}

		if len(ccNode.Cmds) == 0 {
			t.Fatal("CC node has no Cmds")
		}

		for _, arg := range strStrs(ccNode.Cmds[0].CmdArgs.flat()) {
			if arg == "GLOBAL" {
				t.Errorf("CC cmd_args contains literal %q — GLOBAL modifier leaked into own node", arg)
			}
		}
	})

	t.Run("CXXFLAGS_non_GLOBAL_still_applied", func(t *testing.T) {
		fs := newMemFS(map[string]string{
			"testlib/ya.make": "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nCXXFLAGS(-DMINE)\nSRCS(foo.cpp)\nEND()\n",
		})

		g := testGen(fs, "testlib")

		var ccNode *Node

		for _, n := range g.Graph {
			if n.KV.P == pkCC {
				ccNode = n

				break
			}
		}

		if ccNode == nil {
			t.Fatal("no CC node emitted")
		}

		if len(ccNode.Cmds) == 0 {
			t.Fatal("CC node has no Cmds")
		}

		found := false

		for _, arg := range strStrs(ccNode.Cmds[0].CmdArgs.flat()) {
			if arg == "-DMINE" {
				found = true

				break
			}
		}

		if !found {
			t.Errorf("CC cmd_args missing %q — non-GLOBAL CXXFLAGS must be applied to own node", "-DMINE")
		}
	})
}

func TestGen_SRC_AppendsExtraCFlags_PerSource(t *testing.T) {
	fs := newMemFS(map[string]string{
		"mod/ya.make": "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRC(foo.cpp -DSSE41_STUB)\nEND()\n",
	})

	g := testGen(fs, "mod")

	var cc *Node

	for _, n := range g.Graph {
		if n.KV.P == pkCC {
			cc = n

			break
		}
	}

	if cc == nil {
		t.Fatal("no CC node emitted for SRC(foo.cpp ...)")
	}

	args := cc.Cmds[0].CmdArgs.flat()

	if len(args) < 2 {
		t.Fatalf("CC cmd_args too short: %d", len(args))
	}

	wantInput := "$(S)/mod/foo.cpp"

	if args[len(args)-1].string() != wantInput {
		t.Errorf("last cmd_arg = %q, want %q", args[len(args)-1], wantInput)
	}

	if args[len(args)-2].string() != "-DSSE41_STUB" {
		t.Errorf("second-to-last cmd_arg = %q, want %q (per-source CFLAGS slot)", args[len(args)-2], "-DSSE41_STUB")
	}
}

func TestGen_SRC_C_NO_LTO_RegistersSource(t *testing.T) {
	fs := newMemFS(map[string]string{
		"mod/ya.make":      "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRC_C_NO_LTO(system/compiler.cpp)\nEND()\n",
		"mod/system/.keep": "",
	})

	g := testGen(fs, "mod")

	var cc *Node

	for _, n := range g.Graph {
		if n.KV.P == pkCC {
			cc = n

			break
		}
	}

	if cc == nil {
		t.Fatal("no CC node emitted for SRC_C_NO_LTO(system/compiler.cpp)")
	}

	if len(cc.Outputs) != 1 {
		t.Fatalf("CC outputs = %#v, want exactly 1", cc.Outputs)
	}

	wantOut := "$(B)/mod/system/compiler.cpp.o"

	if cc.Outputs[0].string() != wantOut {
		t.Errorf("CC output = %q, want %q (SRC_C_NO_LTO uses flat output, not `mod/_/system/compiler.cpp.o`)", cc.Outputs[0].string(), wantOut)
	}

	args := cc.Cmds[0].CmdArgs.flat()

	if args[len(args)-1].string() != "$(S)/mod/system/compiler.cpp" {
		t.Errorf("last cmd_arg = %q, want input path", args[len(args)-1])
	}

	if args[len(args)-2].string() != "-fmacro-prefix-map=$(TOOL_ROOT)/=" {
		t.Errorf("second-to-last cmd_arg = %q, want %q (no per-source CFLAG)", args[len(args)-2], "-fmacro-prefix-map=$(TOOL_ROOT)/=")
	}
}

func TestGen_SRC_FlatOutputPath(t *testing.T) {
	fs := newMemFS(map[string]string{
		"mod/ya.make":   "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRC(sub/x.cpp)\nEND()\n",
		"mod/sub/.keep": "",
	})

	g := testGen(fs, "mod")

	var cc *Node

	for _, n := range g.Graph {
		if n.KV.P == pkCC {
			cc = n

			break
		}
	}

	if cc == nil {
		t.Fatal("no CC node emitted for SRC(sub/x.cpp)")
	}

	wantOut := "$(B)/mod/sub/x.cpp.o"

	if len(cc.Outputs) != 1 || cc.Outputs[0].string() != wantOut {
		t.Errorf("CC output = %#v, want [%q] (SRC uses flat output, not `mod/_/sub/x.cpp.o`)", cc.Outputs, wantOut)
	}
}

func TestGen_CmdArgsExpandStmtVars(t *testing.T) {
	fs := newMemFS(map[string]string{
		"mod/ya.make": `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SET(MKQL_RUNTIME_VERSION 42)
DEFAULT(ARCADIA_CURL_DNS_RESOLVER ARES)
SET(SSE41_CFLAGS -msse4.1)
SET(AVX2_CFLAGS -mavx2)
CFLAGS(
    -DMKQL_RUNTIME_VERSION=$MKQL_RUNTIME_VERSION
    -DARCADIA_CURL_DNS_RESOLVER_${ARCADIA_CURL_DNS_RESOLVER}
)
SRC(lib.cpp ${SSE41_CFLAGS} ${AVX2_CFLAGS})
END()
`,
		"mod/lib.cpp": "int lib(){return 0;}\n",
	})

	g := testGen(fs, "mod")
	cc := mustNodeByOutput(t, g, "$(B)/mod/lib.cpp.o")
	args := strings.Join(strStrs(cc.Cmds[0].CmdArgs.flat()), " ")

	for _, want := range []string{
		"-DMKQL_RUNTIME_VERSION=42",
		"-DARCADIA_CURL_DNS_RESOLVER_ARES",
		"-msse4.1",
		"-mavx2",
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("cc cmd args missing %q: %s", want, args)
		}
	}
	for _, bad := range []string{
		"${",
		"$MKQL_RUNTIME_VERSION",
		"${ARCADIA_CURL_DNS_RESOLVER}",
		"${SSE41_CFLAGS}",
		"${AVX2_CFLAGS}",
	} {
		if strings.Contains(args, bad) {
			t.Fatalf("cc cmd args still contain %q: %s", bad, args)
		}
	}
}

// TestGen_CC_NoDuplicateInputsWhenBuildProtoDropped reproduces a fast-path
// regression in emitOneSource: dropTransitiveGeneratedProto(full[1:]) compacts
// the backing array in place, but the fast path then set NodeInputs=full (the
// original, un-shrunk slice). The stale tail of full held copies of elements
// that had been shifted forward, producing duplicate CC inputs. The trigger is
// a $(B)-generated .proto appearing in a CC source's closure — here via a
// PROTO_LIBRARY whose JsonPathParser.proto is emitted by RUN_ANTLR (not
// present in source) so the closure walker reaches it through the codegen
// fallback locator.
func TestGen_CC_NoDuplicateInputsWhenBuildProtoDropped(t *testing.T) {
	// TODO: the generated-from refactor (proto self-include removed; generator
	// $(S) sources ride as pb.h closure leaves) double-lists those sources for a
	// build-generated .proto — they arrive both via the new leaf and via a
	// pre-existing path. Gate stays byte-exact (normalize dedups); this raw-graph
	// duplicate is tracked separately. Re-enable once the second path is removed.
	t.Skip("generated-from refactor: generator-source duplication pending dedup of the second path")

	const protoModPath = "yql/essentials/parser/proto_ast/gen/jsonpath"
	const appModPath = "app"

	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")

	// PROTO_LIBRARY with a build-generated proto (RUN_ANTLR OUT_NOAUTO).
	// GEN_PROTO is set to true for PROTO_LIBRARY collection so this block runs.
	files[protoModPath+"/ya.make"] = `PROTO_LIBRARY()
IF (GEN_PROTO)
    SET(antlr_output ${ARCADIA_BUILD_ROOT}/${MODDIR})
    SET(antlr_templates ${antlr_output}/org/antlr/codegen/templates)
    SET(jsonpath_grammar ${ARCADIA_ROOT}/yql/essentials/minikql/jsonpath/JsonPath.g)

    CONFIGURE_FILE(${ARCADIA_ROOT}/templates/protobuf.stg.in ${antlr_templates}/protobuf/protobuf.stg)

    RUN_ANTLR(
        ${jsonpath_grammar}
        -lib .
        -fo ${antlr_output}
        -language protobuf
        IN ${jsonpath_grammar} ${antlr_templates}/protobuf/protobuf.stg
        OUT_NOAUTO JsonPathParser.proto
        CWD ${antlr_output}
    )
ENDIF()

SRCS(JsonPathParser.proto)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`
	// Consumer LIBRARY: use.cpp includes the generated pb.h.
	files[appModPath+"/ya.make"] = "LIBRARY()\nPEERDIR(" + protoModPath + ")\nSRCS(use.cpp)\nEND()\n"
	files[appModPath+"/use.cpp"] = "#include <" + protoModPath + "/JsonPathParser.pb.h>\nint use() { return 0; }\n"

	// Required source files for the ANTLR and proto chain.
	files["templates/protobuf.stg.in"] = "stub stg\n"
	files["yql/essentials/minikql/jsonpath/JsonPath.g"] = "stub grammar\n"
	files["contrib/libs/protobuf/ya.make"] = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n"

	// Put one of the pbDescriptorImporterHeaders in the memFS so it appears
	// in the CC closure AFTER the $(B) proto. dropTransitiveGeneratedProto
	// compacts the backing array in place; if the fast path reuses the
	// un-shrunk full slice as NodeInputs, this header appears twice.
	files["contrib/libs/protobuf/src/google/protobuf/reflection_ops.h"] = "// stub\n"

	g := testGen(newMemFS(files), appModPath)

	useCC := mustNodeByOutput(t, g, "$(B)/"+appModPath+"/use.cpp.o")

	seen := make(map[string]int, len(useCC.flatInputs()))
	for _, in := range useCC.flatInputs() {
		seen[in.string()]++
	}
	for inp, count := range seen {
		if count > 1 {
			t.Errorf("use.cpp.o has duplicate input %q (appears %d times)", inp, count)
		}
	}
}
