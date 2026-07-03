package main

import (
	"slices"
	"strings"
	"testing"
)

func addLLVMBCToolchainPeer(files map[string]string) {
	files["build/platform/clang/ya.make"] = "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON(CLANG16 clang16.json)\nEND()\n"
	files["build/platform/clang/clang16.json"] = `{"by_platform":{"linux-x86_64":{"uri":"sbr:test-clang16"}}}`
}

func TestEmitLLVMBC_OptPassesNoBraceComma(t *testing.T) {
	const modPath = "mod/llvm"

	files := map[string]string{}
	addLLVMBCToolchainPeer(files)
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	files[modPath+"/ya.make"] = `LIBRARY()
USE_LLVM_BC16()
LLVM_BC(
    foo.cpp
    NAME
    Bar
    SUFFIX .16
    SYMBOLS
    DoThing
)
SRCS(foo.cpp)
END()
`
	files[modPath+"/foo.cpp"] = "int Bar(){return 0;}\n"

	g := testGen(newMemFS(files), modPath)

	var opNode *Node

	for _, n := range g.Graph {
		if p := n.KV.P.string(); p != "OP" {
			continue
		}

		for _, o := range n.Outputs {
			if strings.Contains(o.string(), "Bar_optimized") {
				opNode = n

				break
			}
		}

		if opNode != nil {
			break
		}
	}

	if opNode == nil {
		t.Fatal("graph missing OP node for Bar_optimized")
	}

	if len(opNode.Cmds) == 0 {
		t.Fatal("OP node has no cmds")
	}

	args := strStrs(opNode.Cmds[0].CmdArgs.flat())
	var passesArg string

	for _, a := range args {
		if strings.HasPrefix(a, "-passes=") {
			passesArg = a

			break
		}
	}

	if passesArg == "" {
		t.Fatalf("OP cmd args contain no -passes= arg: %v", args)
	}

	if strings.Contains(passesArg, "${__COMMA__}") {
		t.Errorf("-passes= arg still contains unexpanded ${__COMMA__}: %q", passesArg)
	}

	if strings.HasPrefix(passesArg, "'") || strings.HasSuffix(passesArg, "'") {
		t.Errorf("-passes= arg has spurious outer single-quotes: %q", passesArg)
	}

	if !strings.Contains(passesArg, ",") {
		t.Errorf("-passes= arg has no comma separators: %q", passesArg)
	}

	want := `-passes="default<O2>,globalopt,globaldce,internalize"`

	if passesArg != want {
		t.Errorf("-passes= arg = %q, want %q", passesArg, want)
	}
}

func TestEmitLLVMBC_BCNodeIncludesCompileFlags(t *testing.T) {
	const modPath = "mod/llvm"

	files := map[string]string{}
	addLLVMBCToolchainPeer(files)
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	files[modPath+"/ya.make"] = `LIBRARY()
USE_LLVM_BC16()
LLVM_BC(
    foo.cpp
    NAME
    Bar
    SUFFIX .16
)
SRCS(foo.cpp)
END()
`
	files[modPath+"/foo.cpp"] = "int Bar(){return 0;}\n"

	g := testGen(newMemFS(files), modPath)

	var bcNode *Node

	for _, n := range g.Graph {
		if p := n.KV.P.string(); p != "BC" {
			continue
		}

		for _, o := range n.Outputs {
			if strings.HasSuffix(o.string(), "foo.cpp.16.bc") {
				bcNode = n

				break
			}
		}

		if bcNode != nil {
			break
		}
	}

	if bcNode == nil {
		t.Fatal("graph missing BC node for foo.cpp.16.bc")
	}

	if len(bcNode.Cmds) == 0 {
		t.Fatal("BC node has no cmds")
	}

	args := strStrs(bcNode.Cmds[0].CmdArgs.flat())

	hasIB := false
	hasIS := false

	for _, a := range args {
		if a == "-I$(B)" {
			hasIB = true
		}

		if a == "-I$(S)" {
			hasIS = true
		}
	}

	if !hasIB {
		t.Errorf("BC compile cmd missing -I$(B): %v", args)
	}

	if !hasIS {
		t.Errorf("BC compile cmd missing -I$(S): %v", args)
	}

	hasArcadiaRoot := false

	for _, a := range args {
		if a == "-DARCADIA_ROOT=$(S)" {
			hasArcadiaRoot = true

			break
		}
	}

	if !hasArcadiaRoot {
		t.Errorf("BC compile cmd missing -DARCADIA_ROOT=$(S): %v", args)
	}
}

