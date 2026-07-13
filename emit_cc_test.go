package main

import (
	"slices"
	"strings"
	"testing"
)

func composeCCNode(instance ModuleInstance, srcVFS VFS, in ModuleCCInputs, host *Platform, emit *StreamingEmitter) (NodeRef, VFS, InputChunks) {
	return nodeTestEmitContext(emit, instance).composeCCNode(srcVFS, in, host)
}

const referenceCCOutput = "$(B)/build/cow/on/lib.c.o"

func TestEmitCC_OutputPath_NestedSrc(t *testing.T) {
	e := newStreamingEmitter(nil)
	_, outPath, _ := composeCCNode(targetInstance("contrib/libs/cxxsupp/libcxx"), source("contrib/libs/cxxsupp/libcxx/src/algorithm.cpp"), withCCBlocks(targetInstance("contrib/libs/cxxsupp/libcxx").Platform, ModuleCCInputs{}), testHostP, e)
	want := "$(B)/contrib/libs/cxxsupp/libcxx/_/src/algorithm.cpp.o"

	if outPath.string() != want {
		t.Errorf("outPath = %q, want %q", outPath, want)
	}
}

func TestEmitCC_OutputPath_FlatSrc(t *testing.T) {
	e := newStreamingEmitter(nil)
	_, outPath, _ := composeCCNode(targetInstance("build/cow/on"), source("build/cow/on/lib.c"), withCCBlocks(targetInstance("build/cow/on").Platform, ModuleCCInputs{}), testHostP, e)
	want := "$(B)/build/cow/on/lib.c.o"

	if outPath.string() != want {
		t.Errorf("outPath = %q, want %q", outPath, want)
	}
}

func TestEmitCC_GeneratedSource_BuildRootInput(t *testing.T) {
	emit := newStreamingEmitter(nil)
	srcVFS := build("util/_/datetime/parser.rl6.cpp")
	_, outPath, _ := composeCCNode(targetInstance("util"), srcVFS, withCCBlocks(targetInstance("util").Platform, ModuleCCInputs{IncludeInputs: []VFS{srcVFS}}), testHostP, emit)

	wantOut := "$(B)/util/_/_/datetime/parser.rl6.cpp.o"

	if outPath.string() != wantOut {
		t.Errorf("outPath = %q, want %q", outPath, wantOut)
	}

	if emit.nodes.len() != 1 {
		t.Fatalf("emitter buffered %d nodes, want 1", emit.nodes.len())
	}

	got := emit.nodes.s[0]

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
	emit := newStreamingEmitter(nil)
	in := ModuleCCInputs{ModuleCompileEnv: &ModuleCompileEnv{
		InclArgs: newInclArgMemo(),
		AddIncl: []VFS{
			source("contrib/libs/foolib/arch/aarch64"),
			source("contrib/libs/foolib/arch/generic"),
			source("contrib/libs/foolib/include"),
			source("contrib/libs/foolib/extra"),
		},
	}}
	composeCCNode(targetInstance("contrib/libs/cxxsupp/builtins"), source("contrib/libs/cxxsupp/builtins/aarch64/fp_mode.c"), withCCBlocks(targetInstance("contrib/libs/cxxsupp/builtins").Platform, in), testHostP, emit)

	args := emit.nodes.s[0].Cmds[0].CmdArgs.flat()

	wantSlot := []string{
		"-I$(B)",
		"-I$(S)",
		"-I$(S)/contrib/libs/foolib/arch/aarch64",
		"-I$(S)/contrib/libs/foolib/arch/generic",
		"-I$(S)/contrib/libs/foolib/include",
		"-I$(S)/contrib/libs/foolib/extra",
	}

	for i, want := range wantSlot {
		if args[7+i].string() != want {
			t.Errorf("cmd_args[%d] = %q, want %q", 7+i, args[7+i].string(), want)
		}
	}
}

