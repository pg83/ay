package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestGen_AcceptsProgramModule_Synthetic(t *testing.T) {
	root := t.TempDir()

	mainProgDir := filepath.Join(root, "mainprog")
	libDir := filepath.Join(root, "thelib")

	if err := os.MkdirAll(mainProgDir, 0o755); err != nil {
		t.Fatalf("mkdir mainprog: %v", err)
	}

	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatalf("mkdir thelib: %v", err)
	}

	mainYaMake := []byte("PROGRAM()\nPEERDIR(thelib)\nSRCS(main.cpp)\nEND()\n")
	libYaMake := []byte("LIBRARY()\nSRCS(lib.cpp)\nEND()\n")

	if err := os.WriteFile(filepath.Join(mainProgDir, "ya.make"), mainYaMake, 0o644); err != nil {
		t.Fatalf("write mainprog/ya.make: %v", err)
	}

	if err := os.WriteFile(filepath.Join(libDir, "ya.make"), libYaMake, 0o644); err != nil {
		t.Fatalf("write thelib/ya.make: %v", err)
	}

	g := testGen(root, "mainprog")

	if len(g.Graph) != 4 {
		t.Fatalf("Gen produced %d nodes, want 4 (2 CC + 1 AR + 1 LD)", len(g.Graph))
	}

	if len(g.Result) != 1 {
		t.Fatalf("Gen produced %d results, want 1", len(g.Result))
	}

	nodesByOutput := make(map[string]*Node, len(g.Graph))

	for _, n := range g.Graph {
		if len(n.Outputs) == 0 {
			t.Fatalf("node uid=%s has no outputs", n.UID)
		}

		nodesByOutput[n.Outputs[0].String()] = n
	}

	const (
		libCCOut    = "$(B)/thelib/lib.cpp.o"
		libARout    = "$(B)/thelib/libthelib.a"
		mainCCOut   = "$(B)/mainprog/main.cpp.o"
		mainBinPath = "$(B)/mainprog/mainprog"
	)

	for _, key := range []string{libCCOut, libARout, mainCCOut, mainBinPath} {
		if _, ok := nodesByOutput[key]; !ok {
			t.Fatalf("graph is missing expected output %q", key)
		}
	}

	rootLD := nodesByOutput[mainBinPath]

	if rootLD.KV["p"] != "LD" {
		t.Errorf("root node kv.p = %q, want LD", rootLD.KV["p"])
	}

	if len(rootLD.Cmds) != 4 {
		t.Errorf("root LD Cmds = %d, want 4", len(rootLD.Cmds))
	}

	if g.Result[0] != rootLD.UID {
		t.Errorf("result UID = %q, want mainprog LD uid %q", g.Result[0], rootLD.UID)
	}

	if rootLD.TargetProperties["module_dir"] != "mainprog" {
		t.Errorf("root LD module_dir = %q, want %q", rootLD.TargetProperties["module_dir"], "mainprog")
	}

	if rootLD.TargetProperties["module_type"] != "bin" {
		t.Errorf("root LD module_type = %q, want bin", rootLD.TargetProperties["module_type"])
	}

	mainCC := nodesByOutput[mainCCOut]
	libAR := nodesByOutput[libARout]

	depSet := make(map[string]struct{}, len(rootLD.Deps))
	for _, d := range rootLD.Deps {
		depSet[d] = struct{}{}
	}

	if _, ok := depSet[mainCC.UID]; !ok {
		t.Errorf("root LD deps %v missing main.cpp.o uid %q", rootLD.Deps, mainCC.UID)
	}

	if _, ok := depSet[libAR.UID]; !ok {
		t.Errorf("root LD deps %v missing thelib AR uid %q", rootLD.Deps, libAR.UID)
	}
}

func TestGen_UnittestFor_Synthetic(t *testing.T) {
	root := t.TempDir()

	mk := func(dir, body string) {
		d := filepath.Join(root, dir)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}

		if err := os.WriteFile(filepath.Join(d, "ya.make"), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s/ya.make: %v", dir, err)
		}
	}

	mkfile := func(rel, body string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(rel), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	mk("mod", "UNITTEST_FOR(thelib)\nSRCS(a_ut.cpp)\nEND()\n")
	mk("thelib", "LIBRARY()\nSRCS(lib.cpp)\nEND()\n")
	mk("build/cow/on", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(cow.cpp)\nEND()\n")
	mk("library/cpp/malloc/api", "LIBRARY()\nSRCS(api.cpp)\nEND()\n")
	mk("library/cpp/malloc/tcmalloc", "LIBRARY()\nPEERDIR(library/cpp/malloc/api contrib/restricted/abseil-cpp contrib/libs/tcmalloc/malloc_extension contrib/libs/tcmalloc/no_percpu_cache)\nSRCS(tcmalloc.cpp)\nEND()\n")
	mk("contrib/restricted/abseil-cpp", "LIBRARY()\nSRCS(absl.cpp)\nEND()\n")
	mk("contrib/libs/tcmalloc/malloc_extension", "LIBRARY()\nSRCS(ext.cpp)\nEND()\n")
	mk("contrib/libs/tcmalloc/no_percpu_cache", "LIBRARY()\nSRCS(npc.cpp)\nEND()\n")
	mk("library/cpp/testing/unittest_main", "LIBRARY()\nSRCS(main.cpp)\nEND()\n")
	mkfile("thelib/a_ut.cpp", "int a_ut() { return 0; }\n")
	mkfile("thelib/lib.cpp", "int thelib() { return 0; }\n")
	mkfile("build/cow/on/cow.cpp", "int cow() { return 0; }\n")
	mkfile("library/cpp/malloc/api/api.cpp", "int malloc_api() { return 0; }\n")
	mkfile("library/cpp/malloc/tcmalloc/tcmalloc.cpp", "int tcmalloc_lib() { return 0; }\n")
	mkfile("contrib/restricted/abseil-cpp/absl.cpp", "int absl() { return 0; }\n")
	mkfile("contrib/libs/tcmalloc/malloc_extension/ext.cpp", "int malloc_ext() { return 0; }\n")
	mkfile("contrib/libs/tcmalloc/no_percpu_cache/npc.cpp", "int no_percpu_cache() { return 0; }\n")
	mkfile("library/cpp/testing/unittest_main/main.cpp", "int unittest_main() { return 0; }\n")

	g := testGen(root, "mod")

	byOut := make(map[string]*Node, len(g.Graph))
	for _, n := range g.Graph {
		if len(n.Outputs) > 0 {
			byOut[n.Outputs[0].String()] = n
		}
	}

	ld := byOut["$(B)/mod/mod"]
	if ld == nil {
		t.Fatalf("missing UNITTEST_FOR LD output $(B)/mod/mod")
	}

	if ld.KV["p"] != "LD" {
		t.Errorf("root node kv.p = %q, want LD", ld.KV["p"])
	}

	if ld.TargetProperties["module_type"] != "bin" {
		t.Errorf("module_type = %q, want bin", ld.TargetProperties["module_type"])
	}

	deps := make(map[string]struct{}, len(ld.Deps))
	for _, d := range ld.Deps {
		deps[d] = struct{}{}
	}

	libAR := byOut["$(B)/thelib/libthelib.a"]
	if libAR == nil {
		t.Fatal("missing tested-lib AR $(B)/thelib/libthelib.a")
	}

	if _, ok := deps[libAR.UID]; !ok {
		t.Errorf("LD deps missing tested-lib AR uid %q (implicit PEERDIR($arg) not walked)", libAR.UID)
	}

	umAR := byOut["$(B)/library/cpp/testing/unittest_main/libcpp-testing-unittest_main.a"]
	if umAR == nil {
		t.Fatal("missing unittest_main AR")
	}

	if _, ok := deps[umAR.UID]; !ok {
		t.Errorf("LD deps missing unittest_main AR uid %q (implicit PEERDIR not walked)", umAR.UID)
	}

	cc := byOut["$(B)/mod/__/thelib/a_ut.cpp.o"]
	if cc == nil {
		t.Fatal("missing own CC $(B)/mod/__/thelib/a_ut.cpp.o")
	}

	if cc.TargetProperties["module_dir"] != "mod" {
		t.Fatalf("cc module_dir = %q, want mod", cc.TargetProperties["module_dir"])
	}

	inputs := make([]string, 0, len(cc.Inputs))
	for _, in := range cc.Inputs {
		inputs = append(inputs, in.String())
	}
	if !slicesContains(inputs, "$(S)/thelib/a_ut.cpp") {
		t.Fatalf("cc inputs missing $(S)/thelib/a_ut.cpp: %v", inputs)
	}

	for _, c := range cc.Cmds {
		for _, a := range c.CmdArgs {
			if a == "-I$(S)/thelib" {
				t.Fatalf("own CC unexpectedly carries direct -I$(S)/thelib: cmds=%+v", cc.Cmds)
			}
		}
	}

	linkArgs := ld.Cmds[2].CmdArgs
	thelibIdx := indexOfArg(linkArgs, "thelib/libthelib.a")
	cowIdx := indexOfArg(linkArgs, "build/cow/on/libbuild-cow-on.a")
	if thelibIdx < 0 || cowIdx < 0 {
		t.Fatalf("LD link args missing expected tested-dir/program-default archives: %v", linkArgs)
	}
	if thelibIdx > cowIdx {
		t.Fatalf("tested-dir archive lands after build/cow/on in LD cmd: thelib=%d cow=%d args=%v", thelibIdx, cowIdx, linkArgs)
	}
}

func TestGen_SyntheticPROGRAM_EmitsLD(t *testing.T) {
	root := t.TempDir()

	progDir := filepath.Join(root, "lone")
	if err := os.MkdirAll(progDir, 0o755); err != nil {
		t.Fatalf("mkdir lone: %v", err)
	}

	yamake := []byte("PROGRAM()\nSRCS(main.cpp)\nEND()\n")
	if err := os.WriteFile(filepath.Join(progDir, "ya.make"), yamake, 0o644); err != nil {
		t.Fatalf("write lone/ya.make: %v", err)
	}

	g := testGen(root, "lone")

	if len(g.Graph) != 2 {
		t.Fatalf("Gen produced %d nodes, want 2 (1 CC + 1 LD)", len(g.Graph))
	}

	if len(g.Result) != 1 {
		t.Fatalf("Gen produced %d results, want 1", len(g.Result))
	}

	var ld, cc *Node

	for _, n := range g.Graph {
		switch n.KV["p"] {
		case "LD":
			ld = n
		case "CC":
			cc = n
		}
	}

	if ld == nil {
		t.Fatal("Gen produced no LD node for PROGRAM module")
	}

	if cc == nil {
		t.Fatal("Gen produced no CC node for PROGRAM module")
	}

	if len(ld.Cmds) != 4 {
		t.Errorf("LD Cmds = %d, want 4", len(ld.Cmds))
	}

	wantOut := "$(B)/lone/lone"
	if len(ld.Outputs) != 1 || ld.Outputs[0].String() != wantOut {
		t.Errorf("LD outputs = %#v, want [%q]", ld.Outputs, wantOut)
	}

	if g.Result[0] != ld.UID {
		t.Errorf("result UID = %q, want LD uid %q", g.Result[0], ld.UID)
	}
}

func TestGen_RejectsUnsupportedMacro(t *testing.T) {
	root := t.TempDir()

	modDir := filepath.Join(root, "mod")

	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir mod: %v", err)
	}

	yamake := []byte("LIBRARY()\nTOTALLY_UNKNOWN(foo bar)\nSRCS(main.cpp)\nEND()\n")

	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644); err != nil {
		t.Fatalf("write mod/ya.make: %v", err)
	}

	exc := Try(func() {
		testGen(root, "mod")
	})

	if exc == nil {
		t.Fatal("expected exception for unsupported macro, got nil")
	}

	if !strings.Contains(exc.Error(), "does not yet support macro") {
		t.Errorf("error %q does not contain 'does not yet support macro'", exc.Error())
	}
}

func TestGen_RejectsMultipleModules(t *testing.T) {
	tmp := t.TempDir()
	Throw(os.MkdirAll(filepath.Join(tmp, "bad"), 0755))
	Throw(os.WriteFile(filepath.Join(tmp, "bad", "ya.make"), []byte(`LIBRARY()
SRCS(a.c)
PROGRAM()
SRCS(b.c)
END()
`), 0644))

	exc := Try(func() {
		testGen(tmp, "bad")
	})

	if exc == nil {
		t.Fatal("expected exception")
	}

	if !strings.Contains(exc.Error(), "multiple modules") {
		t.Errorf("unexpected error: %v", exc)
	}
}

func TestGen_RejectsZeroModule(t *testing.T) {
	tmp := t.TempDir()
	Throw(os.MkdirAll(filepath.Join(tmp, "noop"), 0755))
	Throw(os.WriteFile(filepath.Join(tmp, "noop", "ya.make"), []byte(`SET(X y)
END()
`), 0644))

	exc := Try(func() {
		testGen(tmp, "noop")
	})

	if exc == nil {
		t.Fatal("expected exception")
	}

	if !strings.Contains(exc.Error(), "no module declaration") {
		t.Errorf("unexpected error: %v", exc)
	}
}

func TestGen_RejectsProgramAsPeer(t *testing.T) {
	tmp := t.TempDir()
	Throw(os.MkdirAll(filepath.Join(tmp, "peerprog"), 0755))
	Throw(os.MkdirAll(filepath.Join(tmp, "caller"), 0755))
	Throw(os.WriteFile(filepath.Join(tmp, "peerprog", "ya.make"), []byte(`PROGRAM()
SRCS(peer_main.cpp)
END()
`), 0644))
	Throw(os.WriteFile(filepath.Join(tmp, "caller", "ya.make"), []byte(`PROGRAM()
PEERDIR(peerprog)
SRCS(caller_main.cpp)
END()
`), 0644))

	exc := Try(func() {
		testGen(tmp, "caller")
	})

	if exc == nil {
		t.Fatal("expected exception")
	}

	if !strings.Contains(exc.Error(), "peers PROGRAM module") {
		t.Errorf("unexpected error: %v", exc)
	}
}

func TestGen_PeerdirDeclarationOrder_Preserved(t *testing.T) {
	tmp := t.TempDir()

	Throw(os.MkdirAll(filepath.Join(tmp, "mainprog"), 0755))
	Throw(os.MkdirAll(filepath.Join(tmp, "zlib"), 0755))
	Throw(os.MkdirAll(filepath.Join(tmp, "alib"), 0755))
	Throw(os.WriteFile(filepath.Join(tmp, "mainprog", "ya.make"), []byte(`PROGRAM()
PEERDIR(zlib alib)
SRCS(main.cpp)
END()
`), 0644))
	Throw(os.WriteFile(filepath.Join(tmp, "zlib", "ya.make"), []byte(`LIBRARY()
SRCS(zlib.c)
END()
`), 0644))
	Throw(os.WriteFile(filepath.Join(tmp, "alib", "ya.make"), []byte(`LIBRARY()
SRCS(alib.c)
END()
`), 0644))

	g := testGen(tmp, "mainprog")

	var zlibIdx, alibIdx int = -1, -1

	for i, n := range g.Graph {
		if len(n.Outputs) > 0 {
			if strings.Contains(n.Outputs[0].String(), "/zlib/") && n.KV["p"] == "AR" {
				zlibIdx = i
			}
			if strings.Contains(n.Outputs[0].String(), "/alib/") && n.KV["p"] == "AR" {
				alibIdx = i
			}
		}
	}

	if zlibIdx == -1 || alibIdx == -1 {
		t.Fatalf("expected both zlib and alib AR nodes; zlibIdx=%d alibIdx=%d", zlibIdx, alibIdx)
	}

	if len(g.Graph) != 6 {
		t.Errorf("expected 6 nodes (3 CC + 2 AR + 1 LD), got %d", len(g.Graph))
	}
}

func TestGen_MacroEvaluation_IfStmt_TakeThen(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "ifmod")
	Throw(os.MkdirAll(modDir, 0o755))

	yamake := []byte(`LIBRARY()
IF (OS_LINUX)
    SRCS(linux.c)
ELSE()
    SRCS(other.c)
ENDIF()
END()
`)
	Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644))

	g := testGen(root, "ifmod")

	if len(g.Graph) != 2 {
		t.Fatalf("expected 2 nodes (1 CC + 1 AR), got %d", len(g.Graph))
	}

	var ccInputs []string

	for _, n := range g.Graph {
		if n.KV["p"] == "CC" {
			ccInputs = append(ccInputs, vfsStrings(n.Inputs)...)
		}
	}

	if len(ccInputs) != 1 {
		t.Fatalf("expected 1 CC input, got %d (%v)", len(ccInputs), ccInputs)
	}

	if !strings.Contains(ccInputs[0], "linux.c") {
		t.Errorf("CC input %q does not reference linux.c (THEN branch)", ccInputs[0])
	}

	if strings.Contains(ccInputs[0], "other.c") {
		t.Errorf("CC input %q unexpectedly references other.c (ELSE branch should be unreached)", ccInputs[0])
	}
}

func TestGen_MacroEvaluation_NoLibcFlag(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "nolibcmod")
	Throw(os.MkdirAll(modDir, 0o755))

	yamake := []byte(`LIBRARY()
NO_LIBC()
NO_UTIL()
NO_RUNTIME()
SRCS(lib.c)
END()
`)
	Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644))

	mf := Throw2(ParseFile(NewFS(root), filepath.Join(modDir, "ya.make")))

	d := collectModule(newIncludeParserManager(root), "nolibcmod", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Kind: KindLib, Platform: testTargetP}))

	if !d.flags.NoLibc {
		t.Errorf("flags.NoLibc = false, want true (macro overlay should have flipped it)")
	}

	if !d.flags.NoUtil {
		t.Errorf("flags.NoUtil = false, want true")
	}

	if !d.flags.NoRuntime {
		t.Errorf("flags.NoRuntime = false, want true")
	}

	g := testGen(root, "nolibcmod")

	if len(g.Graph) != 2 {
		t.Errorf("Gen produced %d nodes, want 2 (1 CC + 1 AR)", len(g.Graph))
	}
}

func TestGen_NoStdIncGlobalCFlagsPropagateToExplicitPeer(t *testing.T) {
	root := t.TempDir()

	fooDir := filepath.Join(root, "contrib/libs/foolib")
	bridgeDir := filepath.Join(root, "bridge")
	Throw(os.MkdirAll(fooDir, 0o755))
	Throw(os.MkdirAll(bridgeDir, 0o755))

	Throw(os.WriteFile(filepath.Join(fooDir, "ya.make"), []byte(`LIBRARY()
NO_PLATFORM()
CFLAGS(
    GLOBAL -D_foolib_=1
    -nostdinc
)
SRCS(m.c)
END()
`), 0o644))
	Throw(os.WriteFile(filepath.Join(fooDir, "m.c"), []byte("int foolib_symbol(void) { return 1; }\n"), 0o644))

	Throw(os.WriteFile(filepath.Join(bridgeDir, "ya.make"), []byte(`LIBRARY()
NO_RUNTIME()
PEERDIR(contrib/libs/foolib)
SRCS(x.cpp)
END()
`), 0o644))
	Throw(os.WriteFile(filepath.Join(bridgeDir, "x.cpp"), []byte("int bridge_symbol(void) { return 2; }\n"), 0o644))

	g := testGen(root, "bridge")
	var args []string

	for _, n := range g.Graph {
		if len(n.Outputs) == 1 && n.Outputs[0].String() == "$(B)/bridge/x.cpp.o" {
			args = n.Cmds[0].CmdArgs
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

func TestGen_JoinSrcs_EmitsJSAndCC(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "joinmod")
	Throw(os.MkdirAll(modDir, 0o755))

	yamake := []byte(`LIBRARY()
JOIN_SRCS(all_my.cpp src1.cpp src2.cpp)
SRCS(other.cpp)
END()
`)
	Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644))

	g := testGen(root, "joinmod")

	counts := make(map[string]int)
	for _, n := range g.Graph {
		p, _ := n.KV["p"].(string)
		counts[p]++
	}

	if counts["JS"] != 1 {
		t.Errorf("JS count = %d, want 1", counts["JS"])
	}

	if counts["CC"] != 2 {
		t.Errorf("CC count = %d, want 2 (1 for joined + 1 for other.cpp)", counts["CC"])
	}

	if counts["AR"] != 1 {
		t.Errorf("AR count = %d, want 1", counts["AR"])
	}

	if len(g.Graph) != 4 {
		t.Fatalf("total graph nodes = %d, want 4", len(g.Graph))
	}

	var (
		joinedInput string
		otherInput  string
	)

	for _, n := range g.Graph {
		if n.KV["p"] != "CC" {
			continue
		}

		if len(n.Inputs) == 0 {
			continue
		}

		switch {
		case strings.Contains(n.Inputs[0].String(), "all_my.cpp"):
			joinedInput = n.Inputs[0].String()
		case strings.Contains(n.Inputs[0].String(), "other.cpp"):
			otherInput = n.Inputs[0].String()
		}
	}

	if joinedInput == "" {
		t.Errorf("no CC node found whose input references all_my.cpp")
	}

	if otherInput == "" {
		t.Errorf("no CC node found whose input references other.cpp")
	}

	var jsOut string

	for _, n := range g.Graph {
		if n.KV["p"] == "JS" && len(n.Outputs) > 0 {
			jsOut = n.Outputs[0].String()
		}
	}

	wantJSOut := "$(B)/joinmod/all_my.cpp"
	if jsOut != wantJSOut {
		t.Errorf("JS output = %q, want %q", jsOut, wantJSOut)
	}
}

