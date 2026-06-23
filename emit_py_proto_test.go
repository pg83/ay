package main

import (
	encb64 "encoding/base64"
	"slices"
	"strings"
	"testing"
)

func TestEmitPyProto_GeneratedProtoBareTokenObjcopyRouting(t *testing.T) {
	const modPath = "irt/test/banner_flags"
	const consumer = "irt/test/app"

	files := map[string]string{
		consumer + "/ya.make": `PY3_LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PYTHON_INCLUDES()
PEERDIR(` + modPath + `)
END()
`,
		modPath + "/ya.make": `PROTO_LIBRARY()
NO_MYPY()
IF (GEN_PROTO)
RUN_PROGRAM(
    ` + modPath + `/gen
    STDOUT_NOAUTO banner_flags.proto
    OUTPUT_INCLUDES dep/markup.proto
)
ENDIF()
SRCS(banner_flags.proto)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`,
		"dep/markup.proto":                "syntax = \"proto3\";\nmessage Markup { int32 x = 1; }\n",
		"contrib/libs/protobuf/ya.make":   "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n",
		"contrib/python/protobuf/ya.make": "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\nEND()\n",
		"contrib/libs/python/ya.make":     "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n",
	}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "tools/py3cc", "py3cc")
	writeToolProgram(files, "tools/py3cc/slow", "slow")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeToolProgram(files, "tools/archiver", "archiver")
	writeToolProgram(files, modPath+"/gen", "gen")

	g := testGen(newMemFS(files), consumer)

	pyOut := "$(B)/" + modPath + "/banner_flags__intpy3___pb2.py"
	yapyc3 := pyOut + ".yapyc3"
	kqhj := pyOut + ".kqhj.yapyc3"

	for _, n := range g.Graph {
		if len(n.Outputs) == 0 {
			continue
		}

		o := n.Outputs[0].string()

		if strings.HasPrefix(o, "$(B)/"+modPath+"/") && strings.Contains(o, "_raw.auxcpp") {
			t.Fatalf("generated proto produced a rescompiler aux node %q; want objcopy resfs", o)
		}
	}

	if nodeByOutput(g, kqhj) != nil {
		t.Fatalf("py3cc emitted a path-id-suffixed yapyc3 %q; generated proto must be unsuffixed", kqhj)
	}

	py3cc := mustNodeByOutput(t, g, yapyc3)

	if first := prCmdArgStrings(py3cc); !slices.Contains(first, "banner_flags__intpy3___pb2.py-") {
		t.Fatalf("py3cc first token is not the bare `banner_flags__intpy3___pb2.py-`: %v", first)
	}

	mustNodeByOutput(t, g, "$(B)/"+modPath+"/banner_flags.proto")

	pb := mustNodeByOutput(t, g, pyOut)

	for _, n := range []*Node{pb, py3cc} {
		if !nodeHasInput(n, "$(S)/dep/markup.proto") {
			t.Fatalf("node producing %v is missing producer-closure input $(S)/dep/markup.proto: %v",
				n.Outputs, n.flatInputs())
		}
	}

	pb2Key := "resfs/file/py/" + modPath + "/banner_flags_pb2.py"
	yapKey := pb2Key + ".yapyc3"
	bare := []string{"banner_flags__intpy3___pb2.py", "banner_flags__intpy3___pb2.py.yapyc3"}
	keysB64 := []string{
		encb64.StdEncoding.EncodeToString([]byte(pb2Key)),
		encb64.StdEncoding.EncodeToString([]byte(yapKey)),
	}
	kvsHash := []string{
		"resfs/src/" + pb2Key + "=${rootrel;context=TEXT;input=TEXT:\"banner_flags__intpy3___pb2.py\"}",
		"resfs/src/" + yapKey + "=${rootrel;context=TEXT;input=TEXT:\"banner_flags__intpy3___pb2.py.yapyc3\"}",
	}
	wantHash := objcopyHash(bare, keysB64, kvsHash, modPath, stringPtr("PY3_PROTO"))
	wantObjcopy := "$(B)/" + modPath + "/objcopy_" + wantHash + ".o"

	objcopy := nodeByOutput(g, wantObjcopy)

	if objcopy == nil {
		t.Fatalf("graph is missing generated-proto objcopy %q\nobjcopy nodes: %v", wantObjcopy, objcopyOutputs(g))
	}

	if !nodeHasInput(objcopy, pyOut) || !nodeHasInput(objcopy, yapyc3) {
		t.Fatalf("objcopy inputs missing generated py/yapyc3 from $(B): %#v", objcopy.flatInputs())
	}

	deps := graphDeps(g, objcopy)

	if !slices.Contains(deps, pb.UID) {
		t.Fatalf("objcopy deps %v missing PB producer uid %q", deps, pb.UID)
	}

	if !slices.Contains(deps, py3cc.UID) {
		t.Fatalf("objcopy deps %v missing py3cc producer uid %q", deps, py3cc.UID)
	}

	globalAr := mustNodeByOutput(t, g, "$(B)/"+modPath+"/libpy3irt-test-banner_flags.global.a")
	arArgs := prCmdArgStrings(globalAr)

	if !slices.Contains(arArgs, wantObjcopy) {
		t.Fatalf("global archive does not link the generated-proto objcopy %q: %v", wantObjcopy, arArgs)
	}

	for _, a := range arArgs {
		if strings.Contains(a, "_raw.auxcpp") {
			t.Fatalf("global archive still links an auxcpp member %q for a generated proto", a)
		}
	}
}