func TestEmitLLVMBC_PipelineProducesFiveNodes(t *testing.T) {
	const modPath = "mod/llvm"

	files := map[string]string{}
	addLLVMBCToolchainPeer(files)
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

	for k, v := range map[string]string{
		modPath + "/ya.make": `LIBRARY()

USE_LLVM_BC16()

LLVM_BC(
    foo.cpp
    NAME
    Bar
    SUFFIX .16
    SYMBOLS
    DoThing
)

SRCS(foo.cpp)

END()
`,
		modPath + "/foo.cpp": "int Bar(){return 0;}\n",
	} {
		files[k] = v
	}

	g := testGen(newMemFS(files), modPath)

	byOut := make(map[string]*Node, len(g.Graph))

	for _, n := range g.Graph {
		for _, o := range n.Outputs {
			byOut[o.string()] = n
		}
	}

	want := map[string]string{
		"$(B)/" + modPath + "/foo.cpp.16.bc":       "BC",
		"$(B)/" + modPath + "/Bar_merged.16.bc":    "LD",
		"$(B)/" + modPath + "/Bar_optimized.16.bc": "OP",
	}

	for path, kvp := range want {
		n := byOut[path]

		if n == nil {
			t.Errorf("graph missing %s node with output %q", kvp, path)

			continue
		}

		if got := n.KV.P.string(); got != kvp {
			t.Errorf("output %q kv.p = %q, want %q", path, got, kvp)
		}
	}

	var pyNode *Node

	for _, n := range g.Graph {
		if got := n.KV.P.string(); got != "PY" {
			continue
		}

		for _, o := range n.Outputs {
			if strings.HasPrefix(o.string(), "$(B)/"+modPath+"/objcopy_") &&
				strings.HasSuffix(o.string(), ".o") {
				pyNode = n

				break
			}
		}

		if pyNode != nil {
			break
		}
	}

	if pyNode == nil {
		t.Errorf("graph missing PY objcopy node for embedded LLVM_BC output")
	} else {
		hasOptBc := false

		for _, in := range pyNode.flatInputs() {
			if in.string() == "$(B)/"+modPath+"/Bar_optimized.16.bc" {
				hasOptBc = true

				break
			}
		}

		if !hasOptBc {
			t.Errorf("PY objcopy inputs do not include the optimized.bc: %v", pyNode.flatInputs())
		}
	}

	var arNode *Node

	for _, n := range g.Graph {
		if got := n.KV.P.string(); got != "AR" {
			continue
		}

		for _, o := range n.Outputs {
			if strings.HasSuffix(o.string(), ".global.a") {
				arNode = n

				break
			}
		}

		if arNode != nil {
			break
		}
	}

	if arNode == nil {
		t.Errorf("graph missing AR .global.a node carrying the PY objcopy.o")
	}
}