func TestGen_GlobalSrcs_EmitsTwoARs(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "globalmod")
	Throw(os.MkdirAll(modDir, 0o755))

	yamake := []byte(`LIBRARY()
GLOBAL_SRCS(global.cpp)
SRCS(regular.cpp)
END()
`)
	Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644))

	g := testGen(root, "globalmod")

	counts := make(map[string]int)
	for _, n := range g.Graph {
		p, _ := n.KV["p"].(string)
		counts[p]++
	}

	if counts["CC"] != 2 {
		t.Errorf("CC count = %d, want 2 (regular + global)", counts["CC"])
	}

	if counts["AR"] != 2 {
		t.Errorf("AR count = %d, want 2 (regular + global)", counts["AR"])
	}

	var globalARs, regularARs int

	for _, n := range g.Graph {
		if n.KV["p"] != "AR" {
			continue
		}

		if n.TargetProperties["module_tag"] == "global" {
			globalARs++
		} else if _, has := n.TargetProperties["module_tag"]; !has {
			regularARs++
		}
	}

	if globalARs != 1 {
		t.Errorf("global ARs = %d, want 1", globalARs)
	}

	if regularARs != 1 {
		t.Errorf("regular (no-tag) ARs = %d, want 1", regularARs)
	}
}

func TestGen_HostToolRecursion_R6(t *testing.T) {
	root := t.TempDir()

	ragelDir := filepath.Join(root, "contrib/tools/ragel6/bin")
	Throw(os.MkdirAll(ragelDir, 0o755))
	Throw(os.WriteFile(filepath.Join(ragelDir, "ya.make"), []byte("PROGRAM(ragel6)\nSRCS(main.cpp)\nEND()\n"), 0o644))

	consumerDir := filepath.Join(root, "consumer")
	Throw(os.MkdirAll(consumerDir, 0o755))
	Throw(os.WriteFile(filepath.Join(consumerDir, "ya.make"), []byte("LIBRARY()\nSRCS(parser.rl6)\nEND()\n"), 0o644))

	g := testGen(root, "consumer")

	counts := make(map[string]int)
	platforms := make(map[string]int)
	hostNodes := 0

	for _, n := range g.Graph {
		p, _ := n.KV["p"].(string)
		counts[p]++
		platforms[n.Platform]++

		if nodeHasHostTag(n.Tags) {
			hostNodes++
		}
	}

	if counts["R6"] != 1 {
		t.Errorf("R6 count = %d, want 1", counts["R6"])
	}

	if counts["LD"] != 1 {
		t.Errorf("LD count = %d, want 1 (host ragel6 LD)", counts["LD"])
	}

	if counts["AR"] != 1 {
		t.Errorf("AR count = %d, want 1 (target consumer AR)", counts["AR"])
	}

	if counts["CC"] != 2 {
		t.Errorf("CC count = %d, want 2 (host ragel6/main.cpp + target generated parser.rl6.cpp)", counts["CC"])
	}

	if hostNodes != 2 {
		t.Errorf("host nodes = %d, want 2 (host CC + host LD)", hostNodes)
	}

	if platforms[string(PlatformDefaultLinuxAArch64)] != 3 {
		t.Errorf("target nodes = %d, want 3", platforms[string(PlatformDefaultLinuxAArch64)])
	}

	if platforms[string(PlatformDefaultLinuxX8664)] != 2 {
		t.Errorf("host nodes (by platform) = %d, want 2", platforms[string(PlatformDefaultLinuxX8664)])
	}

	var (
		r6Node *Node
		ldNode *Node
	)

	for _, n := range g.Graph {
		switch n.KV["p"] {
		case "R6":
			r6Node = n
		case "LD":
			ldNode = n
		}
	}

	if r6Node == nil {
		t.Fatal("no R6 node found")
	}

	if ldNode == nil {
		t.Fatal("no host ragel6 LD node found")
	}

	if len(r6Node.Deps) != 1 || r6Node.Deps[0] != ldNode.UID {
		t.Errorf("R6 Deps = %v, want [%q]", r6Node.Deps, ldNode.UID)
	}

	if len(r6Node.ForeignDeps) != 1 || len(r6Node.ForeignDeps["tool"]) != 1 || r6Node.ForeignDeps["tool"][0] != ldNode.UID {
		t.Errorf("R6 ForeignDeps = %v, want {tool: [%q]}", r6Node.ForeignDeps, ldNode.UID)
	}

	if len(r6Node.Cmds) == 0 || len(r6Node.Cmds[0].CmdArgs) == 0 {
		t.Fatalf("R6 node has no Cmds[0].CmdArgs; got Cmds=%v", r6Node.Cmds)
	}

	if len(ldNode.Outputs) == 0 {
		t.Fatalf("host LD node has no Outputs; got Outputs=%v", ldNode.Outputs)
	}

	wantCmd0 := canonicalizeRagel6Binary(ldNode.Outputs[0]).String()

	if r6Node.Cmds[0].CmdArgs[0] != wantCmd0 {
		t.Errorf("R6 cmd_args[0] = %q, want canonicalised host ragel6 LD outputs[0] = %q (raw outputs[0] = %q)",
			r6Node.Cmds[0].CmdArgs[0], wantCmd0, ldNode.Outputs[0].String())
	}
}

func TestGen_PeerGlobalArchive_ThreadsToLD(t *testing.T) {
	root := t.TempDir()

	peerDir := filepath.Join(root, "peerlib")
	Throw(os.MkdirAll(peerDir, 0o755))
	Throw(os.WriteFile(filepath.Join(peerDir, "ya.make"), []byte(
		"LIBRARY()\nSRCS(regular.cpp)\nGLOBAL_SRCS(global.cpp)\nEND()\n",
	), 0o644))

	consumerDir := filepath.Join(root, "consumer")
	Throw(os.MkdirAll(consumerDir, 0o755))
	Throw(os.WriteFile(filepath.Join(consumerDir, "ya.make"), []byte(
		"PROGRAM()\nSRCS(main.cpp)\nPEERDIR(peerlib)\nEND()\n",
	), 0o644))

	g := testGen(root, "consumer")

	var ldNode *Node
	for _, n := range g.Graph {
		if n.KV["p"] == "LD" {
			ldNode = n
		}
	}

	if ldNode == nil {
		t.Fatal("no LD node found in graph")
	}

	arCount := 0
	for _, n := range g.Graph {
		if n.KV["p"] == "AR" {
			arCount++
		}
	}

	if arCount != 2 {
		t.Errorf("AR count = %d, want 2 (regular + global from peerlib)", arCount)
	}

	if len(ldNode.Deps) < 3 {
		t.Errorf("LD Deps = %d, want >= 3 (own CC + peer AR + peer global AR)", len(ldNode.Deps))
	}

	expectedInput := "$(B)/peerlib/libpeerlib.global.a"
	foundInInputs := false

	for _, in := range ldNode.Inputs {
		if in.String() == expectedInput {
			foundInInputs = true
			break
		}
	}

	if !foundInInputs {
		t.Errorf("expected single-prefixed global archive in inputs; got: %v", ldNode.Inputs)
	}

	for _, in := range ldNode.Inputs {
		if strings.Contains(in.String(), "$(B)/$(B)") {
			t.Errorf("double-prefixed input found: %q", in.String())
		}
	}

	if len(ldNode.Cmds) < 3 {
		t.Fatalf("LD node has %d cmds, want >= 3", len(ldNode.Cmds))
	}

	linkArgs := ldNode.Cmds[2].CmdArgs
	expectedCmdArg := "peerlib/libpeerlib.global.a"
	foundInCmdArgs := false

	for _, a := range linkArgs {
		if a == expectedCmdArg {
			foundInCmdArgs = true
			break
		}
	}

	if !foundInCmdArgs {
		t.Errorf("expected unprefixed global archive in cmd_args[2]; got: %v", linkArgs)
	}
}

func TestGen_AllocatorMacro_ResolvesToPeer(t *testing.T) {
	root := t.TempDir()

	progDir := filepath.Join(root, "prog")
	Throw(os.MkdirAll(progDir, 0o755))
	Throw(os.WriteFile(filepath.Join(progDir, "ya.make"),
		[]byte("PROGRAM()\nNO_PLATFORM()\nALLOCATOR(MIM)\nSRCS(main.cpp)\nEND()\n"), 0o644))

	mimDir := filepath.Join(root, "library/cpp/malloc/mimalloc")
	Throw(os.MkdirAll(mimDir, 0o755))
	Throw(os.WriteFile(filepath.Join(mimDir, "ya.make"),
		[]byte("LIBRARY()\nNO_PLATFORM()\nSRCS(mim.cpp)\nEND()\n"), 0o644))

	g := testGen(root, "prog")

	var sawMimDir bool

	for _, n := range g.Graph {
		if n.TargetProperties["module_dir"] == "library/cpp/malloc/mimalloc" {
			sawMimDir = true

			break
		}
	}

	if !sawMimDir {
		t.Errorf("expected ALLOCATOR(MIM) to add library/cpp/malloc/mimalloc as peer; got Graph with no such module_dir")
	}
}

func TestGen_DefaultPeerdirs_SimpleLibrary(t *testing.T) {
	root := t.TempDir()

	stubLib := "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(stub.cpp)\nEND()\n"

	for _, path := range []string{
		"contrib/libs/cxxsupp/builtins",
		"library/cpp/malloc/api",
		"contrib/libs/cxxsupp/libcxx",
		"contrib/libs/cxxsupp/libcxxrt",
		"contrib/libs/libunwind",
		"util",
	} {
		dir := filepath.Join(root, path)

		Throw(os.MkdirAll(dir, 0o755))
		Throw(os.WriteFile(filepath.Join(dir, "ya.make"), []byte(stubLib), 0o644))
	}

	plain := ModuleInstance{
		Path:     "consumer",
		Kind:     KindLib,
		Language: LangCPP,
		Platform: testTargetP,
	}

	wantDefaults := []string{
		"contrib/libs/cxxsupp/libcxx",
		"contrib/libs/cxxsupp/libcxxrt",
		"contrib/libs/libunwind",
		"util",
	}

	gotDefaults := defaultPeerdirsForWithState(nil, plain, FlagSet{}, false)

	if !stringSlicesEqual(gotDefaults, wantDefaults) {
		t.Errorf("defaultPeerdirsForWithState(plain CPP) = %v, want %v", gotDefaults, wantDefaults)
	}

	consumerDir := filepath.Join(root, "consumer")

	Throw(os.MkdirAll(consumerDir, 0o755))
	Throw(os.WriteFile(filepath.Join(consumerDir, "ya.make"), []byte("LIBRARY()\nSRCS(main.cpp)\nEND()\n"), 0o644))

	g := testGen(root, "consumer")

	emittedDirs := make(map[string]bool)

	for _, n := range g.Graph {
		if md, ok := n.TargetProperties["module_dir"]; ok {
			emittedDirs[md] = true
		}
	}

	for _, want := range []string{
		"consumer",
		"contrib/libs/cxxsupp/libcxx",
		"contrib/libs/cxxsupp/libcxxrt",
		"contrib/libs/libunwind",
		"util",
	} {
		if !emittedDirs[want] {
			t.Errorf("graph missing module_dir %q; got %v", want, emittedDirs)
		}
	}
}

func TestGen_DefaultPeerdirs_HelperSuppression(t *testing.T) {

	fullSet := []string{
		"contrib/libs/cxxsupp/libcxx",
		"contrib/libs/cxxsupp/libcxxrt",
		"contrib/libs/libunwind",
		"util",
	}

	cases := []struct {
		name  string
		mi    ModuleInstance
		flags FlagSet
		want  []string
	}{
		{
			name: "effective_no_platform",
			mi: ModuleInstance{
				Path:     "x",
				Kind:     KindLib,
				Language: LangCPP,
			},
			flags: FlagSet{NoLibc: true, NoRuntime: true, NoUtil: true},
			want:  nil,
		},
		{
			name: "explicit_no_platform",
			mi: ModuleInstance{
				Path:     "x",
				Kind:     KindLib,
				Language: LangCPP,
			},
			flags: FlagSet{NoPlatform: true},
			want:  nil,
		},
		{
			name: "no_libc_only",
			mi: ModuleInstance{
				Path:     "x",
				Kind:     KindLib,
				Language: LangCPP,
				Platform: testTargetP,
			},
			flags: FlagSet{NoLibc: true},

			want: fullSet,
		},
		{
			name: "no_runtime_only",
			mi: ModuleInstance{
				Path:     "x",
				Kind:     KindLib,
				Language: LangCPP,
				Platform: testTargetP,
			},
			flags: FlagSet{NoRuntime: true},

			want: []string{"util"},
		},
		{
			name: "non_cpp",
			mi: ModuleInstance{
				Path:     "x",
				Kind:     KindLib,
				Language: LangProto,
			},
			want: nil,
		},
		{
			name:  "no_util_only",
			mi:    ModuleInstance{Path: "x", Kind: KindLib, Language: LangCPP},
			flags: FlagSet{NoUtil: true},

			want: []string{
				"contrib/libs/cxxsupp/libcxx",
				"contrib/libs/cxxsupp/libcxxrt",
				"contrib/libs/libunwind",
			},
		},

		{
			name: "self_builtins_runtime_ancestor",
			mi: ModuleInstance{
				Path:     "contrib/libs/cxxsupp/builtins",
				Kind:     KindLib,
				Language: LangCPP,
			},
			want: nil,
		},
		{
			name: "self_malloc_api_runtime_ancestor",
			mi: ModuleInstance{
				Path:     "library/cpp/malloc/api",
				Kind:     KindLib,
				Language: LangCPP,
			},
			want: nil,
		},
		{
			name: "self_libcxx_runtime_ancestor",
			mi: ModuleInstance{
				Path:     "contrib/libs/cxxsupp/libcxx",
				Kind:     KindLib,
				Language: LangCPP,
			},
			want: nil,
		},
		{
			name: "self_util_runtime_ancestor",
			mi: ModuleInstance{
				Path:     "util",
				Kind:     KindLib,
				Language: LangCPP,
			},
			want: nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := defaultPeerdirsForWithState(nil, c.mi, c.flags, false)

			if !stringSlicesEqual(got, c.want) {
				t.Errorf("defaultPeerdirsForWithState(%+v, %+v) = %v, want %v", c.mi, c.flags, got, c.want)
			}
		})
	}
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

func TestGen_DefaultPeerdirs_ExplicitDuplicateDeduped(t *testing.T) {
	tmp := t.TempDir()
	Throw(os.MkdirAll(filepath.Join(tmp, "lib1"), 0755))
	Throw(os.WriteFile(filepath.Join(tmp, "lib1", "ya.make"), []byte(`LIBRARY()
PEERDIR(contrib/libs/foolib)
SRCS(a.cpp)
END()
`), 0644))

	Throw(os.MkdirAll(filepath.Join(tmp, "contrib/libs/foolib"), 0755))
	Throw(os.WriteFile(filepath.Join(tmp, "contrib/libs/foolib", "ya.make"), []byte(`LIBRARY()
NO_LIBC()
NO_UTIL()
NO_RUNTIME()
NO_PLATFORM()
SRCS(stub.c)
END()
`), 0644))

	g := testGen(tmp, "lib1")

	var lib1AR *Node
	for _, n := range g.Graph {
		if n.KV["p"] == "AR" && n.TargetProperties["module_dir"] == "lib1" {
			lib1AR = n
			break
		}
	}

	if lib1AR == nil {
		t.Fatal("lib1 AR not found")
	}

	for _, ref := range lib1AR.Deps {
		for _, n := range g.Graph {
			if n.UID == ref && n.KV["p"] == "AR" {
				t.Errorf("lib1 AR has AR-typed dep %q (module_dir=%q); reference invariant: zero AR-on-AR deps", ref, n.TargetProperties["module_dir"])
			}
		}
	}
}

func TestGen_SrcDirRebasesSourceResolution(t *testing.T) {
	t.Run("with_srcdir", func(t *testing.T) {

		root := t.TempDir()

		modDir := filepath.Join(root, "mymod")
		Throw(os.MkdirAll(modDir, 0o755))

		yamake := []byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCDIR(other/dir)\nSRCS(foo.cpp)\nEND()\n")
		Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644))

		g := testGen(root, "mymod")

		if len(g.Graph) != 2 {
			t.Fatalf("expected 2 nodes (1 CC + 1 AR), got %d", len(g.Graph))
		}

		var ccNode *Node

		for _, n := range g.Graph {
			if n.KV["p"] == "CC" {
				ccNode = n
			}
		}

		if ccNode == nil {
			t.Fatal("no CC node emitted")
		}

		if ccNode.TargetProperties["module_dir"] != "mymod" {
			t.Errorf("CC module_dir = %q, want %q", ccNode.TargetProperties["module_dir"], "mymod")
		}

		wantInput := "$(S)/other/dir/foo.cpp"

		if len(ccNode.Inputs) == 0 || ccNode.Inputs[0].String() != wantInput {
			t.Errorf("CC inputs = %v, want first = %q", ccNode.Inputs, wantInput)
		}

		wantOutput := "$(B)/mymod/__/other/dir/foo.cpp.o"

		if len(ccNode.Outputs) == 0 || ccNode.Outputs[0].String() != wantOutput {
			t.Errorf("CC outputs = %v, want first = %q", ccNode.Outputs, wantOutput)
		}
	})

	t.Run("without_srcdir_baseline", func(t *testing.T) {
		root := t.TempDir()

		modDir := filepath.Join(root, "basemod")
		Throw(os.MkdirAll(modDir, 0o755))

		yamake := []byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(bar.cpp)\nEND()\n")
		Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644))

		g := testGen(root, "basemod")

		if len(g.Graph) != 2 {
			t.Fatalf("expected 2 nodes (1 CC + 1 AR), got %d", len(g.Graph))
		}

		var ccNode *Node

		for _, n := range g.Graph {
			if n.KV["p"] == "CC" {
				ccNode = n
			}
		}

		if ccNode == nil {
			t.Fatal("no CC node emitted")
		}

		if ccNode.TargetProperties["module_dir"] != "basemod" {
			t.Errorf("CC module_dir = %q, want %q", ccNode.TargetProperties["module_dir"], "basemod")
		}

		wantInput := "$(S)/basemod/bar.cpp"

		if len(ccNode.Inputs) == 0 || ccNode.Inputs[0].String() != wantInput {
			t.Errorf("CC inputs = %v, want first = %q", ccNode.Inputs, wantInput)
		}
	})

	t.Run("join_srcs_with_srcdir_library_non_ancestor", func(t *testing.T) {

		root := t.TempDir()

		modDir := filepath.Join(root, "jsmod")
		Throw(os.MkdirAll(modDir, 0o755))

		yamake := []byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCDIR(other/dir)\nJOIN_SRCS(all.cpp s1.cpp s2.cpp)\nEND()\n")
		Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644))

		g := testGen(root, "jsmod")

		if len(g.Graph) != 3 {
			t.Fatalf("expected 3 nodes (1 JS + 1 CC + 1 AR), got %d", len(g.Graph))
		}

		var jsNode, ccNode *Node

		for _, n := range g.Graph {
			switch n.KV["p"] {
			case "JS":
				jsNode = n
			case "CC":
				ccNode = n
			}
		}

		if jsNode == nil {
			t.Fatal("no JS node emitted")
		}

		if ccNode == nil {
			t.Fatal("no CC node emitted")
		}

		if jsNode.TargetProperties["module_dir"] != "jsmod" {
			t.Errorf("JS module_dir = %q, want %q", jsNode.TargetProperties["module_dir"], "jsmod")
		}

		if ccNode.TargetProperties["module_dir"] != "jsmod" {
			t.Errorf("CC module_dir = %q, want %q", ccNode.TargetProperties["module_dir"], "jsmod")
		}
	})

	t.Run("ancestor_program_rebases_module_dir", func(t *testing.T) {

		root := t.TempDir()

		modDir := filepath.Join(root, "tools/r6/bin")
		Throw(os.MkdirAll(modDir, 0o755))

		yamake := []byte("PROGRAM(myprog)\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nALLOCATOR(FAKE)\nSRCDIR(tools/r6)\nSRCS(main.cpp)\nEND()\n")
		Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644))

		g := testGen(root, "tools/r6/bin")

		var ccNode *Node

		for _, n := range g.Graph {
			if n.KV["p"] == "CC" {
				ccNode = n
			}
		}

		if ccNode == nil {
			t.Fatal("no CC node emitted")
		}

		if ccNode.TargetProperties["module_dir"] != "tools/r6" {
			t.Errorf("CC module_dir = %q, want %q", ccNode.TargetProperties["module_dir"], "tools/r6")
		}

		wantInput := "$(S)/tools/r6/main.cpp"
		if len(ccNode.Inputs) == 0 || ccNode.Inputs[0].String() != wantInput {
			t.Errorf("CC inputs = %v, want first = %q", ccNode.Inputs, wantInput)
		}

		wantOutput := "$(B)/tools/r6/main.cpp.o"
		if len(ccNode.Outputs) == 0 || ccNode.Outputs[0].String() != wantOutput {
			t.Errorf("CC outputs = %v, want first = %q", ccNode.Outputs, wantOutput)
		}
	})

	t.Run("ancestor_program_nested_source_keeps_module_dir", func(t *testing.T) {
		root := t.TempDir()

		modDir := filepath.Join(root, "tools/r6/bin")
		Throw(os.MkdirAll(filepath.Join(root, "tools/r6/sub"), 0o755))
		Throw(os.MkdirAll(modDir, 0o755))

		yamake := []byte("PROGRAM(myprog)\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nALLOCATOR(FAKE)\nSRCDIR(tools/r6)\nSRCS(sub/main.cpp)\nEND()\n")
		Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644))
		Throw(os.WriteFile(filepath.Join(root, "tools/r6/sub/main.cpp"), []byte("int main() { return 0; }\n"), 0o644))

		g := testGen(root, "tools/r6/bin")

		var ccNode *Node
		for _, n := range g.Graph {
			if n.KV["p"] == "CC" {
				ccNode = n
				break
			}
		}
		if ccNode == nil {
			t.Fatal("no CC node emitted")
		}

		if ccNode.TargetProperties["module_dir"] != "tools/r6/bin" {
			t.Errorf("CC module_dir = %q, want %q", ccNode.TargetProperties["module_dir"], "tools/r6/bin")
		}

		if got := ccNode.Inputs[0].String(); got != "$(S)/tools/r6/sub/main.cpp" {
			t.Errorf("CC input = %q, want %q", got, "$(S)/tools/r6/sub/main.cpp")
		}

		if got := ccNode.Outputs[0].String(); got != "$(B)/tools/r6/bin/__/sub/main.cpp.o" {
			t.Errorf("CC output = %q, want %q", got, "$(B)/tools/r6/bin/__/sub/main.cpp.o")
		}
	})
}