func TestEmitPyProto_ProtocFatalWarningsRidesPyCommand(t *testing.T) {
	const fwPath = "sprav/test/protos"
	const plainPath = "sprav/test/plain"
	const consumer = "sprav/test/app"

	files := map[string]string{
		consumer + "/ya.make": `PY3_LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PYTHON_INCLUDES()
PEERDIR(` + fwPath + ` ` + plainPath + `)
END()
`,
		fwPath + "/ya.make": `PROTO_LIBRARY()
PROTOC_FATAL_WARNINGS()
SRCS(altay_api.proto)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`,
		plainPath + "/ya.make": `PROTO_LIBRARY()
SRCS(plain.proto)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`,
		fwPath + "/altay_api.proto":       "syntax = \"proto3\";\nmessage AltayApi { int32 x = 1; }\n",
		plainPath + "/plain.proto":        "syntax = \"proto3\";\nmessage Plain { int32 x = 1; }\n",
		"contrib/libs/protobuf/ya.make":   "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n",
		"contrib/python/protobuf/ya.make": "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\nEND()\n",
		"contrib/libs/python/ya.make":     "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n",
	}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "contrib/python/mypy-protobuf/bin/protoc-gen-mypy", "protoc-gen-mypy")
	writeToolProgram(files, "tools/py3cc", "py3cc")
	writeToolProgram(files, "tools/py3cc/slow", "slow")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeToolProgram(files, "tools/archiver", "archiver")

	g := testGen(newMemFS(files), consumer)

	fwPy := mustNodeByOutput(t, g, "$(B)/"+fwPath+"/altay_api__intpy3___pb2.py")
	args := prCmdArgStrings(fwPy)
	fwIdx := slices.Index(args, "--fatal_warnings")

	if fwIdx < 0 {
		t.Fatalf("py PB command for PROTOC_FATAL_WARNINGS module missing --fatal_warnings: %v", args)
	}

	var pyOutIdx, srcIdx int = -1, -1

	for i, a := range args {
		if strings.HasPrefix(a, "--python_out=") {
			pyOutIdx = i
		}

		if a == fwPath+"/altay_api.proto" && pyOutIdx >= 0 {
			srcIdx = i
		}
	}

	if !(pyOutIdx >= 0 && pyOutIdx < fwIdx) {
		t.Fatalf("--fatal_warnings (idx %d) must follow --python_out (idx %d): %v", fwIdx, pyOutIdx, args)
	}

	if !(srcIdx >= 0 && fwIdx < srcIdx) {
		t.Fatalf("--fatal_warnings (idx %d) must precede source token (idx %d): %v", fwIdx, srcIdx, args)
	}

	if mustNodeByAnyOutput(t, g, "$(B)/"+fwPath+"/altay_api__intpy3___pb2.pyi") != fwPy {
		t.Fatalf(".pyi output is not produced by the same PB node as the .py")
	}

	plainPy := mustNodeByOutput(t, g, "$(B)/"+plainPath+"/plain__intpy3___pb2.py")

	if slices.Contains(prCmdArgStrings(plainPy), "--fatal_warnings") {
		t.Fatalf("py PB command for non-macro module must NOT carry --fatal_warnings: %v", prCmdArgStrings(plainPy))
	}
}

