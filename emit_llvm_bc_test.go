package main

import (
	"strings"
	"testing"
)

// addLLVMBCToolchainPeer injects a synthetic build/platform/clang RESOURCES_LIBRARY
// (the peer every C++ module implicitly PEERDIRs) into the in-memory fixture, so
// CLANG16_RESOURCE_GLOBAL reaches the module's resource-global closure — the peerdir
// value emitLLVMBC resolves CLANG_BC_ROOT against. Self-contained: no real FS access;
// the uri is a fixture token, and the clang++ path is not asserted by these tests.
func addLLVMBCToolchainPeer(files map[string]string) {
	files["build/platform/clang/ya.make"] = "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON(CLANG16 clang16.json)\nEND()\n"
	files["build/platform/clang/clang16.json"] = `{"by_platform":{"linux-x86_64":{"uri":"sbr:test-clang16"}}}`
}

// TestEmitLLVMBC_OptPassesNoBraceComma verifies that the OP node's -passes arg
// uses literal commas (not ${__COMMA__}) and has no spurious outer single-quotes.
// Upstream: ymake expands ${__COMMA__} → , and strips shell-quoting before
// writing graph JSON. Our code must emit the already-expanded form.
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
		if p := n.KV.P.String(); p != "OP" {
			continue
		}
		for _, o := range n.Outputs {
			if strings.Contains(o.String(), "Bar_optimized") {
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
	args := strStrs(opNode.Cmds[0].CmdArgs)
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
	// Must contain commas as separator between passes.
	if !strings.Contains(passesArg, ",") {
		t.Errorf("-passes= arg has no comma separators: %q", passesArg)
	}
	want := `-passes="default<O2>,globalopt,globaldce,internalize"`
	if passesArg != want {
		t.Errorf("-passes= arg = %q, want %q", passesArg, want)
	}
}

// TestEmitLLVMBC_BCNodeIncludesCompileFlags verifies that BC compile nodes include
// the standard include paths and defines (like a full CC compile), not just the
// bare -emit-llvm -c src -o out. Upstream LLVM_COMPILE_CXX macro includes
// ${pre=-I:_C__INCLUDE} $BC_CXXFLAGS which is the full CXXFLAGS set.
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
		if p := n.KV.P.String(); p != "BC" {
			continue
		}
		for _, o := range n.Outputs {
			if strings.HasSuffix(o.String(), "foo.cpp.16.bc") {
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
	args := strStrs(bcNode.Cmds[0].CmdArgs)

	// Must include standard include paths from ccIncludesPrefix.
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

	// Must include ARCADIA_ROOT define (from hostDefines / $BC_CXXFLAGS).
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

// TestEmitLLVMBC_PipelineProducesFiveNodes reproduces the G3 yt codec llvm16
// gap: USE_LLVM_BC16 + LLVM_BC parses (modules.go:1029) but emission is
// missing. Upstream `build/plugins/llvm_bc.py` drives the 5-step pipeline:
//   - llvm_compile_cxx  → $(B)/<src>.<suffix>.bc                kv.p=BC
//   - llvm_link         → $(B)/<mod>/<NAME>_merged.<suffix>.bc  kv.p=LD
//   - llvm_opt          → $(B)/<mod>/<NAME>_optimized.<suffix>.bc kv.p=OP
//   - onresource([out_bc, '/llvm_bc/'+NAME]) ⇒
//     objcopy_<hash>.o   kv.p=PY  (handled by existing emitResourceObjcopy)
//     lib<mod>.global.a  kv.p=AR  (handled by existing global-archive flow)
//
// Test asserts all 5 nodes reachable from the LIBRARY's archive root.
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
			byOut[o.String()] = n
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
		if got := n.KV.P.String(); got != kvp {
			t.Errorf("output %q kv.p = %q, want %q", path, got, kvp)
		}
	}

	var pyNode *Node
	for _, n := range g.Graph {
		if got := n.KV.P.String(); got != "PY" {
			continue
		}
		for _, o := range n.Outputs {
			if strings.HasPrefix(o.String(), "$(B)/"+modPath+"/objcopy_") &&
				strings.HasSuffix(o.String(), ".o") {
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
			if in.String() == "$(B)/"+modPath+"/Bar_optimized.16.bc" {
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
		if got := n.KV.P.String(); got != "AR" {
			continue
		}
		for _, o := range n.Outputs {
			if strings.HasSuffix(o.String(), ".global.a") {
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

// TestEmitLLVMBC_BCNodeIncludesArchArgs verifies that the BC compile command
// carries $C_FLAGS_PLATFORM arch flags (e.g. -march=armv8-a for AArch64).
// testGen targets AArch64, so bundle.ArchArgs = ["-march=armv8-a"]; the BC
// command must include that flag between --target=... and -B/usr/bin.
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
		if p := n.KV.P.String(); p != "BC" {
			continue
		}
		for _, o := range n.Outputs {
			if strings.HasSuffix(o.String(), "foo.cpp.16.bc") {
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
	args := strStrs(bcNode.Cmds[0].CmdArgs)

	// Must carry -march=armv8-a (AArch64 platform; testGen targets AArch64).
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

	// The order must be: --target=... then -march=... then -B/usr/bin.
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

// TestEmitLLVMBC_BCNodeCarriesIncludeClosure verifies that the BC compile node
// carries the full transitive include closure in Inputs, not just the single
// source file. Upstream emits all header dependencies as direct node inputs so
// any header change retriggers the BC compile. Our prior code used
// Inputs: inputChunks{[]VFS{inputVFS} — only the source — which diverged from upstream.
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
	// foo.cpp includes foo.h; the closure must carry foo.h as an input.
	files[modPath+"/foo.cpp"] = "#include \"foo.h\"\nint Bar(){return 0;}\n"
	files[modPath+"/foo.h"] = "#pragma once\n"

	g := testGen(newMemFS(files), modPath)

	var bcNode *Node
	for _, n := range g.Graph {
		if p := n.KV.P.String(); p != "BC" {
			continue
		}
		for _, o := range n.Outputs {
			if strings.HasSuffix(o.String(), "foo.cpp.16.bc") {
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

	// BC node must carry the source file plus its include closure (foo.h).
	// Before fix: Inputs was [foo.cpp] only — len == 1.
	if len(bcNode.flatInputs()) < 2 {
		t.Errorf("BC node Inputs has only %d entries; want source + closure: %v", len(bcNode.flatInputs()), vfsStringsT3(bcNode.flatInputs()))
	}

	// First input must be the source file.
	if !strings.HasSuffix(bcNode.flatInputs()[0].String(), "foo.cpp") {
		t.Errorf("BC node first input is not foo.cpp: %v", bcNode.flatInputs()[0])
	}

	// Closure must include foo.h.
	if !nodeHasInput(bcNode, "$(S)/"+modPath+"/foo.h") {
		t.Errorf("BC node Inputs missing foo.h from include closure: %v", vfsStringsT3(bcNode.flatInputs()))
	}
}

// TestEmitLLVMBC_ObjcopyNodeCarriesBCClosure verifies that the PY objcopy node
// for the LLVM_BC resource embed carries the BC compilation closure scripts as
// inputs, matching upstream's flat input propagation. Upstream ymake propagates
// the OP node's inputs (which include the clang_wrapper.py and llvm_opt_wrapper.py
// scripts plus all closure headers) into the PY objcopy node. In our code this
// propagation flows through d.prOutputInputs, which emitLLVMBC must populate.
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

	// Find the PY objcopy node that embeds the LLVM BC output.
	var pyNode *Node
	for _, n := range g.Graph {
		if p := n.KV.P.String(); p != "PY" {
			continue
		}
		for _, o := range n.Outputs {
			if strings.HasPrefix(o.String(), "$(B)/"+modPath+"/objcopy_") &&
				strings.HasSuffix(o.String(), ".o") {
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

	// The PY node must carry clang_wrapper.py (from BC inputs) and
	// llvm_opt_wrapper.py (from OP inputs) as part of the propagated closure.
	// Before fix: d.prOutputInputs[optOutName] is never set, so prResourceExtraInputs
	// returns nil and neither script appears in the PY node's inputs.
	wantInputs := []string{
		"$(S)/build/scripts/clang_wrapper.py",
		"$(S)/build/scripts/llvm_opt_wrapper.py",
	}
	for _, want := range wantInputs {
		if !nodeHasInput(pyNode, want) {
			t.Errorf("PY objcopy node missing BC closure input %q; inputs: %v",
				want, vfsStringsT3(pyNode.flatInputs()))
		}
	}
}

// TestEmitLLVMBC_BCNodeGeneratedSourceClosure verifies that a BC source produced
// by COPY_FILE(TEXT) (i.e. a build-root generated source like yt_codec_bc.cpp)
// compiles the build-root copy as its primary input and carries its include
// closure — both matching the upstream LLVM_COMPILE_CXX node shape.
func TestEmitLLVMBC_BCNodeGeneratedSourceClosure(t *testing.T) {
	const modPath = "mod/llvm"

	files := map[string]string{}
	addLLVMBCToolchainPeer(files)
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	// COPY_FILE(TEXT src.in dst) creates a build-root generated source.
	// The scanner registers src.in's parsed includes for $(B)/mod/dst,
	// so walkClosure on $(B)/mod/dst returns the transitive header set.
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
		if p := n.KV.P.String(); p != "BC" {
			continue
		}
		for _, o := range n.Outputs {
			if strings.HasSuffix(o.String(), "gen.cpp.16.bc") {
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

	// Primary input must be the build-root generated copy, not the source.
	if len(bcNode.flatInputs()) == 0 {
		t.Fatal("BC node has no inputs")
	}
	primaryInput := bcNode.flatInputs()[0].String()
	if !strings.HasPrefix(primaryInput, "$(B)/") || !strings.HasSuffix(primaryInput, "gen.cpp") {
		t.Errorf("BC node primary input is not $(B)/.../gen.cpp: %q", primaryInput)
	}

	// BC node must also carry gen.h from the closure.
	if !nodeHasInput(bcNode, "$(S)/"+modPath+"/gen.h") {
		t.Errorf("BC node Inputs missing gen.h from closure: %v", vfsStringsT3(bcNode.flatInputs()))
	}
}