func TestGen_CXXFLAGS_GLOBAL_LandsOnOwnCmdArgs(t *testing.T) {
	t.Run("CXXFLAGS_GLOBAL_emitted_twice_no_literal_GLOBAL", func(t *testing.T) {
		root := t.TempDir()
		modDir := filepath.Join(root, "testlib")
		Throw(os.MkdirAll(modDir, 0o755))

		yamake := []byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nCXXFLAGS(GLOBAL -nostdinc++)\nSRCS(foo.cpp)\nEND()\n")
		Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644))

		g := testGen(root, "testlib")

		var ccNode *Node

		for _, n := range g.Graph {
			if n.KV["p"] == "CC" {
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

		for _, arg := range ccNode.Cmds[0].CmdArgs {
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
		root := t.TempDir()
		modDir := filepath.Join(root, "testlib")
		Throw(os.MkdirAll(modDir, 0o755))

		yamake := []byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nCONLYFLAGS(GLOBAL -Dfoo)\nSRCS(bar.c)\nEND()\n")
		Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644))

		g := testGen(root, "testlib")

		var ccNode *Node

		for _, n := range g.Graph {
			if n.KV["p"] == "CC" {
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

		for _, arg := range ccNode.Cmds[0].CmdArgs {
			if arg == "GLOBAL" {
				t.Errorf("CC cmd_args contains literal %q — GLOBAL modifier leaked into own node", arg)
			}
		}
	})

	t.Run("CXXFLAGS_non_GLOBAL_still_applied", func(t *testing.T) {
		root := t.TempDir()
		modDir := filepath.Join(root, "testlib")
		Throw(os.MkdirAll(modDir, 0o755))

		yamake := []byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nCXXFLAGS(-DMINE)\nSRCS(foo.cpp)\nEND()\n")
		Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644))

		g := testGen(root, "testlib")

		var ccNode *Node

		for _, n := range g.Graph {
			if n.KV["p"] == "CC" {
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

		for _, arg := range ccNode.Cmds[0].CmdArgs {
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

func TestGen_GeneratorWiredIntoDepRefs_JS(t *testing.T) {
	root := t.TempDir()

	modDir := filepath.Join(root, "jsmod")
	Throw(os.MkdirAll(modDir, 0o755))
	yamake := []byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nJOIN_SRCS(all.cpp s1.cpp s2.cpp)\nEND()\n")
	Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644))

	g := testGen(root, "jsmod")

	var jsNode, ccNode *Node

	for _, n := range g.Graph {
		switch n.KV["p"] {
		case "JS":
			jsNode = n
		case "CC":
			ccNode = n
		}
	}

	if jsNode == nil {
		t.Fatal("no JS node emitted")
	}

	if ccNode == nil {
		t.Fatal("no CC node emitted")
	}

	found := false

	for _, dep := range ccNode.Deps {
		if dep == jsNode.UID {
			found = true

			break
		}
	}

	if !found {
		t.Errorf("CC.Deps = %v, want to contain JS UID %q (PR-30 D04 Generator wiring)", ccNode.Deps, jsNode.UID)
	}
}

func TestGen_GeneratorWiredIntoDepRefs_R6(t *testing.T) {
	root := t.TempDir()

	modDir := filepath.Join(root, "r6mod")
	Throw(os.MkdirAll(modDir, 0o755))
	Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), []byte(`LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(thing.rl6)
END()
`), 0o644))

	Throw(os.MkdirAll(filepath.Join(root, "contrib/tools/ragel6/bin"), 0o755))
	Throw(os.WriteFile(filepath.Join(root, "contrib/tools/ragel6/bin", "ya.make"), []byte(`PROGRAM(ragel6)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
ALLOCATOR(FAKE)
SRCS(main.cpp)
END()
`), 0o644))

	g := testGen(root, "r6mod")

	var r6Node, ccNode *Node

	for _, n := range g.Graph {
		switch n.KV["p"] {
		case "R6":
			r6Node = n
		case "CC":

			ip := ""
			if len(n.Inputs) > 0 {
				ip = n.Inputs[0].String()
			}

			if ccNode == nil && strings.HasPrefix(ip, "$(B)/") {
				ccNode = n
			}
		}
	}

	if r6Node == nil {
		t.Fatal("no R6 node emitted")
	}

	if ccNode == nil {
		t.Fatal("no R6-derived CC node emitted")
	}

	found := false

	for _, dep := range ccNode.Deps {
		if dep == r6Node.UID {
			found = true

			break
		}
	}

	if !found {
		t.Errorf("R6-derived CC.Deps = %v, want to contain R6 UID %q (PR-30 D04 Generator wiring)", ccNode.Deps, r6Node.UID)
	}
}

func TestEmitAR_NoPeerArchivesInDeps(t *testing.T) {
	tmp := t.TempDir()
	Throw(os.MkdirAll(filepath.Join(tmp, "lib_consumer"), 0o755))
	Throw(os.WriteFile(filepath.Join(tmp, "lib_consumer", "ya.make"), []byte(`LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(lib_peer)
SRCS(c.cpp)
END()
`), 0o644))

	Throw(os.MkdirAll(filepath.Join(tmp, "lib_peer"), 0o755))
	Throw(os.WriteFile(filepath.Join(tmp, "lib_peer", "ya.make"), []byte(`LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(p.cpp)
END()
`), 0o644))

	g := testGen(tmp, "lib_consumer")

	var consumerAR *Node

	for _, n := range g.Graph {
		if n.KV["p"] == "AR" && n.TargetProperties["module_dir"] == "lib_consumer" {
			consumerAR = n

			break
		}
	}

	if consumerAR == nil {
		t.Fatal("lib_consumer AR not found")
	}

	for _, dep := range consumerAR.Deps {
		for _, n := range g.Graph {
			if n.UID == dep && n.KV["p"] == "AR" {
				t.Errorf("lib_consumer AR has AR-typed dep (peer module_dir=%q); reference invariant: zero AR-on-AR deps", n.TargetProperties["module_dir"])
			}
		}
	}
}

func TestGen_PROGRAM_DefaultAllocator_TcmallocTc(t *testing.T) {
	tmp := t.TempDir()

	Throw(os.MkdirAll(filepath.Join(tmp, "myprog"), 0o755))

	Throw(os.WriteFile(filepath.Join(tmp, "myprog", "ya.make"), []byte(`PROGRAM(myprog)
NO_RUNTIME()
NO_UTIL()
SRCS(main.cpp)
END()
`), 0o644))

	Throw(os.MkdirAll(filepath.Join(tmp, "library/cpp/malloc/tcmalloc"), 0o755))
	Throw(os.WriteFile(filepath.Join(tmp, "library/cpp/malloc/tcmalloc", "ya.make"), []byte(`LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PLATFORM()
SRCS(stub.cpp)
END()
`), 0o644))

	Throw(os.MkdirAll(filepath.Join(tmp, "contrib/libs/tcmalloc/no_percpu_cache"), 0o755))
	Throw(os.WriteFile(filepath.Join(tmp, "contrib/libs/tcmalloc/no_percpu_cache", "ya.make"), []byte(`LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PLATFORM()
SRCS(stub.cpp)
END()
`), 0o644))

	host := newTestPlatform(OSLinux, ISAX8664, "yes", []string{"tool"})
	target := newTestPlatform(OSLinux, ISAX8664, "no", nil)
	g := Gen(tmp, "myprog", host, target, func(Warn) {})

	hasTcmalloc := false
	hasNoPercpu := false

	for _, n := range g.Graph {
		md := n.TargetProperties["module_dir"]

		if n.KV["p"] != "AR" {
			continue
		}

		switch md {
		case "library/cpp/malloc/tcmalloc":
			hasTcmalloc = true
		case "contrib/libs/tcmalloc/no_percpu_cache":
			hasNoPercpu = true
		}
	}

	if !hasTcmalloc {
		t.Errorf("expected AR with module_dir=library/cpp/malloc/tcmalloc; not found")
	}

	if !hasNoPercpu {
		t.Errorf("expected AR with module_dir=contrib/libs/tcmalloc/no_percpu_cache; not found")
	}
}

func TestGen_PROGRAM_ExplicitAllocator_NoTcmallocDefault(t *testing.T) {
	tmp := t.TempDir()

	Throw(os.MkdirAll(filepath.Join(tmp, "myprog"), 0o755))
	Throw(os.WriteFile(filepath.Join(tmp, "myprog", "ya.make"), []byte(`PROGRAM(myprog)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
ALLOCATOR(FAKE)
SRCS(main.cpp)
END()
`), 0o644))

	g := testGen(tmp, "myprog")

	for _, n := range g.Graph {
		md := n.TargetProperties["module_dir"]

		if md == "library/cpp/malloc/tcmalloc" || md == "contrib/libs/tcmalloc/no_percpu_cache" {
			t.Errorf("PROGRAM with ALLOCATOR(FAKE) emitted unexpected node module_dir=%q (TCMALLOC_TC default must be suppressed)", md)
		}
	}
}

func TestGen_SrcdirSibling_KeepsModuleDir(t *testing.T) {
	tmp := t.TempDir()

	Throw(os.MkdirAll(filepath.Join(tmp, "mylib"), 0o755))
	Throw(os.WriteFile(filepath.Join(tmp, "mylib", "ya.make"), []byte(`LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCDIR(other)
SRCS(src/foo.cpp)
END()
`), 0o644))

	g := testGen(tmp, "mylib")

	var ccNode *Node

	for _, n := range g.Graph {
		if n.KV["p"] == "CC" {
			ccNode = n

			break
		}
	}

	if ccNode == nil {
		t.Fatal("no CC node emitted")
	}

	if got := ccNode.TargetProperties["module_dir"]; got != "mylib" {
		t.Errorf("CC module_dir = %q, want %q (sibling SRCDIR — module_dir stays at instance.Path)", got, "mylib")
	}

	wantInput := "$(S)/other/src/foo.cpp"

	if len(ccNode.Inputs) == 0 || ccNode.Inputs[0].String() != wantInput {
		t.Errorf("CC input = %v, want first = %q", ccNode.Inputs, wantInput)
	}

	wantOutput := "$(B)/mylib/__/other/src/foo.cpp.o"

	if len(ccNode.Outputs) == 0 || ccNode.Outputs[0].String() != wantOutput {
		t.Errorf("CC output = %v, want first = %q", ccNode.Outputs, wantOutput)
	}
}

func TestGen_SrcdirLocal_IgnoresSrcdir(t *testing.T) {
	tmp := t.TempDir()

	Throw(os.MkdirAll(filepath.Join(tmp, "mylib"), 0o755))
	Throw(os.WriteFile(filepath.Join(tmp, "mylib", "ya.make"), []byte(`LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCDIR(other)
SRCS(local.c)
END()
`), 0o644))

	Throw(os.WriteFile(filepath.Join(tmp, "mylib", "local.c"), []byte("int x;\n"), 0o644))

	g := testGen(tmp, "mylib")

	var ccNode *Node

	for _, n := range g.Graph {
		if n.KV["p"] == "CC" {
			ccNode = n

			break
		}
	}

	if ccNode == nil {
		t.Fatal("no CC node emitted")
	}

	wantInput := "$(S)/mylib/local.c"

	if len(ccNode.Inputs) == 0 || ccNode.Inputs[0].String() != wantInput {
		t.Errorf("CC input = %v, want first = %q (local-existing source must ignore SRCDIR)", ccNode.Inputs, wantInput)
	}

	wantOutput := "$(B)/mylib/local.c.o"

	if len(ccNode.Outputs) == 0 || ccNode.Outputs[0].String() != wantOutput {
		t.Errorf("CC output = %v, want first = %q", ccNode.Outputs, wantOutput)
	}
}

func TestGen_AddInclMixed_OwnPathStaysOwn(t *testing.T) {
	root := t.TempDir()

	libDir := filepath.Join(root, "lib")
	Throw(os.MkdirAll(libDir, 0o755))
	Throw(os.MkdirAll(filepath.Join(root, "lib/include"), 0o755))
	Throw(os.WriteFile(filepath.Join(libDir, "ya.make"), []byte(
		"LIBRARY()\nADDINCL(\n    GLOBAL lib/include\n    lib/src\n)\nSRCS(lib.cpp)\nEND()\n",
	), 0o644))

	consDir := filepath.Join(root, "consumer")
	Throw(os.MkdirAll(consDir, 0o755))
	Throw(os.WriteFile(filepath.Join(consDir, "ya.make"), []byte(
		"LIBRARY()\nPEERDIR(lib)\nSRCS(main.cpp)\nEND()\n",
	), 0o644))

	g := testGen(root, "consumer")

	var consumerCC *Node

	for _, n := range g.Graph {
		if n.KV["p"] == "CC" {
			for _, out := range n.Outputs {
				if strings.Contains(out.String(), "main.cpp.o") {
					consumerCC = n
					break
				}
			}
		}
	}

	if consumerCC == nil {
		t.Fatal("consumer CC node for main.cpp not found")
	}

	var iFlags []string

	if len(consumerCC.Cmds) > 0 {
		for _, arg := range consumerCC.Cmds[0].CmdArgs {
			if strings.HasPrefix(arg, "-I") {
				iFlags = append(iFlags, arg)
			}
		}
	}

	wantGlobal := "-I$(S)/lib/include"
	foundGlobal := false

	for _, f := range iFlags {
		if f == wantGlobal {
			foundGlobal = true
			break
		}
	}

	if !foundGlobal {
		t.Errorf("consumer CC -I flags = %v; want %q (GLOBAL ADDINCL must propagate to peers)", iFlags, wantGlobal)
	}

	wantAbsent := "-I$(S)/lib/src"

	for _, f := range iFlags {
		if f == wantAbsent {
			t.Errorf("consumer CC -I flags = %v; must NOT contain %q (module-own ADDINCL must stay module-own, PR-31 D13)", iFlags, wantAbsent)
			break
		}
	}
}

func TestIsRuntimeAncestor_LiteralOnly(t *testing.T) {
	literals := []string{
		"contrib/libs/libc_compat",
		"contrib/libs/linuxvdso",
		"contrib/libs/cxxsupp/builtins",
		"contrib/libs/cxxsupp/libcxx",
		"contrib/libs/cxxsupp/libcxxrt",
		"contrib/libs/cxxsupp/libcxxabi",
		"contrib/libs/cxxsupp/libcxxabi-parts",
		"contrib/libs/libunwind",
		"library/cpp/malloc/api",
		"library/cpp/sanitizer/include",
		"util",
	}

	for _, p := range literals {
		if !isRuntimeAncestor(p) {
			t.Errorf("isRuntimeAncestor(%q) = false, want true (literal entry)", p)
		}
	}

	subtree := []string{
		"util/charset",
		"util/datetime/parser.rl6.cpp.o",
		"contrib/libs/cxxsupp/libcxxabi-parts/src",
		"contrib/libs/libunwind/private",
	}

	for _, p := range subtree {
		if isRuntimeAncestor(p) {
			t.Errorf("isRuntimeAncestor(%q) = true, want false (subtree extension dropped in PR-33 D01)", p)
		}
	}
}

func TestGen_SRC_AppendsExtraCFlags_PerSource(t *testing.T) {
	root := t.TempDir()

	modDir := filepath.Join(root, "mod")

	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir mod: %v", err)
	}

	yamake := []byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRC(foo.cpp -DSSE41_STUB)\nEND()\n")

	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644); err != nil {
		t.Fatalf("write mod/ya.make: %v", err)
	}

	g := testGen(root, "mod")

	var cc *Node

	for _, n := range g.Graph {
		if n.KV["p"] == "CC" {
			cc = n

			break
		}
	}

	if cc == nil {
		t.Fatal("no CC node emitted for SRC(foo.cpp ...)")
	}

	args := cc.Cmds[0].CmdArgs

	if len(args) < 2 {
		t.Fatalf("CC cmd_args too short: %d", len(args))
	}

	wantInput := "$(S)/mod/foo.cpp"

	if args[len(args)-1] != wantInput {
		t.Errorf("last cmd_arg = %q, want %q", args[len(args)-1], wantInput)
	}

	if args[len(args)-2] != "-DSSE41_STUB" {
		t.Errorf("second-to-last cmd_arg = %q, want %q (per-source CFLAGS slot)", args[len(args)-2], "-DSSE41_STUB")
	}
}

func TestGen_SRC_C_NO_LTO_RegistersSource(t *testing.T) {
	root := t.TempDir()

	modDir := filepath.Join(root, "mod")
	subDir := filepath.Join(modDir, "system")

	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("mkdir mod/system: %v", err)
	}

	yamake := []byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRC_C_NO_LTO(system/compiler.cpp)\nEND()\n")

	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644); err != nil {
		t.Fatalf("write mod/ya.make: %v", err)
	}

	g := testGen(root, "mod")

	var cc *Node

	for _, n := range g.Graph {
		if n.KV["p"] == "CC" {
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

	if cc.Outputs[0].String() != wantOut {
		t.Errorf("CC output = %q, want %q (SRC_C_NO_LTO uses flat output, not `mod/_/system/compiler.cpp.o`)", cc.Outputs[0].String(), wantOut)
	}

	args := cc.Cmds[0].CmdArgs

	if args[len(args)-1] != "$(S)/mod/system/compiler.cpp" {
		t.Errorf("last cmd_arg = %q, want input path", args[len(args)-1])
	}

	if args[len(args)-2] != "-fmacro-prefix-map=$(TOOL_ROOT)/=" {
		t.Errorf("second-to-last cmd_arg = %q, want %q (no per-source CFLAG)", args[len(args)-2], "-fmacro-prefix-map=$(TOOL_ROOT)/=")
	}
}

func TestGen_SRC_FlatOutputPath(t *testing.T) {
	root := t.TempDir()

	modDir := filepath.Join(root, "mod")

	if err := os.MkdirAll(filepath.Join(modDir, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir mod/sub: %v", err)
	}

	yamake := []byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRC(sub/x.cpp)\nEND()\n")

	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644); err != nil {
		t.Fatalf("write mod/ya.make: %v", err)
	}

	g := testGen(root, "mod")

	var cc *Node

	for _, n := range g.Graph {
		if n.KV["p"] == "CC" {
			cc = n

			break
		}
	}

	if cc == nil {
		t.Fatal("no CC node emitted for SRC(sub/x.cpp)")
	}

	wantOut := "$(B)/mod/sub/x.cpp.o"

	if len(cc.Outputs) != 1 || cc.Outputs[0].String() != wantOut {
		t.Errorf("CC output = %#v, want [%q] (SRC uses flat output, not `mod/_/sub/x.cpp.o`)", cc.Outputs, wantOut)
	}
}

func TestGen_SRC_RejectsZeroArgs(t *testing.T) {
	root := t.TempDir()

	modDir := filepath.Join(root, "mod")

	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir mod: %v", err)
	}

	yamake := []byte("LIBRARY()\nSRC()\nEND()\n")

	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644); err != nil {
		t.Fatalf("write mod/ya.make: %v", err)
	}

	exc := Try(func() {
		testGen(root, "mod")
	})

	if exc == nil {
		t.Fatal("expected exception for SRC() with no args, got nil")
	}

	if !strings.Contains(exc.Error(), "SRC()") {
		t.Errorf("error %q does not mention SRC()", exc.Error())
	}
}

func TestEvalCond_ARCH_ARM64_Aliased(t *testing.T) {
	inst := ModuleInstance{Kind: KindLib, Platform: testTargetP}
	env := buildIfEnv(inst)

	if !EvalCond(&ExprIdent{Name: "ARCH_ARM64"}, env) {
		t.Errorf("EvalCond(ARCH_ARM64) on aarch64 instance = false, want true (alias for ARCH_AARCH64)")
	}

	if !EvalCond(&ExprIdent{Name: "ARCH_AARCH64"}, env) {
		t.Errorf("EvalCond(ARCH_AARCH64) on aarch64 instance = false, want true")
	}

	hostInst := ModuleInstance{Kind: KindLib, Platform: testHostP}
	hostEnv := buildIfEnv(hostInst)

	if EvalCond(&ExprIdent{Name: "ARCH_ARM64"}, hostEnv) {
		t.Errorf("EvalCond(ARCH_ARM64) on x86_64 instance = true, want false")
	}

	if !EvalCond(&ExprIdent{Name: "ARCH_X86_64"}, hostEnv) {
		t.Errorf("EvalCond(ARCH_X86_64) on x86_64 instance = false, want true")
	}
}