func TestEmitLLVMBC_BCNodeIncludesArchArgs(t *testing.T) {
	const modPath = "mod/llvm"

	files := map[string]string{}
	addLLVMBCToolchainPeer(files)
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	files[modPath+"/ya.make"] = `LIBRARY()
USE_LLVM_BC16()
LLVM_BC(
    foo.cpp
    NAME
    Bar
    SUFFIX .16
)
SRCS(foo.cpp)
END()
`
	files[modPath+"/foo.cpp"] = "int Bar(){return 0;}\n"

	g := testGen(newMemFS(files), modPath)

	var bcNode *Node

	for _, n := range g.Graph {
		if p := n.KV.P.string(); p != "BC" {
			continue
		}

		for _, o := range n.Outputs {
			if strings.HasSuffix(o.string(), "foo.cpp.16.bc") {
				bcNode = n

				break
			}
		}

		if bcNode != nil {
			break
		}
	}

	if bcNode == nil {
		t.Fatal("graph missing BC node for foo.cpp.16.bc")
	}

	if len(bcNode.Cmds) == 0 {
		t.Fatal("BC node has no cmds")
	}

	args := strStrs(bcNode.Cmds[0].CmdArgs.flat())

	hasMarch := false

	for _, a := range args {
		if a == "-march=armv8-a" {
			hasMarch = true

			break
		}
	}

	if !hasMarch {
		t.Errorf("BC compile cmd missing -march=armv8-a (AArch64 ArchArgs): %v", args)
	}

	targetIdx, marchIdx, binIdx := -1, -1, -1

	for i, a := range args {
		switch {
		case strings.HasPrefix(a, "--target="):
			targetIdx = i
		case a == "-march=armv8-a":
			marchIdx = i
		case strings.HasPrefix(a, "-B"):
			binIdx = i
		}
	}

	if targetIdx < 0 || marchIdx < 0 || binIdx < 0 {
		t.Fatalf("args missing --target / -march / -B: idx %d/%d/%d in %v", targetIdx, marchIdx, binIdx, args)
	}

	if !(targetIdx < marchIdx && marchIdx < binIdx) {
		t.Errorf("platform flag order wrong: --target[%d] -march[%d] -B[%d]; want --target < -march < -B", targetIdx, marchIdx, binIdx)
	}
}

func TestEmitLLVMBC_BCNodeCarriesIncludeClosure(t *testing.T) {
	const modPath = "mod/llvm"

	files := map[string]string{}
	addLLVMBCToolchainPeer(files)
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	files[modPath+"/ya.make"] = `LIBRARY()
USE_LLVM_BC16()
LLVM_BC(
    foo.cpp
    NAME
    Bar
    SUFFIX .16
)
SRCS(foo.cpp)
END()
`
	files[modPath+"/foo.cpp"] = "#include \"foo.h\"\nint Bar(){return 0;}\n"
	files[modPath+"/foo.h"] = "#pragma once\n"

	g := testGen(newMemFS(files), modPath)

	var bcNode *Node

	for _, n := range g.Graph {
		if p := n.KV.P.string(); p != "BC" {
			continue
		}

		for _, o := range n.Outputs {
			if strings.HasSuffix(o.string(), "foo.cpp.16.bc") {
				bcNode = n

				break
			}
		}

		if bcNode != nil {
			break
		}
	}

	if bcNode == nil {
		t.Fatal("graph missing BC node for foo.cpp.16.bc")
	}

	if len(bcNode.flatInputs()) < 2 {
		t.Errorf("BC node Inputs has only %d entries; want source + closure: %v", len(bcNode.flatInputs()), vfsStringsT3(bcNode.flatInputs()))
	}

	if !nodeHasInput(bcNode, "$(S)/"+modPath+"/foo.cpp") {
		t.Errorf("BC node Inputs missing the source foo.cpp: %v", vfsStringsT3(bcNode.flatInputs()))
	}

	if !nodeHasInput(bcNode, "$(S)/"+modPath+"/foo.h") {
		t.Errorf("BC node Inputs missing foo.h from include closure: %v", vfsStringsT3(bcNode.flatInputs()))
	}
}

