package main

import (
	"reflect"
	"testing"
)

const referenceASOutput = "$(B)/contrib/libs/cxxsupp/builtins/_/aarch64/chkstk.S.o"

var builtinsASOwnAddIncl = []VFS{
	source("contrib/libs/foolib/arch/aarch64"),
	source("contrib/libs/foolib/arch/generic"),
	source("contrib/libs/foolib/include"),
	source("contrib/libs/foolib/extra"),
}

func TestEmitAS_NoStdInc_IncludeTailFollowsOwnAddIncl(t *testing.T) {
	e := newStreamingEmitter(nil)
	inst := hostInstance("contrib/libs/foolib")
	in := ModuleCCInputs{ModuleCompileEnv: &ModuleCompileEnv{
		InclArgs: newInclArgMemo(),
		AddIncl: []VFS{
			source("custom/foolib/arch/x86_64"),
			source("custom/foolib/include"),
		},
	}}
	in = newModuleCCInputs(in.ModuleCompileEnv)
	nodeTestEmitContext(e, inst).emitAS("src/math/x86_64/ceill.s", source("contrib/libs/foolib/src/math/x86_64/ceill.s"), in, testHostP)

	args := e.nodes.s[0].Cmds[0].CmdArgs.flat()
	wantTail := []string{
		"-I$(B)",
		"-I$(S)",
		"-I$(S)/custom/foolib/arch/x86_64",
		"-I$(S)/custom/foolib/include",
	}
	start := len(args) - len(wantTail)

	for i, want := range wantTail {
		if args[start+i].string() != want {
			t.Fatalf("cmd_args[%d] = %q, want %q; args=%v", start+i, args[start+i].string(), want, args)
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

func TestEmitAS_OutputPath_FlatSrcRel(t *testing.T) {
	e := newStreamingEmitter(nil)
	_, outPath := nodeTestEmitContext(e, targetInstance("some/module")).emitAS("flat.S", source("some/module/flat.S"), newModuleCCInputs(&ModuleCompileEnv{}), testHostP)
	want := "$(B)/some/module/flat.S.o"

	if outPath.string() != want {
		t.Errorf("outPath = %q, want %q", outPath, want)
	}
}

func TestEmitAS_OutputPath_NestedSrc(t *testing.T) {
	e := newStreamingEmitter(nil)
	_, outPath := nodeTestEmitContext(e, targetInstance("contrib/libs/cxxsupp/builtins")).emitAS("aarch64/chkstk.S", source("contrib/libs/cxxsupp/builtins/aarch64/chkstk.S"), newModuleCCInputs(&ModuleCompileEnv{}), testHostP)
	want := "$(B)/contrib/libs/cxxsupp/builtins/_/aarch64/chkstk.S.o"

	if outPath.string() != want {
		t.Errorf("outPath = %q, want %q", outPath, want)
	}
}

func TestEmitAS_OutputPath_SrcDir(t *testing.T) {
	e := newStreamingEmitter(nil)

	in := ModuleCCInputs{ModuleCompileEnv: &ModuleCompileEnv{SrcDirs: []VFS{dirKey("contrib/libs/tcmalloc").source()}, FS: newMemFS(nil)}}
	in = newModuleCCInputs(in.ModuleCompileEnv)
	_, outPath := nodeTestEmitContext(e, targetInstance("contrib/libs/tcmalloc/no_percpu_cache")).emitAS(
		"tcmalloc/internal/percpu_rseq_asm.S",
		source("contrib/libs/tcmalloc/tcmalloc/internal/percpu_rseq_asm.S"),
		in,
		testHostP,
	)
	want := "$(B)/contrib/libs/tcmalloc/no_percpu_cache/__/tcmalloc/internal/percpu_rseq_asm.S.o"

	if outPath.string() != want {
		t.Errorf("outPath = %q, want %q", outPath, want)
	}
}

func testYasmLDRef(e *StreamingEmitter) NodeRef {
	return e.emitNode(Node{
		Cmds:     []Cmd{{CmdArgs: ArgChunks{ToAnySlice(appendInternStrs(nil, []string{"yasm"}))}, Env: nil}},
		Env:      nil,
		Inputs:   InputChunks{ToVFSSlice([]string{})},
		Outputs:  ToVFSSlice([]string{"$(B)/tools/yasm/yasm"}),
		KV:       &asTestKV,
		Platform: &Platform{Target: PlatformDefaultLinuxX8664},
	})
}

func TestEmitASYasm_YasmLD_PopulatesDepRefs(t *testing.T) {
	e := newStreamingEmitter(nil)
	_ = e.reserve()
	yasmLDRef := testYasmLDRef(e)

	yasmTestIn := ModuleCCInputs{ModuleCompileEnv: &ModuleCompileEnv{InclArgs: newInclArgMemo(), AddIncl: builtinsASOwnAddIncl}}
	yasmTestIn = newModuleCCInputs(yasmTestIn.ModuleCompileEnv)
	ref, _ := nodeTestEmitContext(e, hostInstance("contrib/libs/asmlib")).emitASYasm("memset64.asm", source("contrib/libs/asmlib/memset64.asm"), yasmTestIn, yasmLDRef)

	if e.nodes.len() != 3 {
		t.Fatalf("emitter buffered %d nodes, want 3", e.nodes.len())
	}

	_ = ref
	got := e.nodes.s[2]

	if len(got.ForeignDepRefs) != 1 || got.ForeignDepRefs[0] != yasmLDRef {
		t.Errorf("ForeignDepRefs = %v, want [%v]", got.ForeignDepRefs, yasmLDRef)
	}

	toolRefs := got.ForeignDepRefs

	if len(toolRefs) != 1 || toolRefs[0] != yasmLDRef {
		t.Errorf(`ForeignDepRefs = %v, want [%v]`, toolRefs, yasmLDRef)
	}
}

func TestEmitAS_KV(t *testing.T) {
	e := newStreamingEmitter(nil)
	nodeTestEmitContext(e, targetInstance("some/module")).emitAS("aarch64/foo.S", source("some/module/aarch64/foo.S"), newModuleCCInputs(&ModuleCompileEnv{}), testHostP)

	if e.nodes.len() != 1 {
		t.Fatalf("emitter buffered %d nodes, want 1", e.nodes.len())
	}

	got := e.nodes.s[0]
	want := KV{P: pkAS, PC: pcLightGreen}

	if !reflect.DeepEqual(*got.KV, want) {
		t.Errorf("kv:\n  got:  %#v\n  want: %#v", got.KV, want)
	}
}

func TestEmitAS_AsmlibYasm_OutputPath_NoUnderscoreInfix(t *testing.T) {
	e := newStreamingEmitter(nil)
	_, outPath := nodeTestEmitContext(e, hostInstance("contrib/libs/asmlib")).emitASYasm("memset64.asm", source("contrib/libs/asmlib/memset64.asm"), newModuleCCInputs(&ModuleCompileEnv{}), testYasmLDRef(e))
	want := "$(B)/contrib/libs/asmlib/memset64.pic.o"

	if outPath.string() != want {
		t.Errorf("outPath = %q, want %q", outPath, want)
	}
}

func TestEmitAS_AsmlibYasm_TargetSide_NoPicSuffix(t *testing.T) {
	e := newStreamingEmitter(nil)
	targetX86 := newTestPlatform(OSLinux, ISAX8664, "no")
	instance := ModuleInstance{
		Path:     source("contrib/libs/asmlib"),
		Kind:     KindLib,
		Language: LangCPP,
		Platform: targetX86,
	}
	_, outPath := nodeTestEmitContext(e, instance).emitASYasm("memset64.asm", source("contrib/libs/asmlib/memset64.asm"), newModuleCCInputs(&ModuleCompileEnv{}), testYasmLDRef(e))
	wantYasmPath := "$(B)/contrib/libs/asmlib/memset64.o"

	if outPath.string() != wantYasmPath {
		t.Errorf("outPath = %q, want %q", outPath, wantYasmPath)
	}

	if e.nodes.len() != 2 {
		t.Fatalf("emitter buffered %d nodes, want 2", e.nodes.len())
	}

	got := e.nodes.s[1]

	if got.Cmds[0].Cwd != 0 {
		t.Errorf("Cmds[0].Cwd = %q, want empty (yasm path)", got.Cmds[0].Cwd.string())
	}
}

func TestGen_PR35y_R8_AsmSrcdirRebase(t *testing.T) {
	fs := newMemFS(map[string]string{
		"mod/inner/ya.make": `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PLATFORM()
SRCDIR(mod)
SRCS(sub/foo.S)
END()
`,
		"mod/sub/foo.S": "// asm\n",
	})

	g := testGen(fs, "mod/inner")

	var asNode *Node

	for _, n := range g.Graph {
		if n == nil {
			continue
		}

		if n.KV.P == pkAS {
			asNode = n

			break
		}
	}

	if asNode == nil {
		t.Fatal("no AS node emitted for mod/inner")
	}

	const want = "$(S)/mod/sub/foo.S"
	const forbidden = "$(S)/mod/inner/sub/foo.S"

	if nodeHasInput(asNode, forbidden) {
		t.Errorf("AS.flatInputs() contains %q — SRCDIR rebase must redirect to %q", forbidden, want)
	}

	if !nodeHasInput(asNode, want) {
		t.Errorf("AS.flatInputs() missing %q — SRCDIR rebase for `.S` source: %#v", want, asNode.flatInputs())
	}
}

var (
	asTestKV = KV{P: pkLD, PC: pcLightCyan}
)