func TestGen_PR35y_R7_JoinSrcs_SuppressBuildRootShim(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "joinmod")
	Throw(os.MkdirAll(modDir, 0o755))

	yamake := []byte(`LIBRARY()
JOIN_SRCS(all_my.cpp src1.cpp src2.cpp)
END()
`)
	Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644))

	g := testGen(root, "joinmod")

	var arNode *Node

	for _, n := range g.Graph {
		if n.KV["p"] == "AR" {
			arNode = n

			break
		}
	}

	if arNode == nil {
		t.Fatal("no AR node emitted")
	}

	const forbidden = "$(B)/joinmod/all_my.cpp"

	for _, in := range arNode.Inputs {
		if in.String() == forbidden {
			t.Errorf("AR.Inputs contains %q — JS-derived BUILD_ROOT shim must be filtered (PR-35y R7)", forbidden)
		}
	}

	for _, src := range []string{"$(S)/joinmod/src1.cpp", "$(S)/joinmod/src2.cpp"} {
		if nodeHasInput(arNode, src) {
			t.Errorf("AR.Inputs must not contain JS member source %q: %#v", src, arNode.Inputs)
		}
	}
}

func TestGen_PR35y_R7_RagelRl6_OriginalSourcePair(t *testing.T) {
	root := t.TempDir()
	consumerDir := filepath.Join(root, "consumer")
	Throw(os.MkdirAll(consumerDir, 0o755))
	Throw(os.WriteFile(filepath.Join(consumerDir, "ya.make"), []byte("LIBRARY()\nSRCS(parser.rl6)\nEND()\n"), 0o644))

	Throw(os.WriteFile(filepath.Join(consumerDir, "parser.rl6"), []byte("// fixture\n"), 0o644))
	Throw(os.WriteFile(filepath.Join(consumerDir, "parser.h"), []byte("// fixture\n"), 0o644))

	ragelDir := filepath.Join(root, "contrib/tools/ragel6/bin")
	Throw(os.MkdirAll(ragelDir, 0o755))
	Throw(os.WriteFile(filepath.Join(ragelDir, "ya.make"), []byte("PROGRAM(ragel6)\nSRCS(main.cpp)\nEND()\n"), 0o644))

	g := testGen(root, "consumer")

	var arNode *Node

	for _, n := range g.Graph {
		if n.KV["p"] == "AR" && n.TargetProperties["module_dir"] == "consumer" {
			arNode = n

			break
		}
	}

	if arNode == nil {
		t.Fatal("no consumer AR node emitted")
	}

	const forbidden = "$(B)/consumer/_/parser.rl6.cpp"

	for _, in := range arNode.Inputs {
		if in.String() == forbidden {
			t.Errorf("AR.Inputs contains %q — R6-derived BUILD_ROOT shim must be filtered (PR-35y R7)", forbidden)
		}
	}

	for _, src := range []string{"$(S)/consumer/parser.rl6", "$(S)/consumer/parser.h"} {
		if nodeHasInput(arNode, src) {
			t.Errorf("AR.Inputs must not contain member source %q: %#v", src, arNode.Inputs)
		}
	}
}

func TestGen_PR35y_R8_RegularARIncludesGlobalMemberInputs(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "globalmod")
	Throw(os.MkdirAll(modDir, 0o755))

	yamake := []byte(`LIBRARY()
GLOBAL_SRCS(global.cpp)
SRCS(regular.cpp)
END()
`)
	Throw(os.WriteFile(filepath.Join(modDir, "ya.make"), yamake, 0o644))

	g := testGen(root, "globalmod")

	var (
		regularAR *Node
		globalAR  *Node
	)

	for _, n := range g.Graph {
		if n.KV["p"] != "AR" {
			continue
		}

		if n.TargetProperties["module_tag"] == "global" {
			globalAR = n
		} else {
			regularAR = n
		}
	}

	if regularAR == nil || globalAR == nil {
		t.Fatalf("expected both regular and global AR (got regular=%v, global=%v)", regularAR != nil, globalAR != nil)
	}

	regularInputs := map[string]bool{}
	for _, in := range regularAR.Inputs {
		regularInputs[in.String()] = true
	}

	globalInputs := map[string]bool{}
	for _, in := range globalAR.Inputs {
		globalInputs[in.String()] = true
	}

	const (
		regularSrc = "$(S)/globalmod/regular.cpp"
		globalSrc  = "$(S)/globalmod/global.cpp"
	)

	for _, src := range []string{regularSrc, globalSrc} {
		if regularInputs[src] {
			t.Errorf("regular AR.Inputs must not contain member source %q: %#v", src, regularAR.Inputs)
		}
	}
	if globalInputs[globalSrc] {
		t.Errorf(".global.a AR.Inputs must not contain member source %q: %#v", globalSrc, globalAR.Inputs)
	}

	hasObject := func(n *Node) bool {
		for _, in := range n.Inputs {
			if strings.HasSuffix(in.Rel(), ".o") {
				return true
			}
		}
		return false
	}
	if !hasObject(regularAR) {
		t.Errorf("regular AR.Inputs has no object: %#v", regularAR.Inputs)
	}
	if !hasObject(globalAR) {
		t.Errorf(".global.a AR.Inputs has no object: %#v", globalAR.Inputs)
	}
}

