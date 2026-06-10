package main

import (
	"reflect"
	"testing"
)

const referenceASOutput = "$(B)/contrib/libs/cxxsupp/builtins/_/aarch64/chkstk.S.o"

var builtinsASOwnAddIncl = []VFS{
	Intern("$(S)/contrib/libs/foolib/arch/aarch64"),
	Intern("$(S)/contrib/libs/foolib/arch/generic"),
	Intern("$(S)/contrib/libs/foolib/include"),
	Intern("$(S)/contrib/libs/foolib/extra"),
}

func TestEmitAS_NoStdInc_IncludeTailFollowsOwnAddIncl(t *testing.T) {
	e := NewBufferedEmitter()
	inst := hostInstance("contrib/libs/foolib")
	in := ModuleCCInputs{
		InclArgs: newInclArgMemo(),
		AddIncl: []VFS{
			Intern("$(S)/custom/foolib/arch/x86_64"),
			Intern("$(S)/custom/foolib/include"),
		},
	}
	EmitAS(inst, "src/math/x86_64/ceill.s", Intern("$(S)/contrib/libs/foolib/src/math/x86_64/ceill.s"), in, testHostP, e)

	args := e.nodes[0].Cmds[0].CmdArgs
	wantTail := []string{
		"-I$(B)",
		"-I$(S)",
		"-I$(S)/custom/foolib/arch/x86_64",
		"-I$(S)/custom/foolib/include",
	}
	start := len(args) - len(wantTail)

	for i, want := range wantTail {
		if args[start+i].String() != want {
			t.Fatalf("cmd_args[%d] = %q, want %q; args=%v", start+i, args[start+i].String(), want, args)
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
	e := NewBufferedEmitter()
	_, outPath := EmitAS(targetInstance("some/module"), "flat.S", Intern("$(S)/some/module/flat.S"), ModuleCCInputs{}, testHostP, e)
	want := "$(B)/some/module/flat.S.o"

	if outPath.String() != want {
		t.Errorf("outPath = %q, want %q", outPath, want)
	}
}

func TestEmitAS_OutputPath_NestedSrc(t *testing.T) {
	e := NewBufferedEmitter()
	_, outPath := EmitAS(targetInstance("contrib/libs/cxxsupp/builtins"), "aarch64/chkstk.S", Intern("$(S)/contrib/libs/cxxsupp/builtins/aarch64/chkstk.S"), ModuleCCInputs{}, testHostP, e)
	want := "$(B)/contrib/libs/cxxsupp/builtins/_/aarch64/chkstk.S.o"

	if outPath.String() != want {
		t.Errorf("outPath = %q, want %q", outPath, want)
	}
}

func TestEmitAS_OutputPath_SrcDir(t *testing.T) {
	e := NewBufferedEmitter()

	in := ModuleCCInputs{SrcDirs: []VFS{dirKey("contrib/libs/tcmalloc")}, FS: newMemFS(nil)}
	_, outPath := EmitAS(
		targetInstance("contrib/libs/tcmalloc/no_percpu_cache"),
		"tcmalloc/internal/percpu_rseq_asm.S",
		Intern("$(S)/contrib/libs/tcmalloc/tcmalloc/internal/percpu_rseq_asm.S"),
		in,
		testHostP,
		e,
	)
	want := "$(B)/contrib/libs/tcmalloc/no_percpu_cache/__/tcmalloc/internal/percpu_rseq_asm.S.o"

	if outPath.String() != want {
		t.Errorf("outPath = %q, want %q", outPath, want)
	}
}

func testYasmLDRef(e *BufferedEmitter) NodeRef {
	return e.Emit(&Node{
		Cmds:             []Cmd{{CmdArgs: appendInternStrs(nil, []string{"yasm"}), Env: nil}},
		Env:              nil,
		Inputs:           inputChunks{ToVFSSlice([]string{})},
		Outputs:          ToVFSSlice([]string{"$(B)/tools/yasm/yasm"}),
		KV:               KV{P: pkLD, PC: pcLightCyan},
		Tags:             []STR{internStr("tool")},
		Platform:         &Platform{Target: PlatformDefaultLinuxX8664},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		TargetProperties: TargetProperties{ModuleDir: "tools/yasm"},
	})
}

func TestEmitASYasm_YasmLD_PopulatesDepRefs(t *testing.T) {
	e := NewBufferedEmitter()
	yasmLDRef := testYasmLDRef(e)

	yasmTestIn := ModuleCCInputs{InclArgs: newInclArgMemo(), AddIncl: builtinsASOwnAddIncl}
	ref, _ := emitASYasm(hostInstance("contrib/libs/asmlib"), "memset64.asm", Intern("$(S)/contrib/libs/asmlib/memset64.asm"), yasmTestIn, yasmLDRef, e)

	if len(e.nodes) != 2 {
		t.Fatalf("emitter buffered %d nodes, want 2", len(e.nodes))
	}

	_ = ref
	got := e.nodes[1]

	if len(got.DepRefs) != 1 || got.DepRefs[0] != yasmLDRef {
		t.Errorf("DepRefs = %v, want [%v]", got.DepRefs, yasmLDRef)
	}

	toolRefs := got.ForeignDepRefs

	if len(toolRefs) != 1 || toolRefs[0] != yasmLDRef {
		t.Errorf(`ForeignDepRefs = %v, want [%v]`, toolRefs, yasmLDRef)
	}
}

func TestEmitAS_KV(t *testing.T) {
	e := NewBufferedEmitter()
	EmitAS(targetInstance("some/module"), "aarch64/foo.S", Intern("$(S)/some/module/aarch64/foo.S"), ModuleCCInputs{}, testHostP, e)

	if len(e.nodes) != 1 {
		t.Fatalf("emitter buffered %d nodes, want 1", len(e.nodes))
	}

	got := e.nodes[0]
	want := KV{P: pkAS, PC: pcLightGreen}

	if !reflect.DeepEqual(got.KV, want) {
		t.Errorf("kv:\n  got:  %#v\n  want: %#v", got.KV, want)
	}
}

func TestEmitAS_AsmlibYasm_OutputPath_NoUnderscoreInfix(t *testing.T) {
	e := NewBufferedEmitter()
	_, outPath := emitASYasm(hostInstance("contrib/libs/asmlib"), "memset64.asm", Intern("$(S)/contrib/libs/asmlib/memset64.asm"), ModuleCCInputs{}, testYasmLDRef(e), e)
	want := "$(B)/contrib/libs/asmlib/memset64.pic.o"

	if outPath.String() != want {
		t.Errorf("outPath = %q, want %q", outPath, want)
	}
}

func TestEmitAS_AsmlibYasm_TargetSide_NoPicSuffix(t *testing.T) {
	e := NewBufferedEmitter()
	targetX86 := newTestPlatform(OSLinux, ISAX8664, "no", nil)
	instance := ModuleInstance{
		Path:     Source("contrib/libs/asmlib"),
		Kind:     KindLib,
		Language: LangCPP,
		Platform: targetX86,
	}
	_, outPath := emitASYasm(instance, "memset64.asm", Intern("$(S)/contrib/libs/asmlib/memset64.asm"), ModuleCCInputs{}, testYasmLDRef(e), e)
	wantYasmPath := "$(B)/contrib/libs/asmlib/memset64.o"

	if outPath.String() != wantYasmPath {
		t.Errorf("outPath = %q, want %q", outPath, wantYasmPath)
	}

	if len(e.nodes) != 2 {
		t.Fatalf("emitter buffered %d nodes, want 2", len(e.nodes))
	}

	got := e.nodes[1]

	if got.Cmds[0].Cwd != 0 {
		t.Errorf("Cmds[0].Cwd = %q, want empty (yasm path)", got.Cmds[0].Cwd.String())
	}
}
