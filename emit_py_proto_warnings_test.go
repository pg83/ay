package main

import (
	"strings"
	"testing"
)

// sg7 py-proto aux warning-flag residual (T-76): the autoincluded
// linters.make.inc gates CLANG_WARNINGS(-Wimplicit-fallthrough) behind
// `IF (MODULE_LANG == CPP)`. Upstream evaluates that gate per submodule of a
// PROTO_LIBRARY multimodule: the _CPP_PROTO submodule has MODULE_LANG==CPP, so
// its generated .pb.cc compile receives the warning; the _PY3_PROTO submodule
// (module _PY3_PROTO: PY3_LIBRARY -> SET(MODULE_LANG PY3)) does not, so the
// optimized-py-proto resource aux C++ object must NOT carry it.
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