func TestGen_PR35y_R8_AsmSrcdirRebase(t *testing.T) {
	root := t.TempDir()

	Throw(os.MkdirAll(filepath.Join(root, "mod/inner"), 0o755))
	Throw(os.WriteFile(filepath.Join(root, "mod/inner", "ya.make"), []byte(`LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PLATFORM()
SRCDIR(mod)
SRCS(sub/foo.S)
END()
`), 0o644))

	Throw(os.MkdirAll(filepath.Join(root, "mod/sub"), 0o755))
	Throw(os.WriteFile(filepath.Join(root, "mod/sub", "foo.S"), []byte("// asm\n"), 0o644))

	g := testGen(root, "mod/inner")

	var asNode *Node

	for _, n := range g.Graph {
		if n.KV["p"] == "AS" {
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
		t.Errorf("AS.Inputs contains %q — SRCDIR rebase must redirect to %q (PR-35y R8)", forbidden, want)
	}
	if !nodeHasInput(asNode, want) {
		t.Errorf("AS.Inputs missing %q — PR-35y R8 SRCDIR rebase for `.S` source: %#v", want, asNode.Inputs)
	}
}

func TestGen_ProtoAstStylePipelineExpandsLowercaseVarsAndRootedPaths(t *testing.T) {
	root := t.TempDir()

	writeFile := func(rel, body string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(rel), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	writeToolProgram(t, root, "contrib/tools/protoc/bin", "protoc")
	writeToolProgram(t, root, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")

	writeFile("proto/ya.make", `LIBRARY()
SET(antlr_output ${ARCADIA_BUILD_ROOT}/${MODDIR})
SET(antlr_templates ${antlr_output}/templates)
SET(sql_grammar ${antlr_output}/Grammar.g)
SET(PROTOC_PATH contrib/tools/protoc/bin)

CONFIGURE_FILE(${ARCADIA_ROOT}/templates/Java.stg.in ${antlr_templates}/Java/Java.stg)
CONFIGURE_FILE(${ARCADIA_ROOT}/templates/Grammar.g.in ${sql_grammar})

RUN_ANTLR4(
    ${sql_grammar}
    -lib .
    -o ${antlr_output}
    IN ${sql_grammar} ${antlr_templates}/Java/Java.stg
	    OUT_NOAUTO Generated.proto
	    CWD ${antlr_output}
)

RUN_PROGRAM(
	    $PROTOC_PATH
	    -I=${CURDIR} -I=${ARCADIA_ROOT} -I=${ARCADIA_BUILD_ROOT} -I=${ARCADIA_ROOT}/contrib/libs/protobuf/src
	    --cpp_out=${ARCADIA_BUILD_ROOT} --cpp_styleguide_out=${ARCADIA_BUILD_ROOT}
	    --plugin=protoc-gen-cpp_styleguide=contrib/tools/protoc/plugins/cpp_styleguide
	    Generated.proto
	    IN Generated.proto
	    TOOL contrib/tools/protoc/plugins/cpp_styleguide
	    OUT_NOAUTO Generated.pb.h Generated.pb.cc
	    CWD ${antlr_output}
)

RUN_PYTHON3(
	    ${ARCADIA_ROOT}/tools/multiproto.py Generated
	    IN Generated.pb.h Generated.pb.cc
	    OUT_NOAUTO Generated.code0.cc Generated.main.h
	    CWD ${antlr_output}
)

SRCS(Generated.code0.cc)
END()
`)
	writeFile("templates/Java.stg.in", "java template\n")
	writeFile("templates/Grammar.g.in", "grammar Generated;\n")
	writeFile("tools/multiproto.py", "print('ok')\n")
	writeFile("build/scripts/configure_file.py", "print('cfg')\n")
	writeFile("build/scripts/stdout2stderr.py", "print('stderr')\n")
	writeFile("contrib/java/antlr/antlr4/antlr.jar", "")

	g := testGen(root, "proto")

	cfTemplate := findGraphNodeByOutputs(t, g, "$(B)/proto/templates/Java/Java.stg")
	cfGrammar := findGraphNodeByOutputs(t, g, "$(B)/proto/Grammar.g")
	antlr := findGraphNodeByOutputs(t, g, "$(B)/proto/Generated.proto")
	protoc := findGraphNodeByOutputs(t, g, "$(B)/proto/Generated.pb.h", "$(B)/proto/Generated.pb.cc")
	py := findGraphNodeByOutputs(t, g, "$(B)/proto/Generated.code0.cc", "$(B)/proto/Generated.main.h")
	cc := findGraphNodeByOutputs(t, g, "$(B)/proto/Generated.code0.cc.o")
	ar := findGraphNodeByOutputs(t, g, "$(B)/proto/libproto.a")

	if got := cfTemplate.Inputs[1].String(); got != "$(S)/templates/Java.stg.in" {
		t.Fatalf("template CF input = %q, want $(S)/templates/Java.stg.in", got)
	}
	if got := cfTemplate.Cmds[0].CmdArgs[3]; got != "$(B)/proto/templates/Java/Java.stg" {
		t.Fatalf("template CF output arg = %q, want $(B)/proto/templates/Java/Java.stg", got)
	}
	if got := cfGrammar.Cmds[0].CmdArgs[3]; got != "$(B)/proto/Grammar.g" {
		t.Fatalf("grammar CF output arg = %q, want $(B)/proto/Grammar.g", got)
	}

	if got := antlr.Cmds[0].CmdArgs[5]; got != "$(B)/proto/Grammar.g" {
		t.Fatalf("antlr grammar arg = %q, want $(B)/proto/Grammar.g", got)
	}
	if got := antlr.Cmds[0].CmdArgs[9]; got != "$(B)/proto" {
		t.Fatalf("antlr output dir arg = %q, want $(B)/proto", got)
	}
	if got := antlr.Cmds[0].Cwd; got != "$(B)/proto" {
		t.Fatalf("antlr cwd = %q, want $(B)/proto", got)
	}

	if got := protoc.Cmds[0].CmdArgs[0]; got != "$(B)/contrib/tools/protoc/bin/protoc" {
		t.Fatalf("protoc tool arg = %q, want $(B)/contrib/tools/protoc/bin/protoc", got)
	}
	wantPluginArg := "--plugin=protoc-gen-cpp_styleguide=$(B)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide"
	if !containsString(protoc.Cmds[0].CmdArgs, wantPluginArg) {
		t.Fatalf("protoc cmd args missing %q: %v", wantPluginArg, protoc.Cmds[0].CmdArgs)
	}
	if !nodeHasInput(protoc, "$(B)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide") {
		t.Fatalf("protoc inputs missing built plugin binary: %v", protoc.Inputs)
	}
	if nodeHasInput(protoc, "$(S)/contrib/tools/protoc/plugins/cpp_styleguide") {
		t.Fatalf("protoc inputs still contain source-root plugin path: %v", protoc.Inputs)
	}

	if got := py.Inputs[0].String(); got != "$(S)/tools/multiproto.py" {
		t.Fatalf("python script input = %q, want $(S)/tools/multiproto.py", got)
	}
	if got := py.Inputs[1].String(); got != "$(B)/proto/Generated.pb.h" {
		t.Fatalf("python generated header input = %q, want $(B)/proto/Generated.pb.h", got)
	}
	if got := py.Inputs[2].String(); got != "$(B)/proto/Generated.pb.cc" {
		t.Fatalf("python generated source input = %q, want $(B)/proto/Generated.pb.cc", got)
	}
	if got := py.Cmds[0].CmdArgs[1]; got != "$(S)/tools/multiproto.py" {
		t.Fatalf("python script arg = %q, want $(S)/tools/multiproto.py", got)
	}
	if got := py.Cmds[0].Cwd; got != "$(B)/proto" {
		t.Fatalf("python cwd = %q, want $(B)/proto", got)
	}

	if !containsString(cc.Cmds[0].CmdArgs, "$(B)/proto/Generated.code0.cc") {
		t.Fatalf("cc cmd args missing built generated source: %v", cc.Cmds[0].CmdArgs)
	}
	if containsString(cc.Cmds[0].CmdArgs, "$(S)/proto/Generated.code0.cc") {
		t.Fatalf("cc cmd args still contain source-root generated source: %v", cc.Cmds[0].CmdArgs)
	}
	if !nodeHasInput(cc, "$(B)/proto/Generated.code0.cc") {
		t.Fatalf("cc inputs missing built generated source: %v", cc.Inputs)
	}
	for _, want := range []string{
		"$(S)/tools/multiproto.py",
		"$(S)/build/scripts/stdout2stderr.py",
		"$(S)/contrib/java/antlr/antlr4/antlr.jar",
		"$(S)/build/scripts/configure_file.py",
		"$(S)/templates/Java.stg.in",
		"$(S)/templates/Grammar.g.in",
	} {
		if !nodeHasInput(cc, want) {
			t.Fatalf("cc inputs missing generator closure %q: %v", want, cc.Inputs)
		}
	}

	if nodeHasInput(ar, "$(S)/proto/Generated.code0.cc") {
		t.Fatalf("ar inputs still contain source-root generated source: %v", ar.Inputs)
	}
	if nodeHasInput(ar, "$(B)/proto/Generated.code0.cc") {
		t.Fatalf("ar inputs still contain build-root generated source: %v", ar.Inputs)
	}

	for _, absent := range []string{
		"$(S)/tools/multiproto.py",
		"$(S)/contrib/java/antlr/antlr4/antlr.jar",
		"$(S)/templates/Java.stg.in",
		"$(S)/templates/Grammar.g.in",
	} {
		if nodeHasInput(ar, absent) {
			t.Fatalf("ar inputs must not contain generator-closure source %q: %v", absent, ar.Inputs)
		}
	}

	assertNodeHasNoRawProtoAstPlaceholders := func(node *Node) {
		t.Helper()

		var values []string
		for _, input := range node.Inputs {
			values = append(values, input.String())
		}
		for _, output := range node.Outputs {
			values = append(values, output.String())
		}
		for _, cmd := range node.Cmds {
			values = append(values, cmd.CmdArgs...)
			if cmd.Cwd != "" {
				values = append(values, cmd.Cwd)
			}
			if cmd.Stdout != "" {
				values = append(values, cmd.Stdout)
			}
		}

		for _, value := range values {
			if strings.Contains(value, "${") {
				t.Fatalf("%s contains unresolved placeholder %q", node.KV["p"], value)
			}
			if strings.Contains(value, "/$(S)/") || strings.Contains(value, "/$(B)/") {
				t.Fatalf("%s contains duplicated rooted path %q", node.KV["p"], value)
			}
		}
	}

	assertNodeHasNoRawProtoAstPlaceholders(cfTemplate)
	assertNodeHasNoRawProtoAstPlaceholders(cfGrammar)
	assertNodeHasNoRawProtoAstPlaceholders(antlr)
	assertNodeHasNoRawProtoAstPlaceholders(protoc)
	assertNodeHasNoRawProtoAstPlaceholders(py)
	assertNodeHasNoRawProtoAstPlaceholders(cc)
	assertNodeHasNoRawProtoAstPlaceholders(ar)
}

func TestGen_ProtoLibrary_CPPProtoPlugin0WiresToolDeps(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "protos/ya.make", `PROTO_LIBRARY()
PROTOC_FATAL_WARNINGS()
GRPC()
CPP_PROTO_PLUGIN0(config_proto_plugin tools/config_plugin DEPS deps/generated_runtime)
SRCS(test.proto)
END()
`)
	writeTestModuleFile(t, root, "protos/test.proto", `syntax = "proto3";
package test;
message Row {}
`)

	writeToolProgram(t, root, "contrib/tools/protoc", "protoc")
	writeToolProgram(t, root, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(t, root, "contrib/tools/protoc/plugins/grpc_cpp", "grpc_cpp")

	writeTestModuleFile(t, root, "tools/config_plugin/ya.make", `PROGRAM(config_proto_plugin)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(
    deps/plugin_runtime
)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(t, root, "tools/config_plugin/main.cpp", "int main(){return 0;}\n")

	writeTestModuleFile(t, root, "contrib/libs/grpc/ya.make", "LIBRARY()\nSRCS(grpc.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "contrib/libs/grpc/grpc.cpp", "int grpc(){return 0;}\n")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")
	writeTestModuleFile(t, root, "deps/generated_runtime/ya.make", "LIBRARY()\nSRCS(gen.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "deps/generated_runtime/gen.cpp", "int gen(){return 0;}\n")
	writeTestModuleFile(t, root, "deps/plugin_runtime/ya.make", "LIBRARY()\nSRCS(runtime.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "deps/plugin_runtime/runtime.cpp", "int runtime(){return 0;}\n")

	g := testGen(root, "protos")

	pb := findGraphNodeByOutputs(t, g,
		"$(B)/protos/test.pb.h",
		"$(B)/protos/test.pb.cc",
		"$(B)/protos/test.grpc.pb.cc",
		"$(B)/protos/test.grpc.pb.h",
	)
	styleguide := mustNodeByOutput(t, g, "$(B)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide")
	grpcCpp := mustNodeByOutput(t, g, "$(B)/contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp")
	protoc := mustNodeByOutput(t, g, "$(B)/contrib/tools/protoc/protoc")
	configPlugin := mustNodeByOutput(t, g, "$(B)/tools/config_plugin/config_proto_plugin")
	pluginRuntime := mustNodeByOutput(t, g, "$(B)/deps/plugin_runtime/libdeps-plugin_runtime.a")
	_ = mustNodeByOutput(t, g, "$(B)/deps/generated_runtime/libdeps-generated_runtime.a")

	if !containsString(pb.Cmds[0].CmdArgs, "--plugin=protoc-gen-config_proto_plugin=$(B)/tools/config_plugin/config_proto_plugin") {
		t.Fatalf("pb cmd args missing config proto plugin: %v", pb.Cmds[0].CmdArgs)
	}
	if !containsString(pb.Cmds[0].CmdArgs, "--config_proto_plugin_out=$(B)/") {
		t.Fatalf("pb cmd args missing config proto plugin out flag: %v", pb.Cmds[0].CmdArgs)
	}

	sourceIdx := indexOfArg(pb.Cmds[0].CmdArgs, "protos/test.proto")
	grpcIdx := indexOfArg(pb.Cmds[0].CmdArgs, "--plugin=protoc-gen-grpc_cpp=$(B)/contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp")
	configIdx := indexOfArg(pb.Cmds[0].CmdArgs, "--plugin=protoc-gen-config_proto_plugin=$(B)/tools/config_plugin/config_proto_plugin")
	if sourceIdx < 0 || grpcIdx < 0 || configIdx < 0 {
		t.Fatalf("missing source/grpc/config args in pb cmd: %v", pb.Cmds[0].CmdArgs)
	}
	if !(sourceIdx < grpcIdx && grpcIdx < configIdx) {
		t.Fatalf("pb plugin arg order = source:%d grpc:%d config:%d, want source < grpc < config", sourceIdx, grpcIdx, configIdx)
	}

	inputs := make([]string, 0, len(pb.Inputs))
	for _, input := range pb.Inputs {
		inputs = append(inputs, input.String())
	}
	wantInputsPrefix := []string{
		"$(B)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide",
		"$(B)/contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp",
		"$(B)/contrib/tools/protoc/protoc",
		"$(B)/tools/config_plugin/config_proto_plugin",
		"$(S)/build/scripts/cpp_proto_wrapper.py",
		"$(S)/protos/test.proto",
	}
	if len(inputs) < len(wantInputsPrefix) || !equalStrings(inputs[:len(wantInputsPrefix)], wantInputsPrefix) {
		t.Fatalf("pb inputs prefix = %v, want %v", inputs, wantInputsPrefix)
	}

	wantDeps := []string{styleguide.UID, grpcCpp.UID, protoc.UID, configPlugin.UID}
	if len(pb.Deps) != len(wantDeps) {
		t.Fatalf("pb deps len = %d, want %d (%v)", len(pb.Deps), len(wantDeps), pb.Deps)
	}
	for _, want := range wantDeps {
		if !containsString(pb.Deps, want) {
			t.Fatalf("pb deps = %v, missing %q", pb.Deps, want)
		}
	}
	if got := pb.ForeignDeps["tool"]; len(got) != len(wantDeps) {
		t.Fatalf("pb foreign_deps[tool] len = %d, want %d (%v)", len(got), len(wantDeps), got)
	} else {
		for _, want := range wantDeps {
			if !containsString(got, want) {
				t.Fatalf("pb foreign_deps[tool] = %v, missing %q", got, want)
			}
		}
	}
	if !nodeHasHostTag(configPlugin.Tags) {
		t.Fatalf("config proto plugin tags = %v, want host tool tag", configPlugin.Tags)
	}
	if !containsString(configPlugin.Deps, pluginRuntime.UID) {
		t.Fatalf("config proto plugin deps = %v, want runtime peer uid %q", configPlugin.Deps, pluginRuntime.UID)
	}
}

func TestGen_ProtoLibrary_CPPProtoPluginOutputsReachWrapper(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "protos/ya.make", `PROTO_LIBRARY()
CPP_PROTO_PLUGIN(tasklet_cpp tools/tasklet_plugin .tasklet.h)
SRCS(test.proto)
END()
`)
	writeTestModuleFile(t, root, "protos/test.proto", `syntax = "proto3";
package test;
message Row {}
`)

	writeToolProgram(t, root, "contrib/tools/protoc", "protoc")
	writeToolProgram(t, root, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	writeTestModuleFile(t, root, "tools/tasklet_plugin/ya.make", `PROGRAM(tasklet_cpp)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(t, root, "tools/tasklet_plugin/main.cpp", "int main(){return 0;}\n")

	g := testGen(root, "protos")

	pb := findGraphNodeByOutputs(t, g,
		"$(B)/protos/test.pb.h",
		"$(B)/protos/test.pb.cc",
		"$(B)/protos/test.tasklet.h",
	)

	outputsIdx := indexOfArg(pb.Cmds[0].CmdArgs, "--outputs")
	separatorIdx := indexOfArg(pb.Cmds[0].CmdArgs, "--")
	if outputsIdx < 0 || separatorIdx < 0 || separatorIdx <= outputsIdx {
		t.Fatalf("pb wrapper output section malformed: %v", pb.Cmds[0].CmdArgs)
	}

	wantWrapperOutputs := []string{
		"$(B)/protos/test.pb.h",
		"$(B)/protos/test.pb.cc",
		"$(B)/protos/test.tasklet.h",
	}
	gotWrapperOutputs := pb.Cmds[0].CmdArgs[outputsIdx+1 : separatorIdx]
	if !equalStrings(gotWrapperOutputs, wantWrapperOutputs) {
		t.Fatalf("pb wrapper outputs = %v, want %v", gotWrapperOutputs, wantWrapperOutputs)
	}
}

func TestGen_ProtoLibrary_CPPProtoPlugin2HeaderConsumerInheritsProtoClosure(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "protos/ya.make", `PROTO_LIBRARY()
CPP_PROTO_PLUGIN2(grpc_cpp contrib/tools/protoc/plugins/grpc_cpp .grpc.pb.cc .grpc.pb.h DEPS contrib/libs/grpc)
SRCS(
    dep.proto
    main.proto
)
END()
`)
	writeTestModuleFile(t, root, "protos/dep.proto", `syntax = "proto3";
package test;
message Dep {
  string value = 1;
}
`)
	writeTestModuleFile(t, root, "protos/main.proto", `syntax = "proto3";
package test;
import "dep.proto";
message Main {
  Dep dep = 1;
}
service TestService {
  rpc Ping(Main) returns (Main);
}
`)
	writeTestModuleFile(t, root, "app/ya.make", `LIBRARY()
PEERDIR(protos)
SRCS(use.cpp)
END()
`)
	writeTestModuleFile(t, root, "app/use.cpp", `#include <protos/main.grpc.pb.h>
int use() { return 0; }
`)

	writeToolProgram(t, root, "contrib/tools/protoc", "protoc")
	writeToolProgram(t, root, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(t, root, "contrib/tools/protoc/plugins/grpc_cpp", "grpc_cpp")

	writeTestModuleFile(t, root, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(t, root, "contrib/libs/grpc/ya.make", "LIBRARY()\nSRCS(grpc.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "contrib/libs/grpc/grpc.cpp", "int grpc(){return 0;}\n")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(root, "app")

	useCC := mustNodeByOutput(t, g, "$(B)/app/use.cpp.o")
	mainPB := mustNodeByOutput(t, g, "$(B)/protos/main.pb.h")
	depPB := mustNodeByOutput(t, g, "$(B)/protos/dep.pb.h")

	for _, want := range []string{
		"$(B)/protos/main.grpc.pb.h",
		"$(B)/protos/main.pb.h",
		"$(B)/protos/dep.pb.h",
	} {
		if !nodeHasInput(useCC, want) {
			t.Fatalf("use.cpp.o inputs missing %q: %#v", want, useCC.Inputs)
		}
	}

	for _, want := range []string{mainPB.UID, depPB.UID} {
		if !containsString(useCC.Deps, want) {
			t.Fatalf("use.cpp.o deps missing %q: %v", want, useCC.Deps)
		}
	}
}

func TestGen_ProtoLibrary_CPPProtoPlugin2GeneratedSourceCompilesAndArchives(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "protos/ya.make", `PROTO_LIBRARY()
CPP_PROTO_PLUGIN2(grpc_cpp contrib/tools/protoc/plugins/grpc_cpp .grpc.pb.cc .grpc.pb.h DEPS contrib/libs/grpc)
SRCS(
    dep.proto
    main.proto
)
END()
`)
	writeTestModuleFile(t, root, "protos/dep.proto", `syntax = "proto3";
package test;
message Dep {
  string value = 1;
}
`)
	writeTestModuleFile(t, root, "protos/main.proto", `syntax = "proto3";
package test;
import "dep.proto";
message Main {
  Dep dep = 1;
}
service TestService {
  rpc Ping(Main) returns (Main);
}
`)

	writeToolProgram(t, root, "contrib/tools/protoc", "protoc")
	writeToolProgram(t, root, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(t, root, "contrib/tools/protoc/plugins/grpc_cpp", "grpc_cpp")

	writeTestModuleFile(t, root, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(t, root, "contrib/libs/grpc/ya.make", "LIBRARY()\nSRCS(grpc.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "contrib/libs/grpc/grpc.cpp", "int grpc(){return 0;}\n")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(root, "protos")

	grpcCC := mustNodeByOutput(t, g, "$(B)/protos/main.grpc.pb.cc.o")
	mainPB := mustNodeByOutput(t, g, "$(B)/protos/main.pb.h")
	depPB := mustNodeByOutput(t, g, "$(B)/protos/dep.pb.h")
	ar := mustNodeByOutput(t, g, "$(B)/protos/libprotos.a")

	for _, want := range []string{
		"$(B)/protos/main.grpc.pb.cc",
		"$(B)/protos/main.pb.h",
		"$(B)/protos/dep.pb.h",
	} {
		if !nodeHasInput(grpcCC, want) {
			t.Fatalf("main.grpc.pb.cc.o inputs missing %q: %#v", want, grpcCC.Inputs)
		}
	}

	for _, want := range []string{mainPB.UID, depPB.UID} {
		if !containsString(grpcCC.Deps, want) {
			t.Fatalf("main.grpc.pb.cc.o deps missing %q: %v", want, grpcCC.Deps)
		}
	}

	if !nodeHasInput(ar, "$(B)/protos/main.grpc.pb.cc.o") {
		t.Fatalf("archive inputs missing grpc object: %#v", ar.Inputs)
	}
}

func TestGen_ProtoLibrary_TransitiveHeadersNoAddsDepsHeaderAndEnumUsesGeneratedPBHeader(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "protos/ya.make", `PROTO_LIBRARY()
SET(PROTOC_TRANSITIVE_HEADERS "no")
GRPC()
GENERATE_ENUM_SERIALIZATION(main.pb.h)
SRCS(
    dep.proto
    main.proto
)
END()
`)
	writeTestModuleFile(t, root, "protos/dep.proto", `syntax = "proto3";
package test;
message Dep {
  string value = 1;
}
`)
	writeTestModuleFile(t, root, "protos/main.proto", `syntax = "proto3";
package test;
import "dep.proto";
enum Mode {
  MODE_UNSPECIFIED = 0;
  MODE_READY = 1;
}
message Main {
  Dep dep = 1;
  Mode mode = 2;
}
service TestService {
  rpc Ping(Main) returns (Main);
}
`)
	writeTestModuleFile(t, root, "app/ya.make", `LIBRARY()
PEERDIR(protos)
SRCS(use.cpp)
END()
`)
	writeTestModuleFile(t, root, "app/use.cpp", `#include <protos/main.deps.pb.h>
int use() { return 0; }
`)

	writeToolProgram(t, root, "contrib/tools/protoc", "protoc")
	writeToolProgram(t, root, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(t, root, "contrib/tools/protoc/plugins/grpc_cpp", "grpc_cpp")
	writeToolProgram(t, root, "tools/enum_parser/enum_parser", "enum_parser")
	writeTestModuleFile(t, root, "tools/enum_parser/enum_serialization_runtime/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(runtime.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "tools/enum_parser/enum_serialization_runtime/runtime.cpp", "int runtime(){return 0;}\n")

	writeTestModuleFile(t, root, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(t, root, "contrib/libs/grpc/ya.make", "LIBRARY()\nSRCS(grpc.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "contrib/libs/grpc/grpc.cpp", "int grpc(){return 0;}\n")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(root, "app")

	pb := findGraphNodeByOutputs(t, g,
		"$(B)/protos/main.pb.h",
		"$(B)/protos/main.pb.cc",
		"$(B)/protos/main.deps.pb.h",
		"$(B)/protos/main.grpc.pb.cc",
		"$(B)/protos/main.grpc.pb.h",
	)
	if !containsString(pb.Cmds[0].CmdArgs, "--cpp_out=proto_h=true:$(B)/") {
		t.Fatalf("pb cmd args missing lite-header cpp_out flag: %v", pb.Cmds[0].CmdArgs)
	}

	useCC := mustNodeByOutput(t, g, "$(B)/app/use.cpp.o")
	mainPB := mustNodeByOutput(t, g, "$(B)/protos/main.pb.h")
	depPB := mustNodeByOutput(t, g, "$(B)/protos/dep.pb.h")
	ar := mustNodeByOutput(t, g, "$(B)/protos/libprotos.a")
	for _, want := range []string{
		"$(B)/protos/main.deps.pb.h",
		"$(B)/protos/main.pb.h",
		"$(B)/protos/dep.pb.h",
	} {
		if !nodeHasInput(useCC, want) {
			t.Fatalf("use.cpp.o inputs missing %q: %#v", want, useCC.Inputs)
		}
	}
	for _, want := range []string{mainPB.UID, depPB.UID} {
		if !containsString(useCC.Deps, want) {
			t.Fatalf("use.cpp.o deps missing %q: %v", want, useCC.Deps)
		}
	}

	en := mustNodeByOutput(t, g, "$(B)/protos/main.pb.h_serialized.cpp")
	if !nodeHasInput(en, "$(B)/protos/main.pb.h") {
		t.Fatalf("enum node inputs missing generated pb.h: %#v", en.Inputs)
	}
	if nodeHasInput(en, "$(S)/protos/main.pb.h") {
		t.Fatalf("enum node still consumes source-root pb.h: %#v", en.Inputs)
	}
	if !containsString(en.Deps, mainPB.UID) {
		t.Fatalf("enum node deps missing pb producer uid %q: %v", mainPB.UID, en.Deps)
	}
	if !containsString(en.Deps, depPB.UID) {
		t.Fatalf("enum node deps missing imported pb producer uid %q: %v", depPB.UID, en.Deps)
	}
	if got := en.TargetProperties["module_tag"]; got != "cpp_proto" {
		t.Fatalf("enum node module_tag = %q, want cpp_proto", got)
	}
	if !nodeHasInput(en, "$(B)/protos/dep.pb.h") {
		t.Fatalf("enum node inputs missing imported pb.h dep.pb.h: %#v", en.Inputs)
	}
	if !nodeHasInput(en, "$(S)/contrib/libs/protobuf/src/google/protobuf/message.h") {
		t.Fatalf("enum node inputs missing protobuf runtime header message.h: %#v", en.Inputs)
	}

	if !nodeHasInput(ar, "$(B)/protos/main.pb.h_serialized.cpp.o") {
		t.Fatalf("archive missing enum serialization object: %#v", ar.Inputs)
	}
}

func TestGen_ProtoLibrary_TransitiveHeadersNoKeepsPublicImportsOnLitePBHeader(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "protos/ya.make", `PROTO_LIBRARY()
SET(PROTOC_TRANSITIVE_HEADERS "no")
SRCS(
    leaf.proto
    public.proto
    main.proto
)
END()
`)
	writeTestModuleFile(t, root, "protos/leaf.proto", `syntax = "proto3";
package test;
message Leaf {
  string value = 1;
}
`)
	writeTestModuleFile(t, root, "protos/public.proto", `syntax = "proto3";
package test;
import public "leaf.proto";
message PublicMessage {
  Leaf leaf = 1;
}
`)
	writeTestModuleFile(t, root, "protos/main.proto", `syntax = "proto3";
package test;
import public "public.proto";
message Main {
  PublicMessage message = 1;
}
`)
	writeTestModuleFile(t, root, "app/ya.make", `LIBRARY()
PEERDIR(protos)
SRCS(use.cpp)
END()
`)
	writeTestModuleFile(t, root, "app/use.cpp", `#include <protos/main.pb.h>
int use() { return 0; }
`)

	writeToolProgram(t, root, "contrib/tools/protoc", "protoc")
	writeToolProgram(t, root, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(t, root, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(root, "app")

	useCC := mustNodeByOutput(t, g, "$(B)/app/use.cpp.o")
	mainPB := mustNodeByOutput(t, g, "$(B)/protos/main.pb.h")
	publicPB := mustNodeByOutput(t, g, "$(B)/protos/public.pb.h")
	leafPB := mustNodeByOutput(t, g, "$(B)/protos/leaf.pb.h")

	for _, want := range []string{
		"$(B)/protos/main.pb.h",
		"$(B)/protos/public.pb.h",
		"$(B)/protos/leaf.pb.h",
	} {
		if !nodeHasInput(useCC, want) {
			t.Fatalf("use.cpp.o inputs missing %q: %#v", want, useCC.Inputs)
		}
	}
	for _, want := range []string{mainPB.UID, publicPB.UID, leafPB.UID} {
		if !containsString(useCC.Deps, want) {
			t.Fatalf("use.cpp.o deps missing %q: %v", want, useCC.Deps)
		}
	}
}

func testGen(sourceRoot, targetDir string) *Graph {
	host := newTestPlatform(OSLinux, ISAX8664, "yes", []string{"tool"})
	targetFlags := make(map[string]string, len(testToolchainFlags)+1)
	for k, v := range testToolchainFlags {
		targetFlags[k] = v
	}
	targetFlags["PIC"] = "no"
	target := NewPlatform(OSLinux, ISAAArch64, targetFlags, nil, "", "")
	return Gen(sourceRoot, targetDir, host, target, func(Warn) {})
}

func TestCollectModule_YqlAbiMacrosAppendCXXFlags(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "mod")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir mod: %v", err)
	}

	const yamake = `LIBRARY()
YQL_LAST_ABI_VERSION()
YQL_ABI_VERSION(2 44 0)
SRCS(lib.cpp)
END()
`
	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), []byte(yamake), 0o644); err != nil {
		t.Fatalf("write ya.make: %v", err)
	}

	fs := NewFS(root)
	mf := Throw2(ParseFile(fs, filepath.Join(modDir, "ya.make")))
	d := collectModule(newIncludeParserManagerFS(fs, newSharedParseCache()), "mod", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: "mod", Kind: KindLib, Platform: testTargetP}))

	want := []string{
		"-DUSE_CURRENT_UDF_ABI_VERSION",
		"-DUDF_ABI_VERSION_MAJOR=2",
		"-DUDF_ABI_VERSION_MINOR=44",
		"-DUDF_ABI_VERSION_PATCH=0",
	}
	if !reflect.DeepEqual(d.cxxFlags, want) {
		t.Fatalf("cxxFlags = %#v, want %#v", d.cxxFlags, want)
	}
}

func TestCollectModule_YqlUdfStaticRoutesSrcsToGlobal(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "mod")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir mod: %v", err)
	}

	const yamake = `YQL_UDF_CONTRIB(my_udf)
SRCS(lib.cpp nested/extra.cpp)
PEERDIR(custom/peer)
END()
`
	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), []byte(yamake), 0o644); err != nil {
		t.Fatalf("write ya.make: %v", err)
	}

	fs := NewFS(root)
	mf := Throw2(ParseFile(fs, filepath.Join(modDir, "ya.make")))
	d := collectModule(newIncludeParserManagerFS(fs, newSharedParseCache()), "mod", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: "mod", Kind: KindLib, Platform: testTargetP}))

	if d.moduleStmt == nil || d.moduleStmt.Name != "YQL_UDF_CONTRIB" {
		t.Fatalf("moduleStmt = %#v, want YQL_UDF_CONTRIB", d.moduleStmt)
	}
	if !equalStrings(d.moduleStmt.Args, []string{"my_udf"}) {
		t.Fatalf("module args = %v, want [my_udf]", d.moduleStmt.Args)
	}
	if len(d.srcs) != 0 {
		t.Fatalf("srcs = %v, want empty (SRCS must alias to GLOBAL_SRCS)", d.srcs)
	}
	if !equalStrings(d.globalSrcs, []string{"lib.cpp", "nested/extra.cpp"}) {
		t.Fatalf("globalSrcs = %v, want [lib.cpp nested/extra.cpp]", d.globalSrcs)
	}
	if !equalStrings(d.peerdirs, []string{
		"yql/essentials/public/udf",
		"yql/essentials/public/udf/support",
		"custom/peer",
	}) {
		t.Fatalf("peerdirs = %v", d.peerdirs)
	}
}

func TestCollectModule_ProtocFatalWarningsAddsProtoFlag(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "proto")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir proto: %v", err)
	}

	const yamake = `PROTO_LIBRARY()
PROTOC_FATAL_WARNINGS()
SRCS(test.proto)
END()
`
	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), []byte(yamake), 0o644); err != nil {
		t.Fatalf("write ya.make: %v", err)
	}

	fs := NewFS(root)
	mf := Throw2(ParseFile(fs, filepath.Join(modDir, "ya.make")))
	d := collectModule(newIncludeParserManagerFS(fs, newSharedParseCache()), "proto", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: "proto", Kind: KindLib, Platform: testTargetP}))

	if !equalStrings(d.protocFlags, []string{"--fatal_warnings"}) {
		t.Fatalf("protocFlags = %v, want [--fatal_warnings]", d.protocFlags)
	}
}

func TestCollectModule_CPPProtoPluginRecorded(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "proto")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir proto: %v", err)
	}

	const yamake = `PROTO_LIBRARY()
CPP_PROTO_PLUGIN(validation ydb/public/lib/validation .validation.pb.h DEPS ydb/public/api/protos/annotations EXTRA_OUT_FLAG lite=true)
SRCS(test.proto)
END()
`
	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), []byte(yamake), 0o644); err != nil {
		t.Fatalf("write ya.make: %v", err)
	}

	fs := NewFS(root)
	mf := Throw2(ParseFile(fs, filepath.Join(modDir, "ya.make")))
	d := collectModule(newIncludeParserManagerFS(fs, newSharedParseCache()), "proto", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: "proto", Kind: KindLib, Platform: testTargetP}))

	if len(d.cppProtoPlugins) != 1 {
		t.Fatalf("cppProtoPlugins = %d, want 1", len(d.cppProtoPlugins))
	}

	plugin := d.cppProtoPlugins[0]
	if plugin.Name != "validation" {
		t.Fatalf("plugin.Name = %q, want validation", plugin.Name)
	}
	if plugin.ToolPath != "ydb/public/lib/validation" {
		t.Fatalf("plugin.ToolPath = %q, want ydb/public/lib/validation", plugin.ToolPath)
	}
	if !equalStrings(plugin.OutputSuffixes, []string{".validation.pb.h"}) {
		t.Fatalf("plugin.OutputSuffixes = %v, want [.validation.pb.h]", plugin.OutputSuffixes)
	}
	if !equalStrings(plugin.Deps, []string{"ydb/public/api/protos/annotations"}) {
		t.Fatalf("plugin.Deps = %v, want [ydb/public/api/protos/annotations]", plugin.Deps)
	}
	if plugin.ExtraOutFlag != "lite=true" {
		t.Fatalf("plugin.ExtraOutFlag = %q, want lite=true", plugin.ExtraOutFlag)
	}
	if !containsString(d.peerdirs, "ydb/public/api/protos/annotations") {
		t.Fatalf("peerdirs = %v, want ydb/public/api/protos/annotations", d.peerdirs)
	}
}

func TestCollectModule_FlatcFlagsRecorded(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "flatcmod")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir flatcmod: %v", err)
	}

	const yamake = `LIBRARY()
FLATC_FLAGS(--scoped-enums --gen-all)
SRCS(Schema.fbs)
END()
`
	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), []byte(yamake), 0o644); err != nil {
		t.Fatalf("write ya.make: %v", err)
	}

	fs := NewFS(root)
	mf := Throw2(ParseFile(fs, filepath.Join(modDir, "ya.make")))
	d := collectModule(newIncludeParserManagerFS(fs, newSharedParseCache()), "flatcmod", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: "flatcmod", Kind: KindLib, Platform: testTargetP}))

	if !equalStrings(d.flatcFlags, []string{"--scoped-enums", "--gen-all"}) {
		t.Fatalf("flatcFlags = %v, want [--scoped-enums --gen-all]", d.flatcFlags)
	}
}