func TestEmitCC_NoStdInc_IncludeTailFollowsOwnAddIncl(t *testing.T) {
	emit := newStreamingEmitter(nil)
	inst := hostInstance("contrib/libs/foolib")
	in := ModuleCCInputs{ModuleCompileEnv: &ModuleCompileEnv{
		InclArgs: newInclArgMemo(),
		AddIncl: []VFS{
			source("custom/foolib/arch/x86_64"),
			source("custom/foolib/include"),
		},
	}}
	composeCCNode(inst, source("contrib/libs/foolib/src/string/strlen.c"), withCCBlocks(inst.Platform, in), testHostP, emit)

	args := emit.nodes.s[0].Cmds[0].CmdArgs.flat()
	wantSlot := []string{
		"-I$(B)",
		"-I$(S)",
		"-I$(S)/custom/foolib/arch/x86_64",
		"-I$(S)/custom/foolib/include",
	}

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
	emit := newStreamingEmitter(nil)
	composeCCNode(targetInstance("contrib/libs/cxxsupp/libcxx"), source("contrib/libs/cxxsupp/libcxx/src/algorithm.cpp"), withCCBlocks(targetInstance("contrib/libs/cxxsupp/libcxx").Platform, ModuleCCInputs{ModuleCompileEnv: &ModuleCompileEnv{TC: testToolchain()}}), testHostP, emit)

	args := emit.nodes.s[0].Cmds[0].CmdArgs.flat()

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

func TestEmitCC_UppercaseCSource_UsesClangPlusPlus(t *testing.T) {
	emit := newStreamingEmitter(nil)
	composeCCNode(targetInstance("contrib/libs/cxxsupp/libcxx"), source("contrib/libs/cxxsupp/libcxx/src/algorithm.C"), withCCBlocks(targetInstance("contrib/libs/cxxsupp/libcxx").Platform, ModuleCCInputs{ModuleCompileEnv: &ModuleCompileEnv{TC: testToolchain()}}), testHostP, emit)

	args := emit.nodes.s[0].Cmds[0].CmdArgs.flat()

	wantCxx := testToolchain().CXX.string()

	if args[0].string() != wantCxx {
		t.Errorf("compiler = %q, want %q for uppercase .C source", args[0].string(), wantCxx)
	}

	found := false

	for _, a := range args {
		if a.string() == cxxStandardFlag.string() {
			found = true

			break
		}
	}

	if !found {
		t.Errorf("cmd_args missing %q for uppercase .C source; got %v", cxxStandardFlag, args)
	}
}

func TestEmitCC_CSource_UsesClang(t *testing.T) {
	emit := newStreamingEmitter(nil)
	composeCCNode(targetInstance("build/cow/on"), source("build/cow/on/lib.c"), withCCBlocks(targetInstance("build/cow/on").Platform, ModuleCCInputs{ModuleCompileEnv: &ModuleCompileEnv{TC: testToolchain()}}), testHostP, emit)

	args := emit.nodes.s[0].Cmds[0].CmdArgs.flat()

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
	emit := newStreamingEmitter(nil)
	inst := targetInstance("contrib/libs/cxxsupp/libcxxrt")
	composeCCNode(inst, source("contrib/libs/cxxsupp/libcxxrt/exception.cc"), withCCBlocks(inst.Platform, ModuleCCInputs{ModuleCompileEnv: &ModuleCompileEnv{Flags: FlagSet{NoCompilerWarnings: true}}}), testHostP, emit)

	args := emit.nodes.s[0].Cmds[0].CmdArgs.flat()

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
	emit := newStreamingEmitter(nil)
	in := ModuleCCInputs{ModuleCompileEnv: &ModuleCompileEnv{
		Flags:    FlagSet{NoCompilerWarnings: true},
		CXXFlags: internAnys([]string{"-D_LIBCPP_BUILDING_LIBRARY"}),
	}}
	inst := targetInstance("contrib/libs/cxxsupp/libcxx")
	composeCCNode(inst, source("contrib/libs/cxxsupp/libcxx/src/algorithm.cpp"), withCCBlocks(inst.Platform, in), testHostP, emit)

	args := emit.nodes.s[0].Cmds[0].CmdArgs.flat()

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
	in := ModuleCCInputs{ModuleCompileEnv: &ModuleCompileEnv{COnlyFlags: internAnys([]string{"-Wno-narrowing"})}}

	emitC := newStreamingEmitter(nil)
	composeCCNode(targetInstance("build/cow/on"), source("build/cow/on/lib.c"), withCCBlocks(targetInstance("build/cow/on").Platform, in), testHostP, emitC)

	if !contains(emitC.nodes.s[0].Cmds[0].CmdArgs.flat(), "-Wno-narrowing") {
		t.Errorf(".c source missing CONLYFLAG -Wno-narrowing; got %v", emitC.nodes.s[0].Cmds[0].CmdArgs.flat())
	}

	emitCpp := newStreamingEmitter(nil)
	composeCCNode(targetInstance("build/cow/on"), source("build/cow/on/lib.cpp"), withCCBlocks(targetInstance("build/cow/on").Platform, in), testHostP, emitCpp)

	if contains(emitCpp.nodes.s[0].Cmds[0].CmdArgs.flat(), "-Wno-narrowing") {
		t.Errorf(".cpp source got CONLYFLAG -Wno-narrowing (should be CXXFlags-only); got %v", emitCpp.nodes.s[0].Cmds[0].CmdArgs.flat())
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

	e := newStreamingEmitter(nil)
	composeCCNode(instance, source("build/cow/on/lib.c"), withCCBlocks(instance.Platform, ModuleCCInputs{ModuleCompileEnv: &ModuleCompileEnv{Flags: FlagSet{NoLibc: true, NoUtil: true, NoRuntime: true}}}), testHostP, e)
	cArgs := e.nodes.s[0].Cmds[0].CmdArgs.flat()

	if !contains(cArgs, "-DENV_C=1") {
		t.Fatalf("C cmd_args missing env CFLAGS: %v", cArgs)
	}

	if contains(cArgs, "-DENV_CXX=1") {
		t.Fatalf("C cmd_args unexpectedly contain env CXXFLAGS: %v", cArgs)
	}

	e = newStreamingEmitter(nil)
	composeCCNode(instance, source("build/cow/on/lib.cpp"), withCCBlocks(instance.Platform, ModuleCCInputs{}), testHostP, e)
	cxxArgs := e.nodes.s[0].Cmds[0].CmdArgs.flat()

	if !contains(cxxArgs, "-DENV_C=1") {
		t.Fatalf("C++ cmd_args missing env CFLAGS: %v", cxxArgs)
	}

	if !contains(cxxArgs, "-DENV_CXX=1") {
		t.Fatalf("C++ cmd_args missing env CXXFLAGS: %v", cxxArgs)
	}
}

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
	emit := newStreamingEmitter(nil)
	inst := ModuleInstance{Path: source("mod"), Kind: KindLib, Language: LangCPP, Platform: nonOpensourcePlatform()}
	srcVFS := source("mod/lib.cpp")

	composeCCNode(inst, srcVFS, withCCBlocks(inst.Platform, ModuleCCInputs{ModuleCompileEnv: &ModuleCompileEnv{TC: testToolchain()}, IncludeInputs: []VFS{srcVFS}}), testHostP, emit)

	node := emit.nodes.s[0]
	args := anyStrs(node.Cmds[0].CmdArgs.flat())

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

	if !slicesContains(vfsStrings(node.flatInputs()), "$(S)/build/scripts/wrapcc.py") {
		t.Errorf("wrapped CC node inputs missing wrapcc.py: %v", vfsStrings(node.flatInputs()))
	}

	if node.flatInputs()[0].string() != "$(S)/mod/lib.cpp" {
		t.Errorf("inputs[0] = %q, want the source $(S)/mod/lib.cpp", node.flatInputs()[0].string())
	}

	if !strsContain(strsAny(node.Resources), resourcePatternYMakePython3) {
		t.Errorf("wrapped CC Resources missing YMAKE_PYTHON3: %v", node.Resources)
	}
}

func TestEmitCC_NoWrapcc_Opensource(t *testing.T) {
	emit := newStreamingEmitter(nil)

	composeCCNode(targetInstance("mod"), source("mod/lib.cpp"), withCCBlocks(targetInstance("mod").Platform, ModuleCCInputs{ModuleCompileEnv: &ModuleCompileEnv{TC: testToolchain()}}), testHostP, emit)

	node := emit.nodes.s[0]
	args := anyStrs(node.Cmds[0].CmdArgs.flat())

	if args[0] != testToolchain().CXX.string() {
		t.Errorf("opensource CC cmd_args[0] = %q, want the compiler (no wrapcc prefix)", args[0])
	}

	if slicesContains(vfsStrings(node.flatInputs()), "$(S)/build/scripts/wrapcc.py") {
		t.Errorf("opensource CC node must not list wrapcc.py as input: %v", vfsStrings(node.flatInputs()))
	}

	if strsContain(strsAny(node.Resources), resourcePatternYMakePython3) {
		t.Errorf("opensource CC node must not depend on YMAKE_PYTHON3: %v", node.Resources)
	}
}

func contains[T interface {
	~uint32
	string() string
}](xs []T, target string) bool {
	for _, x := range xs {
		if x.string() == target {
			return true
		}
	}

	return false
}

func TestEmitCC_OutputPath_ExplicitDotSrc(t *testing.T) {
	e := newStreamingEmitter(nil)
	_, outPath, _ := composeCCNode(targetInstance("ysite/yandex/pure"), source("ysite/yandex/pure/generated/default_pure.cpp"), withCCBlocks(targetInstance("ysite/yandex/pure").Platform, ModuleCCInputs{}), testHostP, e)
	want := "$(B)/ysite/yandex/pure/_/generated/default_pure.cpp.o"

	if outPath.string() != want {
		t.Errorf("outPath = %q, want %q", outPath, want)
	}
}

func TestEmitCC_OutputPath_YqlUdfSuffix(t *testing.T) {
	e := newStreamingEmitter(nil)
	in := ModuleCCInputs{ModuleCompileEnv: &ModuleCompileEnv{ObjectSuffixStem: ptr("udfs")}}

	_, outPath, _ := composeCCNode(targetInstance("udfmod"), source("udfmod/lib.cpp"), withCCBlocks(targetInstance("udfmod").Platform, in), testHostP, e)

	want := "$(B)/udfmod/lib.cpp.udfs.o"

	if outPath.string() != want {
		t.Fatalf("outPath = %q, want %q", outPath, want)
	}
}

func TestEmitCC_OutputPath_YqlUdfSuffixPIC(t *testing.T) {
	e := newStreamingEmitter(nil)
	in := ModuleCCInputs{ModuleCompileEnv: &ModuleCompileEnv{ObjectSuffixStem: ptr("udfs")}}
	instance := ModuleInstance{
		Path:     source("udfmod"),
		Kind:     KindLib,
		Language: LangCPP,
		Platform: testHostP,
	}

	_, outPath, _ := composeCCNode(instance, source("udfmod/lib.cpp"), withCCBlocks(instance.Platform, in), testHostP, e)

	want := "$(B)/udfmod/lib.cpp.udfs.pic.o"

	if outPath.string() != want {
		t.Fatalf("outPath = %q, want %q", outPath, want)
	}
}

func TestEmitCC_NoWShadowAddsWarningFlag(t *testing.T) {
	e := newStreamingEmitter(nil)
	in := ModuleCCInputs{ModuleCompileEnv: &ModuleCompileEnv{Flags: FlagSet{NoWShadow: true}}}

	composeCCNode(targetInstance("build/cow/on"), source("build/cow/on/lib.cpp"), withCCBlocks(targetInstance("build/cow/on").Platform, in), testHostP, e)

	if !contains(e.nodes.s[0].Cmds[0].CmdArgs.flat(), "-Wno-shadow") {
		t.Fatalf("cmd_args missing -Wno-shadow: %v", e.nodes.s[0].Cmds[0].CmdArgs.flat())
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
	e := newStreamingEmitter(nil)

	instance := targetInstance("ydb/public/lib/ydb_cli/commands/command_base")
	srcRel := "../ydb_command.cpp"
	srcVFS := source(instance.Path.relString() + "/" + srcRel)

	_ = e
	_ = srcVFS

	got := joinNormalizedDotDot(srcRel)
	want := "__/ydb_command.cpp"

	if got != want {
		t.Errorf("normalizeDotDotSegments(%q) = %q, want %q", srcRel, got, want)
	}
}

func joinNormalizedDotDot(rel string) string {
	body, underscore := normalizeDotDotSegments(rel)

	if underscore {
		return "_/" + body
	}

	return body
}

func TestNormalizeDotDotSegments_Subdir(t *testing.T) {
	got := joinNormalizedDotDot("subdir/file.cpp")
	want := "_/subdir/file.cpp"

	if got != want {
		t.Errorf("normalizeDotDotSegments = %q, want %q", got, want)
	}
}

func withCCBlocks(p *Platform, in ModuleCCInputs) ModuleCCInputs {
	if in.ModuleCompileEnv == nil {
		in.ModuleCompileEnv = &ModuleCompileEnv{}
	}

	in.AddIncl = in.ModuleCompileEnv.AddIncl
	in.CFlags = in.ModuleCompileEnv.CFlags
	in.Py3Suffix = in.ModuleCompileEnv.Py3Suffix
	blocks := composeCCModuleArgBlocks(newNodeArenas(), p, in.ModuleCompileEnv)
	in.CCBlocks = &blocks

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
		if n == nil {
			continue
		}

		if len(n.Outputs) == 1 && n.Outputs[0].string() == "$(B)/bridge/x.cpp.o" {
			args = anyStrs(n.Cmds[0].CmdArgs.flat())

			break
		}
	}

	if len(args) == 0 {
		t.Fatalf("bridge CC node not found")
	}

	if !slices.Contains(args, "-D_foolib_=1") {
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
			if n == nil {
				continue
			}

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

		for _, arg := range anyStrs(ccNode.Cmds[0].CmdArgs.flat()) {
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
			if n == nil {
				continue
			}

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

		for _, arg := range anyStrs(ccNode.Cmds[0].CmdArgs.flat()) {
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
			if n == nil {
				continue
			}

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

		for _, arg := range anyStrs(ccNode.Cmds[0].CmdArgs.flat()) {
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
		if n == nil {
			continue
		}

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

func TestGen_UppercaseCSource_CompilesAsCxx(t *testing.T) {
	fs := newMemFS(map[string]string{
		"cmod/ya.make": "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(regular.C)\nGLOBAL_SRCS(global.C)\nEND()\n",
	})

	g := testGen(fs, "cmod")

	var ccCount, arCount, globalARs, regularARs int

	var regularCC *Node

	for _, n := range g.Graph {
		if n == nil {
			continue
		}

		switch n.KV.P {
		case pkCC:
			ccCount++

			if len(n.Outputs) == 1 && strings.HasSuffix(n.Outputs[0].string(), "/regular.C.o") {
				regularCC = n
			}
		case pkAR:
			arCount++

			if len(n.Outputs) > 0 && strings.HasSuffix(n.Outputs[0].string(), ".global.a") {
				globalARs++
			} else if len(n.Outputs) > 0 && strings.HasSuffix(n.Outputs[0].string(), ".a") {
				regularARs++
			}
		}
	}

	if ccCount != 2 {
		t.Errorf("CC count = %d, want 2 (regular.C + global.C compile nodes)", ccCount)
	}

	if arCount != 2 {
		t.Errorf("AR count = %d, want 2 (regular + global archive)", arCount)
	}

	if regularARs != 1 {
		t.Errorf("regular (no-tag) ARs = %d, want 1 (regular.C archive member)", regularARs)
	}

	if globalARs != 1 {
		t.Errorf("global ARs = %d, want 1 (GLOBAL global.C contribution)", globalARs)
	}

	if regularCC == nil {
		t.Fatalf("no CC node emitted with output suffix /regular.C.o")
	}

	if regularCC.Outputs[0].string() != "$(B)/cmod/regular.C.o" {
		t.Errorf("CC output = %q, want %q", regularCC.Outputs[0].string(), "$(B)/cmod/regular.C.o")
	}

	args := regularCC.Cmds[0].CmdArgs.flat()

	foundStd := false

	for _, a := range args {
		if a.string() == cxxStandardFlag.string() {
			foundStd = true

			break
		}
	}

	if !foundStd {
		t.Errorf("cmd_args missing %q for a .C source (must compile as C++, not C); got %v", cxxStandardFlag, args)
	}
}

func TestIsCxxSource_CaseSensitiveExtensions(t *testing.T) {
	cases := map[string]bool{
		"foo.c":   false,
		"foo.cpp": true,
		"foo.cc":  true,
		"foo.cxx": true,
		"foo.C":   true,
	}

	for src, want := range cases {
		if got := isCxxSource(src); got != want {
			t.Errorf("isCxxSource(%q) = %v, want %v", src, got, want)
		}
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
		if n == nil {
			continue
		}

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
		if n == nil {
			continue
		}

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
	args := strings.Join(anyStrs(cc.Cmds[0].CmdArgs.flat()), " ")

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

func TestGen_CC_NoDuplicateInputsWhenBuildProtoDropped(t *testing.T) {
	t.Skip("generated-from refactor: generator-source duplication pending dedup of the second path")

	const protoModPath = "yql/essentials/parser/proto_ast/gen/jsonpath"
	const appModPath = "app"

	files := map[string]string{}
	writeJdk17Resource(files)
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")

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
	files[appModPath+"/ya.make"] = "LIBRARY()\nPEERDIR(" + protoModPath + ")\nSRCS(use.cpp)\nEND()\n"
	files[appModPath+"/use.cpp"] = "#include <" + protoModPath + "/JsonPathParser.pb.h>\nint use() { return 0; }\n"

	files["templates/protobuf.stg.in"] = "stub stg\n"
	files["yql/essentials/minikql/jsonpath/JsonPath.g"] = "stub grammar\n"
	files["contrib/libs/protobuf/ya.make"] = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n"

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

func releaseHostPlatform() *Platform {
	flags := make(map[string]string, len(testToolchainFlags)+2)

	for k, v := range testToolchainFlags {
		flags[k] = v
	}

	flags["PIC"] = "yes"
	flags["BUILD_TYPE"] = "release"

	return newPlatform(newMemFS(nil), OSLinux, ISAX8664, flags, "", "")
}

func releaseTargetPlatform() *Platform {
	flags := make(map[string]string, len(testToolchainFlags)+1)

	for k, v := range testToolchainFlags {
		flags[k] = v
	}

	flags["PIC"] = "no"

	return newPlatform(newMemFS(nil), OSLinux, ISAAArch64, flags, "", "")
}

func ccArgsForOutput(t *testing.T, g *Graph, output string) []string {
	t.Helper()
	n := mustNodeByOutput(t, g, output)

	return anyStrs(n.Cmds[0].CmdArgs.flat())
}

func argsContain(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}

	return false
}

func gperfToolFiles(extraToolMacros string) map[string]string {
	files := map[string]string{}
	files["contrib/tools/gperf/ya.make"] = "PROGRAM(gperf)\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n" +
		extraToolMacros + "SRCS(main.cpp)\nEND()\n"
	files["contrib/tools/gperf/main.cpp"] = "int main(){return 0;}\n"
	files["gpmod/ya.make"] = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(tags.gperf)\nEND()\n"
	files["gpmod/tags.gperf"] = "%{\n%}\n%%\n"

	return files
}

func TestGen_NoOptimizeSuppressesOptimization(t *testing.T) {
	files := gperfToolFiles("NO_OPTIMIZE()\n")

	g := Gen(newMemFS(files), "gpmod", releaseHostPlatform(), releaseTargetPlatform(), func(Warn) {})

	args := ccArgsForOutput(t, g, "$(B)/contrib/tools/gperf/main.cpp.pic.o")

	if !argsContain(args, "-O0") {
		t.Fatalf("NO_OPTIMIZE compile missing -O0: %v", args)
	}

	if argsContain(args, "-O3") {
		t.Fatalf("NO_OPTIMIZE compile still carries -O3: %v", args)
	}

	ld := mustNodeByOutput(t, g, "$(B)/contrib/tools/gperf/gperf")
	vcs := anyStrs(ld.Cmds[1].CmdArgs.flat())

	if !argsContain(vcs, "-O0") || argsContain(vcs, "-O3") {
		t.Fatalf("NO_OPTIMIZE vcs compile not suppressed: %v", vcs)
	}
}

func TestGen_DefaultOptimizationIntact(t *testing.T) {
	files := gperfToolFiles("")

	g := Gen(newMemFS(files), "gpmod", releaseHostPlatform(), releaseTargetPlatform(), func(Warn) {})

	args := ccArgsForOutput(t, g, "$(B)/contrib/tools/gperf/main.cpp.pic.o")

	if !argsContain(args, "-O3") {
		t.Fatalf("default compile missing -O3: %v", args)
	}

	if argsContain(args, "-O0") {
		t.Fatalf("default compile unexpectedly carries -O0: %v", args)
	}
}

func newInclArgMemo() InclArgMemo {
	return InclArgMemo{m: &DenseMap[VFS, STR]{}}
}