func TestEmitLLVMBC_ObjcopyNodeCarriesBCClosure(t *testing.T) {
	const modPath = "mod/llvm"

	files := map[string]string{}
	addLLVMBCToolchainPeer(files)
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	files[modPath+"/ya.make"] = `LIBRARY()
USE_LLVM_BC16()
LLVM_BC(
    foo.cpp
    NAME
    Bar
    SUFFIX .16
)
SRCS(foo.cpp)
END()
`
	files[modPath+"/foo.cpp"] = "int Bar(){return 0;}\n"

	g := testGen(newMemFS(files), modPath)

	var pyNode *Node

	for _, n := range g.Graph {
		if p := n.KV.P.string(); p != "PY" {
			continue
		}

		for _, o := range n.Outputs {
			if strings.HasPrefix(o.string(), "$(B)/"+modPath+"/objcopy_") &&
				strings.HasSuffix(o.string(), ".o") {
				pyNode = n

				break
			}
		}

		if pyNode != nil {
			break
		}
	}

	if pyNode == nil {
		t.Fatal("graph missing PY objcopy node for LLVM_BC resource")
	}

	for _, unwanted := range []string{
		"$(S)/build/scripts/clang_wrapper.py",
		"$(S)/build/scripts/llvm_opt_wrapper.py",
	} {
		if nodeHasInput(pyNode, unwanted) {
			t.Errorf("PY objcopy node should not carry BC producer-closure input %q; inputs: %v",
				unwanted, vfsStringsT3(pyNode.flatInputs()))
		}
	}
}

func TestEmitLLVMBC_BCNodeGeneratedSourceClosure(t *testing.T) {
	const modPath = "mod/llvm"

	files := map[string]string{}
	addLLVMBCToolchainPeer(files)
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

	files[modPath+"/ya.make"] = `LIBRARY()
USE_LLVM_BC16()
COPY_FILE(TEXT gen.cpp.in gen.cpp)
LLVM_BC(
    gen.cpp
    NAME
    Gen
    SUFFIX .16
)
END()
`
	files[modPath+"/gen.cpp.in"] = "#include \"gen.h\"\nint Gen(){return 0;}\n"
	files[modPath+"/gen.h"] = "#pragma once\n"

	g := testGen(newMemFS(files), modPath)

	var bcNode *Node

	for _, n := range g.Graph {
		if p := n.KV.P.string(); p != "BC" {
			continue
		}

		for _, o := range n.Outputs {
			if strings.HasSuffix(o.string(), "gen.cpp.16.bc") {
				bcNode = n

				break
			}
		}

		if bcNode != nil {
			break
		}
	}

	if bcNode == nil {
		t.Fatal("graph missing BC node for gen.cpp.16.bc")
	}

	if len(bcNode.flatInputs()) == 0 {
		t.Fatal("BC node has no inputs")
	}

	hasGen := false

	for _, in := range bcNode.flatInputs() {
		s := in.string()

		if strings.HasPrefix(s, "$(B)/") && strings.HasSuffix(s, "gen.cpp") {
			hasGen = true
		}
	}

	if !hasGen {
		t.Errorf("BC node Inputs missing $(B)/.../gen.cpp: %v", vfsStringsT3(bcNode.flatInputs()))
	}

	if !nodeHasInput(bcNode, "$(S)/"+modPath+"/gen.h") {
		t.Errorf("BC node Inputs missing gen.h from closure: %v", vfsStringsT3(bcNode.flatInputs()))
	}
}

func TestGen_RunProgramConsumesLLVMBCOutput(t *testing.T) {
	const modPath = "mod/llvm"

	files := map[string]string{}
	addLLVMBCToolchainPeer(files)
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeToolProgram(files, "tools/gp", "gp")
	files[modPath+"/ya.make"] = `LIBRARY()
USE_LLVM_BC16()
LLVM_BC(
    foo.cpp
    NAME
    Bar
    SUFFIX .16
)
SRCS(foo.cpp)
RUN_PROGRAM(
    tools/gp ${BINDIR}/Bar_optimized.16.bc out.txt
    IN ${BINDIR}/Bar_optimized.16.bc
    OUT_NOAUTO out.txt
)
END()
`
	files[modPath+"/foo.cpp"] = "int Bar(){return 0;}\n"

	g := testGen(newMemFS(files), modPath)
	opt := mustNodeByAnyOutput(t, g, "$(B)/"+modPath+"/Bar_optimized.16.bc")
	consumer := mustNodeByOutput(t, g, "$(B)/"+modPath+"/out.txt")

	if !slices.Contains(graphDeps(g, consumer), opt.Ref) {
		t.Fatalf("out.txt deps missing LLVM_BC opt producer ref %d: %v", opt.Ref, graphDeps(g, consumer))
	}
}