func TestCollectModule_UseCommonGoogleApisAddsPeer(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "proto")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir proto: %v", err)
	}

	const yamake = `PROTO_LIBRARY()
USE_COMMON_GOOGLE_APIS(api/annotations)
SRCS(test.proto)
END()
`
	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), []byte(yamake), 0o644); err != nil {
		t.Fatalf("write ya.make: %v", err)
	}

	fs := NewFS(root)
	mf := Throw2(ParseFile(fs, filepath.Join(modDir, "ya.make")))
	d := collectModule(newIncludeParserManagerFS(fs, newSharedParseCache()), "proto", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: "proto", Kind: KindLib, Platform: testTargetP}))

	if !containsString(d.peerdirs, "contrib/libs/googleapis-common-protos") {
		t.Fatalf("peerdirs = %v, want contrib/libs/googleapis-common-protos", d.peerdirs)
	}
}

func TestCollectModule_Py3ProgramSplitsPyMainFromPySrcs(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "pytool")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir pytool: %v", err)
	}

	const yamake = `PY3_PROGRAM()
PY_SRCS(
    MAIN
    __main__.py
)
END()
`
	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), []byte(yamake), 0o644); err != nil {
		t.Fatalf("write ya.make: %v", err)
	}

	fs := NewFS(root)
	mf := Throw2(ParseFile(fs, filepath.Join(modDir, "ya.make")))

	bin := collectModule(newIncludeParserManagerFS(fs, newSharedParseCache()), "pytool", KindBin, mf.Stmts, buildIfEnv(ModuleInstance{Path: "pytool", Kind: KindBin, Platform: testTargetP}))
	if got := bin.pyMain; got == nil || *got != "pytool.__main__:main" {
		t.Fatalf("bin pyMain = %#v, want pytool.__main__:main", got)
	}
	if len(bin.pySrcs) != 0 {
		t.Fatalf("bin pySrcs = %v, want empty", bin.pySrcs)
	}

	lib := collectModule(newIncludeParserManagerFS(fs, newSharedParseCache()), "pytool", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: "pytool", Kind: KindLib, Platform: testTargetP}))
	if lib.pyMain != nil {
		t.Fatalf("lib pyMain = %#v, want nil", lib.pyMain)
	}
	if !equalStrings(lib.pySrcs, []string{"__main__.py"}) {
		t.Fatalf("lib pySrcs = %v, want [__main__.py]", lib.pySrcs)
	}
}

func TestCollectModule_CopyExpandsVarsIntoAutoSources(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "copymod")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir copymod: %v", err)
	}

	const yamake = `LIBRARY()
SET(ORIG_SRC_DIR src)
SET(ORIG_SOURCES a.cpp b.h)
COPY(
    WITH_CONTEXT
    AUTO
    FROM ${ORIG_SRC_DIR}
    ${ORIG_SOURCES}
    OUTPUT_INCLUDES dep.h
)
END()
`
	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), []byte(yamake), 0o644); err != nil {
		t.Fatalf("write ya.make: %v", err)
	}

	fs := NewFS(root)
	mf := Throw2(ParseFile(fs, filepath.Join(modDir, "ya.make")))
	d := collectModule(newIncludeParserManagerFS(fs, newSharedParseCache()), "copymod", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: "copymod", Kind: KindLib, Platform: testTargetP}))

	if !equalStrings(d.srcs, []string{"a.cpp", "b.h"}) {
		t.Fatalf("srcs = %v, want [a.cpp b.h]", d.srcs)
	}
	if len(d.copyFiles) != 2 {
		t.Fatalf("len(copyFiles) = %d, want 2", len(d.copyFiles))
	}
	if d.copyFiles[0].Src != "src/a.cpp" || d.copyFiles[1].Src != "src/b.h" {
		t.Fatalf("copyFiles srcs = %#v", d.copyFiles)
	}
}

