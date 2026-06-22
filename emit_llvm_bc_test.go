package main

import (
	"strings"
	"testing"
)

// addLLVMBCToolchainPeer injects the synthetic clang RESOURCES_LIBRARY every C++
// module implicitly PEERDIRs, so the toolchain resource-global reaches the closure
// — the value emitLLVMBC resolves CLANG_BC_ROOT against.
func addLLVMBCToolchainPeer(files map[string]string) {
	files["build/platform/clang/ya.make"] = "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON(CLANG16 clang16.json)\nEND()\n"
	files["build/platform/clang/clang16.json"] = `{"by_platform":{"linux-x86_64":{"uri":"sbr:test-clang16"}}}`
}

// TestEmitLLVMBC_OptPassesNoBraceComma: the OP node's -passes arg uses literal
// commas (not ${__COMMA__}) with no spurious outer single-quotes.
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

// TestEmitLLVMBC_BCNodeIncludesCompileFlags: BC compile nodes include the standard
// include paths and defines (like a full CC compile), not just bare -emit-llvm.
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

// TestEmitLLVMBC_PipelineProducesFiveNodes: USE_LLVM_BC16 + LLVM_BC emits the
// 5-step pipeline (BC compile, LD link, OP opt, PY objcopy, AR global archive);
// asserts all 5 nodes reachable from the LIBRARY's archive root.
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

// TestEmitLLVMBC_BCNodeIncludesArchArgs: the BC compile command carries the
// platform arch flag (-march=armv8-a for AArch64) between --target=... and -B/usr/bin.
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

// TestEmitLLVMBC_BCNodeCarriesIncludeClosure: the BC compile node carries the full
// transitive include closure in Inputs so a header change retriggers the compile.
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

	// BC node must carry the source plus its include closure (foo.h).
	if len(bcNode.flatInputs()) < 2 {
		t.Errorf("BC node Inputs has only %d entries; want source + closure: %v", len(bcNode.flatInputs()), vfsStringsT3(bcNode.flatInputs()))
	}

	// Chunked inputs have no positional contract; the source rides the window.
	if !nodeHasInput(bcNode, "$(S)/"+modPath+"/foo.cpp") {
		t.Errorf("BC node Inputs missing the source foo.cpp: %v", vfsStringsT3(bcNode.flatInputs()))
	}

	if !nodeHasInput(bcNode, "$(S)/"+modPath+"/foo.h") {
		t.Errorf("BC node Inputs missing foo.h from include closure: %v", vfsStringsT3(bcNode.flatInputs()))
	}
}

// TestEmitLLVMBC_ObjcopyNodeCarriesBCClosure pins that the PY objcopy node for the
// LLVM_BC resource embed does NOT carry the BC producer-closure scripts.
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

	// The objcopy.py action reads only the embedded .bc; the BC source closure
	// (wrapper scripts, headers) is NOT an input of the objcopy node.
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

// TestEmitLLVMBC_BCNodeGeneratedSourceClosure: a BC source produced by COPY_FILE(TEXT)
// compiles the build-root copy as its primary input and carries its include closure.
func TestEmitLLVMBC_BCNodeGeneratedSourceClosure(t *testing.T) {
	const modPath = "mod/llvm"

	files := map[string]string{}
	addLLVMBCToolchainPeer(files)
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	// COPY_FILE(TEXT src.in dst) creates a build-root generated source; the scanner
	// registers src.in's includes for $(B)/mod/dst so walkClosure returns its headers.
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

	// Primary input must be the build-root copy, not the source.
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