func TestEmitPyProto_CheckedInProtoStaysOnRawAux(t *testing.T) {
	const modPath = "irt/test/checked"
	const consumer = "irt/test/checkedapp"

	files := map[string]string{
		consumer + "/ya.make": `PY3_LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PYTHON_INCLUDES()
PEERDIR(` + modPath + `)
END()
`,
		modPath + "/ya.make": `PROTO_LIBRARY()
NO_MYPY()
SRCS(foo.proto)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`,
		modPath + "/foo.proto":            "syntax = \"proto3\";\nmessage Foo { int32 x = 1; }\n",
		"contrib/libs/protobuf/ya.make":   "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n",
		"contrib/python/protobuf/ya.make": "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\nEND()\n",
		"contrib/libs/python/ya.make":     "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n",
	}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "tools/py3cc", "py3cc")
	writeToolProgram(files, "tools/py3cc/slow", "slow")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeToolProgram(files, "tools/archiver", "archiver")

	g := testGen(newMemFS(files), consumer)

	sawAux := false

	for _, n := range g.Graph {
		if len(n.Outputs) == 0 {
			continue
		}

		o := n.Outputs[0].string()

		if strings.HasPrefix(o, "$(B)/"+modPath+"/") && strings.HasSuffix(o, "_raw.auxcpp") {
			sawAux = true
		}

		if strings.HasPrefix(o, "$(B)/"+modPath+"/objcopy_") {
			t.Fatalf("checked-in proto routed to objcopy resfs %q; want _raw.auxcpp", o)
		}
	}

	if !sawAux {
		t.Fatal("checked-in proto did not produce the expected _raw.auxcpp resource node")
	}

	pyRel := "$(B)/" + modPath + "/foo__intpy3___pb2.py"
	suffix := protoPySuffix(modPath)

	if nodeByOutput(g, pyRel+"."+suffix+".yapyc3") == nil {
		t.Fatalf("checked-in proto missing path-id-suffixed yapyc3 %q", pyRel+"."+suffix+".yapyc3")
	}
}

func TestGen_PyProtoAux_ExcludesModuleLangCppClangWarnings(t *testing.T) {
	const modPath = "ads/bsyeti/test/proto"
	const consumer = "ads/bsyeti/test/app"

	files := map[string]string{
		"build/conf/autoincludes.json": `["ads"]`,
		"ads/linters.make.inc": `IF (MODULE_LANG == CPP)
    CLANG_WARNINGS(-Wimplicit-fallthrough)
ENDIF()
`,
		consumer + "/ya.make": `PY3_LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PYTHON_INCLUDES()
PEERDIR(` + modPath + `)
END()
`,
		modPath + "/ya.make": `PROTO_LIBRARY()
NO_MYPY()
SRCS(foo.proto)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`,
		modPath + "/foo.proto":            "syntax = \"proto3\";\nmessage Foo { int32 x = 1; }\n",
		"contrib/libs/protobuf/ya.make":   "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n",
		"contrib/python/protobuf/ya.make": "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\nEND()\n",
		"contrib/libs/python/ya.make":     "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n",
	}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "tools/py3cc", "py3cc")
	writeToolProgram(files, "tools/py3cc/slow", "py3cc_slow")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")

	g := testGen(newMemFS(files), consumer)

	var auxCC *Node
	var pbCC *Node

	for _, n := range g.Graph {
		if n.KV.P != pkCC || len(n.Outputs) == 0 {
			continue
		}

		out := n.Outputs[0].string()

		if strings.Contains(out, "_raw.auxcpp.py3") && strings.HasSuffix(out, ".o") {
			auxCC = n
		}

		if strings.HasSuffix(out, "foo.pb.cc.o") {
			pbCC = n
		}
	}

	if auxCC == nil {
		t.Fatal("no py-proto aux CC node (*_raw.auxcpp.py3.pic.o) emitted")
	}

	if pbCC == nil {
		t.Fatal("no CPP proto CC node (foo.pb.cc.o) emitted")
	}

	flagArgs := func(n *Node) []string {
		var args []string

		for _, c := range n.Cmds {
			args = append(args, strStrs(c.CmdArgs.flat())...)
		}

		return args
	}

	auxHas := false

	for _, a := range flagArgs(auxCC) {
		if a == "-Wimplicit-fallthrough" {
			auxHas = true
		}
	}

	if auxHas {
		t.Errorf("py-proto aux compile must NOT carry -Wimplicit-fallthrough (MODULE_LANG==PY3 for _PY3_PROTO); args=%v", flagArgs(auxCC))
	}

	pbHas := false

	for _, a := range flagArgs(pbCC) {
		if a == "-Wimplicit-fallthrough" {
			pbHas = true
		}
	}

	if !pbHas {
		t.Errorf("CPP proto .pb.cc compile must still carry -Wimplicit-fallthrough (MODULE_LANG==CPP for _CPP_PROTO); args=%v", flagArgs(pbCC))
	}
}