func TestGen_YqlUdfStatic_UsesGlobalArchiveOnly(t *testing.T) {
	root := t.TempDir()

	mkdirWrite := func(rel, body string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(rel), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	mkdirWrite("udfmod/ya.make", `YQL_UDF_CONTRIB(my_udf)
YQL_ABI_VERSION(2 44 0)
SRCS(lib.cpp)
END()
`)
	mkdirWrite("udfmod/lib.cpp", "int udf() { return 0; }\n")
	mkdirWrite("yql/essentials/public/udf/ya.make", "LIBRARY()\nEND()\n")
	mkdirWrite("yql/essentials/public/udf/support/ya.make", "LIBRARY()\nEND()\n")

	g := testGen(root, "udfmod")

	cc := findGraphNodeByOutputs(t, g, "$(B)/udfmod/lib.cpp.udfs.o")
	if cc.TargetProperties["module_tag"] != "yql_udf_static" {
		t.Fatalf("cc module_tag = %q, want yql_udf_static", cc.TargetProperties["module_tag"])
	}

	for _, want := range []string{
		"-DUDF_ABI_VERSION_MAJOR=2",
		"-DUDF_ABI_VERSION_MINOR=44",
		"-DUDF_ABI_VERSION_PATCH=0",
	} {
		if !contains(cc.Cmds[0].CmdArgs, want) {
			t.Fatalf("cc cmd_args missing %q: %v", want, cc.Cmds[0].CmdArgs)
		}
	}

	globalAR := findGraphNodeByOutputs(t, g, "$(B)/udfmod/libmy_udf.global.a")
	if globalAR.TargetProperties["module_tag"] != "yql_udf_static_global" {
		t.Fatalf("global AR module_tag = %q, want yql_udf_static_global", globalAR.TargetProperties["module_tag"])
	}

	for _, n := range g.Graph {
		for _, out := range n.Outputs {
			if out.String() == "$(B)/udfmod/libmy_udf.a" {
				t.Fatalf("unexpected regular archive output %q present in graph", out)
			}
		}
	}
}

func TestGen_FlatcSourcesEmitConsumerInputsAndDeps(t *testing.T) {
	root := t.TempDir()

	mkdirWrite := func(rel, body string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(rel), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	mkdirWrite("mod/ya.make", `LIBRARY()
FLATC_FLAGS(--scoped-enums)
SRCS(
    File.fbs
    Schema.fbs
    consumer.cpp
)
END()
`)
	mkdirWrite("mod/consumer.cpp", `#include "File.fbs.h"
int consume() { return 0; }
`)
	mkdirWrite("mod/Schema.fbs", `namespace test;
table Foo {
  value:int;
}
`)
	mkdirWrite("mod/File.fbs", `include "Schema.fbs";
namespace test;
table Bar {
  foo:Foo;
}
root_type Bar;
`)
	mkdirWrite("build/scripts/cpp_flatc_wrapper.py", "print('stub')\n")
	mkdirWrite("contrib/libs/flatbuffers/include/flatbuffers/flatbuffers.h", "#pragma once\n")
	mkdirWrite("contrib/libs/flatbuffers/flatc/ya.make", "PROGRAM(flatc)\nSRCS(main.cpp)\nEND()\n")
	mkdirWrite("contrib/libs/flatbuffers/flatc/main.cpp", "int main() { return 0; }\n")

	g := testGen(root, "mod")

	findGraphNodeByOutputs(t, g, "$(B)/mod/File.fbs.h", "$(B)/mod/File.fbs.cpp", "$(B)/mod/File.bfbs")
	findGraphNodeByOutputs(t, g, "$(B)/mod/Schema.fbs.h", "$(B)/mod/Schema.fbs.cpp", "$(B)/mod/Schema.bfbs")

	fileCC := findGraphNodeByOutputs(t, g, "$(B)/mod/File.fbs.cpp.o")
	wantFileInputs := []string{
		"$(B)/mod/File.fbs.cpp",
		"$(B)/mod/File.fbs.h",
		"$(B)/mod/Schema.fbs.h",
		"$(S)/build/scripts/cpp_flatc_wrapper.py",
		"$(S)/mod/File.fbs",
		"$(S)/mod/Schema.fbs",
		"$(S)/contrib/libs/flatbuffers/include/flatbuffers/flatbuffers.h",
	}
	if got := vfsStringsT3(fileCC.Inputs); !reflect.DeepEqual(got[:len(wantFileInputs)], wantFileInputs) {
		t.Fatalf("File.fbs.cpp inputs prefix = %v, want %v", got[:len(wantFileInputs)], wantFileInputs)
	}
	if len(fileCC.Deps) != 2 {
		t.Fatalf("len(File.fbs.cpp deps) = %d, want 2 (self + imported schema)", len(fileCC.Deps))
	}

	consumerCC := findGraphNodeByOutputs(t, g, "$(B)/mod/consumer.cpp.o")
	wantConsumerInputs := []string{
		"$(S)/mod/consumer.cpp",
		"$(B)/mod/File.fbs.h",
		"$(B)/mod/Schema.fbs.h",
		"$(S)/build/scripts/cpp_flatc_wrapper.py",
		"$(S)/mod/File.fbs",
		"$(S)/mod/Schema.fbs",
		"$(S)/contrib/libs/flatbuffers/include/flatbuffers/flatbuffers.h",
	}
	if got := vfsStringsT3(consumerCC.Inputs); !reflect.DeepEqual(got[:len(wantConsumerInputs)], wantConsumerInputs) {
		t.Fatalf("consumer.cpp inputs prefix = %v, want %v", got[:len(wantConsumerInputs)], wantConsumerInputs)
	}
	if len(consumerCC.Deps) != 2 {
		t.Fatalf("len(consumer.cpp deps) = %d, want 2 (reachable flatc producers)", len(consumerCC.Deps))
	}
}

func TestGen_CopyFileWithContextAutoCompilesBuildOutput(t *testing.T) {
	root := t.TempDir()

	mkdirWrite := func(rel, body string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(rel), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	mkdirWrite("mod/ya.make", `LIBRARY()
COPY_FILE_WITH_CONTEXT(
    AUTO
    original.cpp
    copied.cpp
)
END()
`)
	mkdirWrite("mod/original.cpp", `#include "dep.h"
int copied() { return 0; }
`)
	mkdirWrite("mod/dep.h", "#pragma once\n")

	g := testGen(root, "mod")

	findGraphNodeByOutputs(t, g, "$(B)/mod/copied.cpp")
	cc := findGraphNodeByOutputs(t, g, "$(B)/mod/copied.cpp.o")
	wantInputs := []string{
		"$(B)/mod/copied.cpp",
		"$(S)/mod/original.cpp",
		"$(S)/mod/dep.h",
	}
	if got := vfsStringsT3(cc.Inputs); !reflect.DeepEqual(got[:len(wantInputs)], wantInputs) {
		t.Fatalf("copied.cpp inputs prefix = %v, want %v", got[:len(wantInputs)], wantInputs)
	}
	if len(cc.Deps) != 1 {
		t.Fatalf("len(copied.cpp deps) = %d, want 1 (copy producer)", len(cc.Deps))
	}
}

func TestGen_CopyFileWithContextExpandsBuildRootModdirDestination(t *testing.T) {
	root := t.TempDir()

	mkdirWrite := func(rel, body string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(rel), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	mkdirWrite("mod/ya.make", `LIBRARY()
COPY_FILE_WITH_CONTEXT(
    AUTO
    original.cpp
    ${ARCADIA_BUILD_ROOT}/${MODDIR}/copied.cpp
)
END()
`)
	mkdirWrite("mod/original.cpp", `#include "dep.h"
int copied() { return 0; }
`)
	mkdirWrite("mod/dep.h", "#pragma once\n")

	g := testGen(root, "mod")

	findGraphNodeByOutputs(t, g, "$(B)/mod/copied.cpp")
	cc := findGraphNodeByOutputs(t, g, "$(B)/mod/copied.cpp.o")
	wantInputs := []string{
		"$(B)/mod/copied.cpp",
		"$(S)/mod/original.cpp",
		"$(S)/mod/dep.h",
	}
	if got := vfsStringsT3(cc.Inputs); !reflect.DeepEqual(got[:len(wantInputs)], wantInputs) {
		t.Fatalf("copied.cpp inputs prefix = %v, want %v", got[:len(wantInputs)], wantInputs)
	}
}

func TestGen_CopyFileAutoDoesNotPropagateSourceContext(t *testing.T) {
	root := t.TempDir()

	mkdirWrite := func(rel, body string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(rel), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	mkdirWrite("mod/ya.make", `LIBRARY()
COPY_FILE(
    AUTO
    original.cpp
    copied.cpp
)
END()
`)
	mkdirWrite("mod/original.cpp", `#include "dep.h"
int copied() { return 0; }
`)
	mkdirWrite("mod/dep.h", "#pragma once\n")

	g := testGen(root, "mod")

	findGraphNodeByOutputs(t, g, "$(B)/mod/copied.cpp")
	cc := findGraphNodeByOutputs(t, g, "$(B)/mod/copied.cpp.o")
	wantInputs := []string{"$(B)/mod/copied.cpp"}
	if got := vfsStringsT3(cc.Inputs); !reflect.DeepEqual(got[:len(wantInputs)], wantInputs) {
		t.Fatalf("copied.cpp inputs prefix = %v, want %v", got[:len(wantInputs)], wantInputs)
	}
	for _, unexpected := range []string{"$(S)/mod/original.cpp", "$(S)/mod/dep.h"} {
		for _, in := range vfsStringsT3(cc.Inputs) {
			if in == unexpected {
				t.Fatalf("copied.cpp inputs unexpectedly contain %s: %v", unexpected, vfsStringsT3(cc.Inputs))
			}
		}
	}
	if len(cc.Deps) != 1 {
		t.Fatalf("len(copied.cpp deps) = %d, want 1 (copy producer)", len(cc.Deps))
	}
}

func TestGen_CopyFileUsesSourceRootInputFromIncludedMacro(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "mod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
INCLUDE(${ARCADIA_ROOT}/shared/copy.ya.make.inc)
END()
`)
	writeTestModuleFile(t, root, "shared/copy.ya.make.inc", `COPY_FILE(
    TEXT
    shared/generated.txt
    ${BINDIR}/shared/generated.h
)
`)
	writeTestModuleFile(t, root, "shared/generated.txt", "generated\n")

	g := testGen(root, "mod")

	cp := mustNodeByOutput(t, g, "$(B)/mod/shared/generated.h")
	if !nodeHasInput(cp, "$(S)/shared/generated.txt") {
		t.Fatalf("copy inputs missing source-root generated.txt: %#v", cp.Inputs)
	}
	if nodeHasInput(cp, "$(S)/mod/shared/generated.txt") {
		t.Fatalf("copy inputs still carry duplicated module-prefixed generated.txt: %#v", cp.Inputs)
	}
}

func TestGen_EnumSerializationRootQualifiedHeaderUsesCanonicalInput(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "pkg/sub/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
GENERATE_ENUM_SERIALIZATION(pkg/sub/codecs.h)
SRCS(stub.cpp)
END()
`)
	writeTestModuleFile(t, root, "pkg/sub/codecs.h", "enum class E { A = 0 };\n")
	writeTestModuleFile(t, root, "pkg/sub/stub.cpp", "int stub(){return 0;}\n")
	writeToolProgram(t, root, "tools/enum_parser/enum_parser", "enum_parser")
	writeTestModuleFile(t, root, "tools/enum_parser/enum_serialization_runtime/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(runtime.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "tools/enum_parser/enum_serialization_runtime/runtime.cpp", "int runtime(){return 0;}\n")

	g := testGen(root, "pkg/sub")

	en := mustNodeByOutput(t, g, "$(B)/pkg/sub/pkg/sub/codecs.h_serialized.cpp")
	if !nodeHasInput(en, "$(S)/pkg/sub/codecs.h") {
		t.Fatalf("enum inputs missing canonical header path: %#v", en.Inputs)
	}
	if nodeHasInput(en, "$(S)/pkg/sub/pkg/sub/codecs.h") {
		t.Fatalf("enum inputs still carry duplicated header path: %#v", en.Inputs)
	}
	if got := en.Cmds[0].CmdArgs[1]; got != "$(S)/pkg/sub/codecs.h" {
		t.Fatalf("enum parser input = %q, want $(S)/pkg/sub/codecs.h", got)
	}
	if idx := indexOfArg(en.Cmds[0].CmdArgs, "--include-path"); idx < 0 || idx+1 >= len(en.Cmds[0].CmdArgs) || en.Cmds[0].CmdArgs[idx+1] != "pkg/sub/codecs.h" {
		t.Fatalf("enum --include-path mismatch: %#v", en.Cmds[0].CmdArgs)
	}
}

func vfsStringsT3(in []VFS) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = v.String()
	}
	return out
}

func TestGen_CF_SetVarsReachCfgVars(t *testing.T) {
	root := t.TempDir()
	libDir := filepath.Join(root, "thelib")
	Throw(os.MkdirAll(libDir, 0o755))
	Throw(os.WriteFile(filepath.Join(libDir, "ya.make"),
		[]byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSET(MYVAR hello)\nDEFAULT(MYDEF world)\nSRCS(lib.cpp x.cpp.in)\nEND()\n"), 0o644))
	Throw(os.WriteFile(filepath.Join(libDir, "lib.cpp"), []byte("int f(){return 0;}\n"), 0o644))
	Throw(os.WriteFile(filepath.Join(libDir, "x.cpp.in"), []byte("int a = @MYVAR@;\nint b = @MYDEF@;\n"), 0o644))

	g := testGen(root, "thelib")
	var cf *Node
	for _, n := range g.Graph {
		if len(n.Outputs) > 0 && n.Outputs[0].String() == "$(B)/thelib/x.cpp" {
			cf = n
			break
		}
	}
	if cf == nil {
		t.Fatal("no CF node emitted for thelib/x.cpp")
	}
	args := strings.Join(cf.Cmds[0].CmdArgs, " ")
	if !strings.Contains(args, "MYVAR=hello") {
		t.Errorf("CF cmd_args missing SET var MYVAR=hello; got: %s", args)
	}
	if !strings.Contains(args, "MYDEF=world") {
		t.Errorf("CF cmd_args missing DEFAULT var MYDEF=world; got: %s", args)
	}
}

func TestGen_HInGeneratedHeader_RealizedInConsumer(t *testing.T) {
	root := t.TempDir()
	genh := filepath.Join(root, "genh")
	cons := filepath.Join(root, "cons")
	app := filepath.Join(root, "app")
	for _, d := range []string{genh, cons, app} {
		Throw(os.MkdirAll(d, 0o755))
	}

	Throw(os.WriteFile(filepath.Join(genh, "ya.make"),
		[]byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSET(MYVAR hello)\nSRCS(config.h.in own.cpp)\nEND()\n"), 0o644))
	Throw(os.WriteFile(filepath.Join(genh, "config.h.in"), []byte("#include \"dep.h\"\n#define X @MYVAR@\n"), 0o644))
	Throw(os.WriteFile(filepath.Join(genh, "dep.h"), []byte("#pragma once\n"), 0o644))
	Throw(os.WriteFile(filepath.Join(genh, "own.cpp"), []byte("int g(){return 0;}\n"), 0o644))

	Throw(os.WriteFile(filepath.Join(cons, "ya.make"),
		[]byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nPEERDIR(genh)\nSRCS(use.cpp)\nEND()\n"), 0o644))
	Throw(os.WriteFile(filepath.Join(cons, "use.cpp"),
		[]byte("#include <genh/config.h>\nint u(){return 0;}\n"), 0o644))

	Throw(os.WriteFile(filepath.Join(app, "ya.make"),
		[]byte("PROGRAM()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nPEERDIR(cons)\nSRCS(main.cpp)\nEND()\n"), 0o644))
	Throw(os.WriteFile(filepath.Join(app, "main.cpp"), []byte("int main(){return 0;}\n"), 0o644))

	g := testGen(root, "app")

	byOut := map[string]*Node{}
	for _, n := range g.Graph {
		if len(n.Outputs) > 0 {
			byOut[n.Outputs[0].String()] = n
		}
	}

	cf := byOut["$(B)/genh/config.h"]
	if cf == nil {
		t.Fatal("no CF node emitted for genh/config.h")
	}
	if got := cf.TargetProperties["module_dir"]; got != "cons" {
		t.Errorf("config.h module_dir = %q, want %q (consuming module)", got, "cons")
	}

	ar := byOut["$(B)/genh/libgenh.a"]
	if ar == nil {
		t.Fatal("no AR node for genh")
	}
	for _, c := range ar.Cmds {
		for _, a := range c.CmdArgs {
			if a == "$(B)/genh/config.h" {
				t.Errorf("genh AR cmd_args archives config.h as a member: %v", c.CmdArgs)
			}
		}
	}
	for _, in := range ar.Inputs {
		if in.String() == "$(B)/genh/config.h" || in.String() == "$(S)/genh/config.h.in" {
			t.Errorf("genh AR inputs include %q (generated header must not be archived)", in.String())
		}
	}

	use := byOut["$(B)/cons/use.cpp.o"]
	if use == nil {
		t.Fatal("no CC node for cons/use.cpp")
	}
	found := false
	for _, d := range use.Deps {
		if d == cf.UID {
			found = true
		}
	}
	if !found {
		t.Errorf("use.cpp.o deps %v missing config.h CF uid %q", use.Deps, cf.UID)
	}
	if !nodeHasInput(use, "$(S)/genh/config.h.in") {
		t.Errorf("use.cpp.o inputs missing config.h.in: %#v", use.Inputs)
	}
	if !nodeHasInput(use, "$(S)/genh/dep.h") {
		t.Errorf("use.cpp.o inputs missing dep.h from config.h.in closure: %#v", use.Inputs)
	}
}

func TestGen_CmdArgsExpandStmtVars(t *testing.T) {
	root := t.TempDir()
	mod := filepath.Join(root, "mod")
	Throw(os.MkdirAll(mod, 0o755))
	Throw(os.WriteFile(filepath.Join(mod, "ya.make"), []byte(`LIBRARY()
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
`), 0o644))
	Throw(os.WriteFile(filepath.Join(mod, "lib.cpp"), []byte("int lib(){return 0;}\n"), 0o644))

	g := testGen(root, "mod")
	cc := mustNodeByOutput(t, g, "$(B)/mod/lib.cpp.o")
	args := strings.Join(cc.Cmds[0].CmdArgs, " ")

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

func TestGen_RunProgramHeaderOutputClosurePropagatesInputs(t *testing.T) {
	root := t.TempDir()

	writeToolProgram(t, root, "tools/genhdr", "genhdr")

	writeTestModuleFile(t, root, "dep/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(dep.cpp dep.h)
END()
`)
	writeTestModuleFile(t, root, "dep/dep.cpp", "int dep(){return 0;}\n")
	writeTestModuleFile(t, root, "dep/dep.h", "#pragma once\n")

	writeTestModuleFile(t, root, "gen/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(dep)
RUN_PROGRAM(
    tools/genhdr
        template.h.in
        gen.h
    OUTPUT_INCLUDES
        dep/dep.h
    IN
        template.h.in
    OUT
        gen.h
)
END()
`)
	writeTestModuleFile(t, root, "gen/template.h.in", "#pragma once\n")

	writeTestModuleFile(t, root, "cons/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(gen)
SRCS(use.cpp)
END()
`)
	writeTestModuleFile(t, root, "cons/use.cpp", `#include <gen/gen.h>
int use() { return 0; }
`)

	writeTestModuleFile(t, root, "app/ya.make", `PROGRAM()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(cons)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(t, root, "app/main.cpp", "int main(){return 0;}\n")

	g := testGen(root, "app")
	genH := mustNodeByOutput(t, g, "$(B)/gen/gen.h")
	use := mustNodeByOutput(t, g, "$(B)/cons/use.cpp.o")

	for _, want := range []string{
		"$(B)/gen/gen.h",
		"$(S)/gen/template.h.in",
		"$(S)/dep/dep.h",
	} {
		if !nodeHasInput(use, want) {
			t.Fatalf("use.cpp.o inputs missing %q: %#v", want, use.Inputs)
		}
	}
	if !containsString(use.Deps, genH.UID) {
		t.Fatalf("use.cpp.o deps missing generated-header PR uid %q: %v", genH.UID, use.Deps)
	}
}

func TestCollectModule_BisonGeneratedHeaderExportedGlobally(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "gen/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(pire/re_parser.y)
END()
`)
	writeTestModuleFile(t, root, "gen/pire/re_parser.y", `%{
#include "re_lexer.h"
%}
%%
`)
	writeTestModuleFile(t, root, "gen/pire/re_lexer.h", "#pragma once\n")

	fs := NewFS(root)
	mf := Throw2(ParseFile(fs, filepath.Join(root, "gen", "ya.make")))
	instance := ModuleInstance{Path: "gen", Kind: KindLib, Platform: testTargetP}
	d := collectModule(newIncludeParserManagerFS(fs, newSharedParseCache()), "gen", KindLib, mf.Stmts, buildIfEnv(instance))

	for _, got := range [][]VFS{d.addIncl, d.addInclGlobal} {
		if !slices.Contains(vfsStrings(got), "$(B)/gen/pire") {
			t.Fatalf("generated bison include dir missing from %v", vfsStrings(got))
		}
	}
}

func TestGen_BisonGeneratedHeaderPreprocessAndPeerBuildRootInclude(t *testing.T) {
	root := t.TempDir()

	writeToolProgram(t, root, "contrib/tools/bison", "bison")
	writeToolProgram(t, root, "contrib/tools/m4", "m4")
	writeTestModuleFile(t, root, bisonPreprocessPyVFS.Rel(), "print('stub')\n")
	for _, input := range bisonCppSkeletonInputs {
		body := ""
		if strings.HasSuffix(input.Rel(), "/stack.hh") {
			body = `#include "skeleton-helper.h"` + "\n"
		}
		writeTestModuleFile(t, root, input.Rel(), body)
	}
	writeTestModuleFile(t, root, "contrib/tools/bison/data/skeletons/skeleton-helper.h", "")

	writeTestModuleFile(t, root, "genlib/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(pire/re_parser.y)
END()
`)
	writeTestModuleFile(t, root, "genlib/pire/re_parser.y", `%{
#include "re_lexer.h"
#include "extra.h"
%}
%%
`)
	writeTestModuleFile(t, root, "genlib/pire/re_lexer.h", `#pragma once
#include "deep.h"
`)
	writeTestModuleFile(t, root, "genlib/pire/extra.h", "#pragma once\n")
	writeTestModuleFile(t, root, "genlib/pire/deep.h", "#pragma once\n")

	writeTestModuleFile(t, root, "app/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(genlib)
SRCS(use.cpp)
END()
`)
	writeTestModuleFile(t, root, "app/use.cpp", "int use() { return 0; }\n")

	g := testGen(root, "app")

	yc := mustNodeByOutput(t, g, "$(B)/genlib/pire/re_parser.h")
	if got := len(yc.Cmds); got != 2 {
		t.Fatalf("bison YC cmd count = %d, want 2", got)
	}
	if !strings.HasSuffix(yc.Cmds[1].CmdArgs[0], "/python3") {
		t.Fatalf("bison preprocess tool = %q, want a python3 binary", yc.Cmds[1].CmdArgs[0])
	}
	wantPreprocess := []string{
		"$(S)/build/scripts/preprocess.py",
		"$(B)/genlib/pire/re_parser.h",
	}
	if got := yc.Cmds[1].CmdArgs[1:]; !reflect.DeepEqual(got, wantPreprocess) {
		t.Fatalf("bison preprocess cmd_args mismatch:\n  got:  %#v\n  want: %#v", got, wantPreprocess)
	}
	for _, want := range []string{
		"$(S)/build/scripts/preprocess.py",
		"$(S)/genlib/pire/re_parser.y",
		"$(S)/contrib/tools/bison/data/skeletons/skeleton-helper.h",
	} {
		if !nodeHasInput(yc, want) {
			t.Fatalf("bison YC inputs missing %q: %#v", want, yc.Inputs)
		}
	}
	for _, unwanted := range []string{
		"$(S)/genlib/pire/re_lexer.h",
		"$(S)/genlib/pire/extra.h",
		"$(S)/genlib/pire/deep.h",
	} {
		if nodeHasInput(yc, unwanted) {
			t.Fatalf("bison YC inputs unexpectedly include grammar-local header %q: %#v", unwanted, yc.Inputs)
		}
	}
	for _, want := range vfsStrings(bisonCppSkeletonInputs) {
		if !nodeHasInput(yc, want) {
			t.Fatalf("bison YC inputs missing skeleton %q", want)
		}
	}

	use := mustNodeByOutput(t, g, "$(B)/app/use.cpp.o")
	if indexOfArg(use.Cmds[0].CmdArgs, "-I$(B)/genlib/pire") < 0 {
		t.Fatalf("peer CC cmd_args missing generated bison build-root addincl: %#v", use.Cmds[0].CmdArgs)
	}

	parserObj := mustNodeByOutput(t, g, "$(B)/genlib/_/_/pire/re_parser.y.cpp.o")
	for _, want := range []string{
		"$(S)/build/scripts/preprocess.py",
		"$(S)/genlib/pire/re_lexer.h",
		"$(S)/genlib/pire/extra.h",
		"$(S)/genlib/pire/deep.h",
		"$(S)/contrib/tools/bison/data/skeletons/skeleton-helper.h",
	} {
		if !nodeHasInput(parserObj, want) {
			t.Fatalf("generated parser object inputs missing %q: %#v", want, parserObj.Inputs)
		}
	}
}

func TestGen_ProtoLibrary_TransitiveHeadersNo_DepsHeaderUsesRuntimeRoot(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "protos/ya.make", `PROTO_LIBRARY()
SET(PROTOC_TRANSITIVE_HEADERS "no")
SRCS(test.proto)
END()
`)
	writeTestModuleFile(t, root, "protos/test.proto", `syntax = "proto3";
package test;
import "google/protobuf/any.proto";
message Row {
  google.protobuf.Any body = 1;
}
`)
	writeTestModuleFile(t, root, "app/ya.make", `LIBRARY()
PEERDIR(protos)
SRCS(use.cpp)
END()
`)
	writeTestModuleFile(t, root, "app/use.cpp", `#include <protos/test.deps.pb.h>
int use() { return 0; }
`)

	writeToolProgram(t, root, "contrib/tools/protoc", "protoc")
	writeToolProgram(t, root, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(t, root, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/src/google/protobuf/any.proto", `syntax = "proto3";
package google.protobuf;
message Any {}
`)
	writeTestModuleFile(t, root, "contrib/libs/protobuf/src/google/protobuf/any.pb.h", "#pragma once\n")

	g := testGen(root, "app")
	pb := findGraphNodeByOutputs(t, g,
		"$(B)/protos/test.pb.h",
		"$(B)/protos/test.pb.cc",
		"$(B)/protos/test.deps.pb.h",
	)
	use := mustNodeByOutput(t, g, "$(B)/app/use.cpp.o")

	if !nodeHasInput(use, "$(B)/protos/test.deps.pb.h") {
		t.Fatalf("use.cpp.o inputs missing deps header output: %#v", use.Inputs)
	}
	if !nodeHasInput(use, "$(S)/contrib/libs/protobuf/src/google/protobuf/any.pb.h") {
		t.Fatalf("use.cpp.o inputs missing protobuf runtime WKT header: %#v", use.Inputs)
	}
	if nodeHasInput(use, "$(S)/google/protobuf/any.pb.h") {
		t.Fatalf("use.cpp.o inputs still contain unrebased WKT header path: %#v", use.Inputs)
	}
	if !containsString(use.Deps, pb.UID) {
		t.Fatalf("use.cpp.o deps missing PB producer uid %q: %v", pb.UID, use.Deps)
	}
}

func TestReorderARMembers_Reg3PICVariantsTrailObjcopy(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		paths     []string
		wantOrder []int
	}{
		{
			name: "protobuf-style host reg3",
			paths: []string{
				"contrib/python/protobuf/py3/google.protobuf.internal._api_implementation.reg3.cpp.pic.o",
				"contrib/python/protobuf/py3/google.protobuf.pyext._message.reg3.cpp.pic.o",
				"contrib/python/protobuf/py3/objcopy_a.o",
				"contrib/python/protobuf/py3/objcopy_b.o",
			},
			wantOrder: []int{2, 3, 0, 1},
		},
		{
			name: "symbols/module-style host py3 reg3",
			paths: []string{
				"library/python/symbols/module/library.python.symbols.module.syms.reg3.cpp.py3.pic.o",
				"library/python/symbols/module/objcopy_a.o",
				"library/python/symbols/module/objcopy_b.o",
			},
			wantOrder: []int{1, 2, 0},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			refs := make([]NodeRef, len(tc.paths))
			paths := make([]VFS, len(tc.paths))
			for i, rel := range tc.paths {
				refs[i] = NodeRef{id: int64(i + 1)}
				paths[i] = Build(rel)
			}

			gotRefs, gotPaths := reorderARMembers(
				refs,
				paths,
				make([]bool, len(tc.paths)),
				make([]bool, len(tc.paths)),
				len(tc.paths),
			)

			wantRefs := make([]NodeRef, len(tc.wantOrder))
			wantPaths := make([]string, len(tc.wantOrder))
			for i, idx := range tc.wantOrder {
				wantRefs[i] = refs[idx]
				wantPaths[i] = Build(tc.paths[idx]).String()
			}

			gotPathStrings := make([]string, len(gotPaths))
			for i, path := range gotPaths {
				gotPathStrings[i] = path.String()
			}

			if !reflect.DeepEqual(gotPathStrings, wantPaths) {
				t.Fatalf("paths mismatch:\n got: %v\nwant: %v", gotPathStrings, wantPaths)
			}
			if !reflect.DeepEqual(gotRefs, wantRefs) {
				t.Fatalf("refs mismatch:\n got: %v\nwant: %v", gotRefs, wantRefs)
			}
		})
	}
}

const t17SwigTargetDir = "contrib/tools/swig"

func TestReorderLDMembers_LegacyDoubleUnderscorePathsTrailRegularSources(t *testing.T) {
	refs := []NodeRef{{id: 1}, {id: 2}, {id: 3}}
	paths := []VFS{
		Intern("$(B)/contrib/tools/swig/_/Source/CParse/cscanner.c.pic.o"),
		Intern("$(B)/contrib/tools/swig/_/_/Source/CParse/parser.y.c.pic.o"),
		Intern("$(B)/contrib/tools/swig/_/Source/CParse/templ.c.pic.o"),
	}

	gotRefs, gotPaths := reorderLDMembers(refs, paths)

	wantRefs := []NodeRef{{id: 1}, {id: 3}, {id: 2}}
	if !reflect.DeepEqual(gotRefs, wantRefs) {
		t.Fatalf("ld refs mismatch:\n  got:  %#v\n  want: %#v", gotRefs, wantRefs)
	}

	wantPaths := []string{
		"$(B)/contrib/tools/swig/_/Source/CParse/cscanner.c.pic.o",
		"$(B)/contrib/tools/swig/_/Source/CParse/templ.c.pic.o",
		"$(B)/contrib/tools/swig/_/_/Source/CParse/parser.y.c.pic.o",
	}
	if got := vfsStrings(gotPaths); !reflect.DeepEqual(got, wantPaths) {
		t.Fatalf("ld paths mismatch:\n  got:  %#v\n  want: %#v", got, wantPaths)
	}
}

var t20ResourceMacroRE = regexp.MustCompile(`\$\((CLANG|LLD_ROOT|YMAKE_PYTHON3)-[0-9]+\)`)

type t20RefCmd struct {
	CmdArgs []string          `json:"cmd_args"`
	Env     map[string]string `json:"env"`
}

type t20RefNode struct {
	Cmds    []t20RefCmd `json:"cmds"`
	Deps    []string    `json:"deps"`
	Inputs  []string    `json:"inputs"`
	Outputs []string    `json:"outputs"`
	UID     string      `json:"uid"`
}

func TestCollectModule_SETAPPENDRPathGlobal(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "mod")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir mod: %v", err)
	}

	content := "RESOURCES_LIBRARY()\nSET_APPEND(RPATH_GLOBAL '-Wl,-rpath,${\"$\"}ORIGIN')\nEND()\n"
	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), []byte(content), 0o644); err != nil {
		t.Fatalf("write ya.make: %v", err)
	}

	fs := NewFS(root)
	mf := Throw2(ParseFile(fs, filepath.Join(modDir, "ya.make")))
	instance := ModuleInstance{Path: "mod", Kind: KindLib, Platform: testTargetP}
	d := collectModule(newIncludeParserManagerFS(fs, newSharedParseCache()), "mod", KindLib, mf.Stmts, buildIfEnv(instance))

	want := []string{"-Wl,-rpath,$ORIGIN"}
	if !reflect.DeepEqual(d.rpathFlagsGlobal, want) {
		t.Fatalf("rpathFlagsGlobal mismatch:\n  got:  %#v\n  want: %#v", d.rpathFlagsGlobal, want)
	}
}

func testGenT20(sourceRoot, targetDir string) *Graph {
	host := newT20ResourcePlatform(OSLinux, ISAX8664, "yes", []string{"tool"})
	target := newT20ResourcePlatform(OSLinux, ISAAArch64, "yes", nil)

	return Gen(sourceRoot, targetDir, host, target, func(Warn) {})
}

func testGenT20Tool(sourceRoot, targetDir string) *Graph {
	host := newT20ResourcePlatform(OSLinux, ISAX8664, "yes", []string{"tool"})

	return Gen(sourceRoot, targetDir, host, host, func(Warn) {})
}

func newT20ResourcePlatform(os OS, isa ISA, pic string, tags []string) *Platform {
	flags := map[string]string{
		"AR_TOOL":           "$(CLANG)/bin/llvm-ar",
		"BUILD_PYTHON_BIN":  "$(YMAKE_PYTHON3)/bin/python3",
		"BUILD_PYTHON3_BIN": "$(YMAKE_PYTHON3)/bin/python3",
		"CLANG_TOOL":        "$(CLANG)/bin/clang",
		"CLANG_pl_pl_TOOL":  "$(CLANG)/bin/clang++",
		"LLD_TOOL":          "$(LLD_ROOT)/bin/ld.lld",
		"OBJCOPY_TOOL":      "$(CLANG)/bin/llvm-objcopy",
		"PIC":               pic,
		"STRIP_TOOL":        "$(CLANG)/bin/llvm-strip",
	}

	return NewPlatform(os, isa, flags, tags, "", "")
}

type t20RefGraph struct {
	nodes []*t20RefNode
	byUID map[string]*t20RefNode
}

func findT20RefNodeByOutputs(t *testing.T, ref *t20RefGraph, wantOutputs ...string) *t20RefNode {
	t.Helper()

	for _, node := range ref.nodes {
		if slices.Equal(node.Outputs, wantOutputs) {
			return node
		}
	}

	t.Fatalf("reference node with outputs %v not found", wantOutputs)
	return nil
}

func findGraphNodeByOutputs(t *testing.T, g *Graph, wantOutputs ...string) *Node {
	t.Helper()

	for _, node := range g.Graph {
		if len(node.Outputs) != len(wantOutputs) {
			continue
		}

		match := true
		for i, out := range node.Outputs {
			if out.String() != wantOutputs[i] {
				match = false

				break
			}
		}

		if match {
			return node
		}
	}

	t.Fatalf("graph node with outputs %v not found", wantOutputs)
	return nil
}

func cmdArgsFrom[T interface{ ~[]string }](t *testing.T, args T, marker string) []string {
	t.Helper()

	idx := slices.Index(args, marker)
	if idx < 0 {
		t.Fatalf("marker %q not found in cmd args: %v", marker, args)
	}

	return append([]string(nil), args[idx:]...)
}

func normalizeT20Token(s string) string {
	s = strings.NewReplacer(
		"$(BUILD_ROOT)", "$(B)",
		"$(SOURCE_ROOT)", "$(S)",
	).Replace(s)

	return t20ResourceMacroRE.ReplaceAllStringFunc(s, func(match string) string {
		dash := strings.IndexByte(match, '-')
		if dash < 0 {
			return match
		}

		return "$(" + match[2:dash] + ")"
	})
}

func normalizeT20Strings(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = normalizeT20Token(s)
	}

	return out
}

func normalizeT20Env(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}

	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = normalizeT20Token(v)
	}

	return out
}

func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)

	return out
}

func assertCmdArgsAbsent(t *testing.T, args []string, banned ...string) {
	t.Helper()

	for _, wantAbsent := range banned {
		if slices.Contains(args, wantAbsent) {
			t.Fatalf("cmd_args unexpectedly contain %q: %v", wantAbsent, args)
		}
	}
}

func projectGraphDepOutputs(t *testing.T, g *Graph, deps []string) [][]string {
	t.Helper()

	byUID := make(map[string]*Node, len(g.Graph))
	for _, node := range g.Graph {
		byUID[node.UID] = node
	}

	out := make([][]string, 0, len(deps))
	for _, uid := range deps {
		node := byUID[uid]
		if node == nil {
			t.Fatalf("dep uid %q not found in generated graph", uid)
		}

		out = append(out, append([]string(nil), vfsStrings(node.Outputs)...))
	}

	sort.Slice(out, func(i, j int) bool {
		return strings.Join(out[i], "\x00") < strings.Join(out[j], "\x00")
	})

	return out
}

func projectT20RefDepOutputs(t *testing.T, ref *t20RefGraph, deps []string) [][]string {
	t.Helper()

	out := make([][]string, 0, len(deps))
	for _, uid := range deps {
		node := ref.byUID[uid]
		if node == nil {
			t.Fatalf("dep uid %q not found in reference graph", uid)
		}

		out = append(out, normalizeT20Strings(node.Outputs))
	}

	sort.Slice(out, func(i, j int) bool {
		return strings.Join(out[i], "\x00") < strings.Join(out[j], "\x00")
	})

	return out
}

func TestGen_ManualCompanionSourceUsesCythonCompanionCCInputs(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeTestModuleFile(t, root, "pkg/ya.make", "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\nSRCS(helper.cpp)\nPY_SRCS(NAMESPACE pkg mod.pyx)\nEND()\n")
	writeTestModuleFile(t, root, "pkg/helper.cpp", "int f(){return 0;}\n")
	writeTestModuleFile(t, root, "pkg/mod.pyx", "def f():\n    return 0\n")

	g := testGen(root, "pkg")
	helper := mustNodeByOutput(t, g, "$(B)/pkg/helper.cpp.o")
	args := helper.Cmds[0].CmdArgs

	pythonIncludeIdx := indexOfArg(args, "-I$(S)/contrib/libs/python/Include")
	if pythonIncludeIdx < 0 {
		t.Fatalf("helper.cpp.o cmd_args missing python include: %#v", args)
	}

	wantNumpy := []string{
		"-I$(S)/contrib/python/numpy/include/numpy/core/include",
		"-I$(S)/contrib/python/numpy/include/numpy/core/include/numpy",
		"-I$(S)/contrib/python/numpy/include/numpy/core/src/common",
		"-I$(S)/contrib/python/numpy/include/numpy/core/src/npymath",
		"-I$(S)/contrib/python/numpy/include/numpy/distutils/include",
	}

	if pythonIncludeIdx+1+len(wantNumpy) > len(args) {
		t.Fatalf("helper.cpp.o cmd_args too short for numpy include bundle: %#v", args)
	}

	for i, want := range wantNumpy {
		if got := args[pythonIncludeIdx+1+i]; got != want {
			t.Fatalf("numpy include bundle mismatch at offset %d: got %q, want %q; cmd_args=%#v", i, got, want, args)
		}
	}

	for _, arg := range args {
		if strings.HasPrefix(arg, "-DPyInit_") || strings.HasPrefix(arg, "-Dinit_module_") {
			t.Fatalf("helper.cpp.o cmd_args still carry PY_REGISTER define %q: %#v", arg, args)
		}
	}
}

func TestGen_LibraryARIncludesResourceObjcopyMemberInputs(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(t, root, "tools/rescompiler/bin", "rescompiler")
	writeToolProgram(t, root, "tools/rescompressor/bin", "rescompressor")

	writeTestModuleFile(t, root, "db/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(main.cpp)\nRESOURCE(data.sql key)\nEND()\n")
	writeTestModuleFile(t, root, "db/main.cpp", "int f(){return 0;}\n")
	writeTestModuleFile(t, root, "db/data.sql", "select 1;\n")

	g := testGen(root, "db")
	regularAR := mustNodeByOutput(t, g, "$(B)/db/libdb.a")
	mustNodeByOutput(t, g, "$(B)/db/libdb.global.a")
	if findNodeByOutputPrefix(g, "$(B)/db/objcopy_") == nil {
		t.Fatal("graph is missing db objcopy output")
	}

	if !nodeHasInput(regularAR, "$(S)/build/scripts/link_lib.py") {
		t.Fatalf("libdb.a inputs missing its own script link_lib.py: %#v", regularAR.Inputs)
	}
	for _, absent := range []string{"$(S)/db/data.sql", "$(S)/build/scripts/objcopy.py"} {
		if nodeHasInput(regularAR, absent) {
			t.Errorf("libdb.a must not list %q (not an AR input): %#v", absent, regularAR.Inputs)
		}
	}
}

func TestGen_ResourceRelativeOutputFeedsObjcopyFromBuildRoot(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(t, root, "tools/rescompiler/bin", "rescompiler")
	writeToolProgram(t, root, "tools/rescompressor/bin", "rescompressor")
	writeToolProgram(t, root, "tools/json_gen/bin", "json_gen")

	writeTestModuleFile(t, root, "db/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    tools/json_gen/bin
        --output
        data.json
    OUT_NOAUTO data.json
)
RESOURCE(
    data.json /data.json
)
END()
`)

	g := testGen(root, "db")

	objcopy := findNodeByOutputPrefix(g, "$(B)/db/objcopy_")
	if objcopy == nil {
		t.Fatal("graph is missing db objcopy output")
	}
	if !nodeHasInput(objcopy, "$(B)/db/data.json") {
		t.Fatalf("objcopy inputs missing build-root data.json: %#v", objcopy.Inputs)
	}
	if nodeHasInput(objcopy, "$(S)/db/data.json") {
		t.Fatalf("objcopy inputs still use source-root data.json: %#v", objcopy.Inputs)
	}
}

func TestGen_ResourceBindirOutputFeedsObjcopyFromBuildRoot(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(t, root, "tools/rescompiler/bin", "rescompiler")
	writeToolProgram(t, root, "tools/rescompressor/bin", "rescompressor")
	writeToolProgram(t, root, "tools/json_gen/bin", "json_gen")

	writeTestModuleFile(t, root, "db/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    tools/json_gen/bin
        --output
        ${BINDIR}/data.json
    OUT_NOAUTO ${BINDIR}/data.json
)
RESOURCE(
    ${BINDIR}/data.json /data.json
)
END()
`)

	g := testGen(root, "db")

	objcopy := findNodeByOutputPrefix(g, "$(B)/db/objcopy_")
	if objcopy == nil {
		t.Fatal("graph is missing db objcopy output")
	}
	if !nodeHasInput(objcopy, "$(B)/db/data.json") {
		t.Fatalf("objcopy inputs missing build-root data.json: %#v", objcopy.Inputs)
	}
	for _, in := range objcopy.Inputs {
		if strings.Contains(in.String(), "${BINDIR}") {
			t.Fatalf("objcopy inputs still leak ${BINDIR}: %#v", objcopy.Inputs)
		}
	}
}

func TestGen_ResourceBindirRunProgramCarriesInputClosure(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(t, root, "tools/rescompiler/bin", "rescompiler")
	writeToolProgram(t, root, "tools/rescompressor/bin", "rescompressor")
	writeToolProgram(t, root, "tools/json_gen/bin", "json_gen")

	writeTestModuleFile(t, root, "dep/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(dep.cpp dep.h)
END()
`)
	writeTestModuleFile(t, root, "dep/dep.cpp", "int dep(){return 0;}\n")
	writeTestModuleFile(t, root, "dep/dep.h", "#pragma once\n")

	writeTestModuleFile(t, root, "db/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(dep)
RUN_PROGRAM(
    tools/json_gen/bin
        --output
        ${BINDIR}/unused.cpp
        --json-output
        ${BINDIR}/data.json
        gen.h
    IN gen.h
    OUT_NOAUTO ${BINDIR}/data.json
)
RESOURCE(
    ${BINDIR}/data.json /data.json
)
END()
`)
	writeTestModuleFile(t, root, "db/gen.h", `#pragma once
#include <dep/dep.h>
`)

	g := testGen(root, "db")

	pr := mustNodeByOutput(t, g, "$(B)/db/data.json")
	if !nodeHasInput(pr, "$(S)/db/gen.h") {
		t.Fatalf("pr inputs missing direct gen.h input: %#v", pr.Inputs)
	}
	if !nodeHasInput(pr, "$(S)/dep/dep.h") {
		t.Fatalf("pr inputs missing transitive dep/dep.h closure: %#v", pr.Inputs)
	}

	objcopy := findNodeByOutputPrefix(g, "$(B)/db/objcopy_")
	if objcopy == nil {
		t.Fatal("graph is missing db objcopy output")
	}
	if !nodeHasInput(objcopy, "$(B)/db/data.json") {
		t.Fatalf("objcopy inputs missing build-root data.json: %#v", objcopy.Inputs)
	}
	if !nodeHasInput(objcopy, "$(S)/dep/dep.h") {
		t.Fatalf("objcopy inputs missing transitive dep/dep.h closure: %#v", objcopy.Inputs)
	}
}

func writeToolProgram(t *testing.T, root, modulePath, binaryName string) {
	t.Helper()

	writeTestModuleFile(t, root, filepath.ToSlash(filepath.Join(modulePath, "ya.make")), "PROGRAM("+binaryName+")\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(main.cpp)\nEND()\n")
	writeTestModuleFile(t, root, filepath.ToSlash(filepath.Join(modulePath, "main.cpp")), "int main(){return 0;}\n")
}

func writeTestModuleFile(t *testing.T, root, rel, content string) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustNodeByOutput(t *testing.T, g *Graph, output string) *Node {
	t.Helper()

	for _, n := range g.Graph {
		if len(n.Outputs) > 0 && n.Outputs[0].String() == output {
			return n
		}
	}

	t.Fatalf("graph is missing output %q", output)
	return nil
}

func findNodeByOutputPrefix(g *Graph, prefix string) *Node {
	for _, n := range g.Graph {
		if len(n.Outputs) > 0 && strings.HasPrefix(n.Outputs[0].String(), prefix) {
			return n
		}
	}

	return nil
}

func nodeHasInput(n *Node, input string) bool {
	for _, got := range n.Inputs {
		if got.String() == input {
			return true
		}
	}

	return false
}

func indexOfArg(args []string, want string) int {
	for i, arg := range args {
		if arg == want {
			return i
		}
	}

	return -1
}

func TestIsHeaderSource_ExtendedHeaderExtensions(t *testing.T) {
	for _, src := range []string{
		"a.h",
		"a.hh",
		"a.hpp",
		"a.hxx",
		"a.ipp",
		"a.ixx",
		"a.inl",
	} {
		if !isHeaderSource(src) {
			t.Fatalf("isHeaderSource(%q) = false, want true", src)
		}
	}
}

type statsUIDRefNode struct {
	HostPlatform bool `json:"host_platform,omitempty"`
	KV           struct {
		P string `json:"p"`
	} `json:"kv"`
	Platform string   `json:"platform"`
	Outputs  []string `json:"outputs"`
	StatsUID string   `json:"stats_uid"`
}

type statsUIDNodeKey struct {
	Outputs      string
	Kind         string
	HostPlatform bool
	Platform     string
}

type indexedStatsUIDNode struct {
	StatsUID string
}

func genStatsUIDReferenceSample(sourceRoot, targetDir string) *Graph {
	host, target := statsUIDReferencePlatforms()

	return Gen(sourceRoot, targetDir, host, target, func(Warn) {})
}

func genDumpStatsUIDReferenceSample(t *testing.T, sourceRoot, targetDir string) []statsUIDRefNode {
	t.Helper()

	out, err := os.CreateTemp(t.TempDir(), "sg3-dump-*.json")
	if err != nil {
		t.Fatalf("create dump graph capture: %v", err)
	}

	oldStdout := os.Stdout
	os.Stdout = out

	var code int
	exc := Try(func() {
		code = cmdMake([]string{
			"-j", "0",
			"-k",
			"-G",
			"--source-root", sourceRoot,
			"--target-platform", "default-linux-aarch64",
			"--host-platform", "default-linux-x86_64",
			"--host-platform-flag", "OS_SDK=local",
			"--sandboxing",
			"-DOS_SDK=local",
			targetDir,
		})
	})

	os.Stdout = oldStdout
	if err := out.Close(); err != nil {
		t.Fatalf("close dump graph capture: %v", err)
	}
	if exc != nil {
		t.Fatalf("cmdMake dump graph failed: %v", exc)
	}
	if code != 0 {
		t.Fatalf("cmdMake dump graph exit code = %d, want 0", code)
	}

	return loadStatsUIDRefNodes(t, out.Name())
}

func statsUIDReferencePlatforms() (*Platform, *Platform) {
	hostPlatformFlags := map[string]string{
		"APPLE_SDK_LOCAL":    "yes",
		"OPENSOURCE":         "yes",
		"OS_SDK":             "local",
		"USE_CLANG_CL":       "yes",
		"USE_PREBUILT_TOOLS": "no",
	}
	hostFlags := make(map[string]string, len(testToolchainFlags)+8)
	for k, v := range testToolchainFlags {
		hostFlags[k] = v
	}
	for k, v := range hostPlatformFlags {
		hostFlags[k] = v
	}
	hostFlags["GG_BUILD_TYPE"] = "release"
	hostFlags["PIC"] = "yes"
	hostFlags["SANDBOXING"] = "yes"
	host := NewPlatform(OSLinux, ISAX8664, hostFlags, []string{"tool"}, "", "")
	host.StatsFlags = buildHostStatsFlags(hostPlatformFlags, nil, true)

	targetFlags := make(map[string]string, len(testToolchainFlags)+4)
	for k, v := range testToolchainFlags {
		targetFlags[k] = v
	}
	targetFlags["GG_BUILD_TYPE"] = "debug"
	targetFlags["PIC"] = "no"
	targetFlags["SANDBOXING"] = "yes"
	target := NewPlatform(OSLinux, ISAAArch64, targetFlags, nil, "", "")
	target.Tags = sandboxingNodeTags(target)
	target.StatsFlags = buildTargetStatsFlags(targetFlags, map[string]string{})

	return host, target
}

func loadStatsUIDRefNodes(t *testing.T, path string) []statsUIDRefNode {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var graph struct {
		Graph []statsUIDRefNode `json:"graph"`
	}
	if err := json.Unmarshal(raw, &graph); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}

	return graph.Graph
}

func assertTargetStatsUIDsMatchReference(t *testing.T, our []*Node, ref []statsUIDRefNode, minCommon int, refName string) {
	t.Helper()

	ourByKey := indexTargetStatsUIDNodes(t, our)
	refByKey := indexTargetStatsUIDRefNodes(t, ref)

	commonKeys, onlyOur, onlyRef := diffStatsUIDNodeKeys(ourByKey, refByKey)
	if len(onlyOur) > 0 || len(onlyRef) > 0 {
		var problems []string
		if len(onlyOur) > 0 {
			problems = append(problems,
				"extra generated non-host key "+statsUIDDescribeKey(onlyOur[0])+
					" ("+strconv.Itoa(len(onlyOur))+" total)")
		}
		if len(onlyRef) > 0 {
			problems = append(problems,
				"missing generated non-host key "+statsUIDDescribeKey(onlyRef[0])+
					" ("+strconv.Itoa(len(onlyRef))+" total)")
		}
		t.Fatalf("non-host node key drift vs %s: %s", refName, strings.Join(problems, "; "))
	}

	for _, key := range commonKeys {
		ourNode := ourByKey[key]
		refNode := refByKey[key]
		if ourNode.StatsUID != refNode.StatsUID {
			t.Fatalf("stats_uid mismatch for %s:\n got: %s\nwant: %s",
				statsUIDDescribeKey(key), ourNode.StatsUID, refNode.StatsUID)
		}
	}

	if len(commonKeys) < minCommon {
		t.Fatalf("expected at least %d common non-host nodes vs %s, found %d", minCommon, refName, len(commonKeys))
	}
}

func assertHostStatsUIDsMatchReference(t *testing.T, our []*Node, ref []statsUIDRefNode, minCommon int, refName string) {
	t.Helper()

	ourByKey := indexHostStatsUIDNodes(t, our)
	refByKey := indexHostStatsUIDRefNodes(t, ref)

	commonKeys, onlyOur, onlyRef := diffStatsUIDNodeKeys(ourByKey, refByKey)
	if len(onlyOur) > 0 || len(onlyRef) > 0 {
		var problems []string
		if len(onlyOur) > 0 {
			problems = append(problems,
				"extra generated host key "+statsUIDDescribeKey(onlyOur[0])+
					" ("+strconv.Itoa(len(onlyOur))+" total)")
		}
		if len(onlyRef) > 0 {
			problems = append(problems,
				"missing generated host key "+statsUIDDescribeKey(onlyRef[0])+
					" ("+strconv.Itoa(len(onlyRef))+" total)")
		}
		t.Fatalf("host node key drift vs %s: %s", refName, strings.Join(problems, "; "))
	}

	for _, key := range commonKeys {
		ourNode := ourByKey[key]
		refNode := refByKey[key]
		if ourNode.StatsUID != refNode.StatsUID {
			t.Fatalf("host stats_uid mismatch for %s:\n got: %s\nwant: %s",
				statsUIDDescribeKey(key), ourNode.StatsUID, refNode.StatsUID)
		}
	}

	if len(commonKeys) < minCommon {
		t.Fatalf("expected at least %d common host nodes vs %s, found %d", minCommon, refName, len(commonKeys))
	}
}

func indexTargetStatsUIDNodes(t *testing.T, nodes []*Node) map[statsUIDNodeKey]indexedStatsUIDNode {
	t.Helper()

	out := make(map[statsUIDNodeKey]indexedStatsUIDNode)
	for _, node := range nodes {
		if nodeHasHostTag(node.Tags) {
			continue
		}

		key := statsUIDNodeKeyFromNode(node)
		value := indexedStatsUIDNode{StatsUID: node.StatsUID}
		if _, exists := out[key]; exists {
			t.Fatalf("duplicate generated non-host key %s", statsUIDDescribeKey(key))
		}
		out[key] = value
	}

	return out
}

func indexHostStatsUIDNodes(t *testing.T, nodes []*Node) map[statsUIDNodeKey]indexedStatsUIDNode {
	t.Helper()

	out := make(map[statsUIDNodeKey]indexedStatsUIDNode)
	for _, node := range nodes {
		if !nodeHasHostTag(node.Tags) {
			continue
		}

		key := statsUIDNodeKeyFromNode(node)
		value := indexedStatsUIDNode{StatsUID: node.StatsUID}
		if _, exists := out[key]; exists {
			t.Fatalf("duplicate generated host key %s", statsUIDDescribeKey(key))
		}
		out[key] = value
	}

	return out
}

func indexTargetStatsUIDRefNodes(t *testing.T, nodes []statsUIDRefNode) map[statsUIDNodeKey]indexedStatsUIDNode {
	t.Helper()

	out := make(map[statsUIDNodeKey]indexedStatsUIDNode)
	for _, node := range nodes {
		if node.HostPlatform {
			continue
		}

		key := statsUIDNodeKeyFromRef(node)
		value := indexedStatsUIDNode{StatsUID: node.StatsUID}
		if _, exists := out[key]; exists {
			t.Fatalf("duplicate reference non-host key %s", statsUIDDescribeKey(key))
		}
		out[key] = value
	}

	return out
}

func indexHostStatsUIDRefNodes(t *testing.T, nodes []statsUIDRefNode) map[statsUIDNodeKey]indexedStatsUIDNode {
	t.Helper()

	out := make(map[statsUIDNodeKey]indexedStatsUIDNode)
	for _, node := range nodes {
		if !node.HostPlatform {
			continue
		}

		key := statsUIDNodeKeyFromRef(node)
		value := indexedStatsUIDNode{StatsUID: node.StatsUID}
		if _, exists := out[key]; exists {
			t.Fatalf("duplicate reference host key %s", statsUIDDescribeKey(key))
		}
		out[key] = value
	}

	return out
}

func diffStatsUIDNodeKeys(our, ref map[statsUIDNodeKey]indexedStatsUIDNode) ([]statsUIDNodeKey, []statsUIDNodeKey, []statsUIDNodeKey) {
	commonKeys := make([]statsUIDNodeKey, 0, len(our))
	onlyOur := make([]statsUIDNodeKey, 0)
	onlyRef := make([]statsUIDNodeKey, 0)

	for key := range our {
		if _, ok := ref[key]; ok {
			commonKeys = append(commonKeys, key)
			continue
		}
		onlyOur = append(onlyOur, key)
	}
	for key := range ref {
		if _, ok := our[key]; ok {
			continue
		}
		onlyRef = append(onlyRef, key)
	}

	sortStatsUIDNodeKeys(commonKeys)
	sortStatsUIDNodeKeys(onlyOur)
	sortStatsUIDNodeKeys(onlyRef)

	return commonKeys, onlyOur, onlyRef
}

func sortStatsUIDNodeKeys(keys []statsUIDNodeKey) {
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Outputs != keys[j].Outputs {
			return keys[i].Outputs < keys[j].Outputs
		}
		if keys[i].Kind != keys[j].Kind {
			return keys[i].Kind < keys[j].Kind
		}
		if keys[i].HostPlatform != keys[j].HostPlatform {
			return !keys[i].HostPlatform && keys[j].HostPlatform
		}
		return keys[i].Platform < keys[j].Platform
	})
}

func statsUIDDescribeKey(key statsUIDNodeKey) string {
	return "outputs=" + strings.Join(statsUIDOutputsFromKey(key), ",") +
		" kind=" + key.Kind +
		" host_platform=" + boolString(key.HostPlatform) +
		" platform=" + key.Platform
}

func statsUIDOutputsFromKey(key statsUIDNodeKey) []string {
	if key.Outputs == "" {
		return nil
	}
	return strings.Split(key.Outputs, "\x00")
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func statsUIDOutputKey(outputs []string) string {
	normalized := append([]string(nil), outputs...)
	for i, out := range normalized {
		normalized[i] = normalizeStatsUIDOutput(out)
	}
	sort.Strings(normalized)

	return strings.Join(normalized, "\x00")
}

func statsUIDNodeKeyFromNode(node *Node) statsUIDNodeKey {
	kind, _ := node.KV["p"].(string)

	return statsUIDNodeKey{
		Outputs:      statsUIDOutputKey(vfsStrings(node.Outputs)),
		Kind:         kind,
		HostPlatform: nodeHasHostTag(node.Tags),
		Platform:     node.Platform,
	}
}

func statsUIDNodeKeyFromRef(node statsUIDRefNode) statsUIDNodeKey {
	return statsUIDNodeKey{
		Outputs:      statsUIDOutputKey(node.Outputs),
		Kind:         node.KV.P,
		HostPlatform: node.HostPlatform,
		Platform:     node.Platform,
	}
}

func normalizeStatsUIDOutput(out string) string {
	out = strings.ReplaceAll(out, "$(BUILD_ROOT)", "$(B)")
	out = strings.ReplaceAll(out, "$(SOURCE_ROOT)", "$(S)")

	return out
}

func TestGen_ProtoLibrary_NamedArgUsedForArchive(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "ydb/public/api/protos/ya.make", `PROTO_LIBRARY(api-protos)
SRCS(ydb.proto)
END()
`)
	writeTestModuleFile(t, root, "ydb/public/api/protos/ydb.proto", `syntax = "proto3";
package test;
message Ydb {}
`)
	writeToolProgram(t, root, "contrib/tools/protoc", "protoc")
	writeToolProgram(t, root, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(root, "ydb/public/api/protos")

	mustNodeByOutput(t, g, "$(B)/ydb/public/api/protos/libapi-protos.a")

	for _, n := range g.Graph {
		for _, o := range n.Outputs {
			if o.String() == "$(B)/ydb/public/api/protos/libprotos.a" {
				t.Fatalf("path-derived archive libprotos.a should not exist; got it with named arg")
			}
		}
	}
}

func TestGen_ProtoLibrary_UnnamedArgKeepsPathDerivedArchive(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "ydb/public/api/protos/ya.make", `PROTO_LIBRARY()
SRCS(ydb.proto)
END()
`)
	writeTestModuleFile(t, root, "ydb/public/api/protos/ydb.proto", `syntax = "proto3";
package test;
message Ydb {}
`)
	writeToolProgram(t, root, "contrib/tools/protoc", "protoc")
	writeToolProgram(t, root, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(t, root, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(root, "ydb/public/api/protos")

	mustNodeByOutput(t, g, "$(B)/ydb/public/api/protos/libpublic-api-protos.a")
}
