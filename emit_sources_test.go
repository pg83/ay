package main

import (
	"slices"
	"strings"
	"testing"
)

type emitBuildSourceCase struct {
	name    string
	source  string
	output  string
	kind    ProcKind
	extra   string
	x86     bool
	prepare func(map[string]string)
}

func TestEmitOneSource_BuildCompileInputs(t *testing.T) {
	cases := []emitBuildSourceCase{
		{name: "cc", source: "generated.cpp", output: "generated.cpp.o", kind: pkCC},
		{name: "rodata", source: "generated.rodata", output: "generated.rodata.o", kind: pkRD, x86: true, prepare: prepareBuildYasmTool},
		{name: "asm", source: "generated.S", output: "generated.S.o", kind: pkAS},
		{name: "yasm", source: "generated.asm", output: "generated.o", kind: pkAS, prepare: prepareBuildYasmTool},
		{name: "cuda", source: "generated.cu", output: "generated.cu.o", kind: pkCU, prepare: func(files map[string]string) {
			writeToolProgram(files, "tools/mtime0", "mtime0")
			writeToolProgram(files, "tools/custom_pid", "custom_pid")
			writeTestModuleFile(files, "build/scripts/compile_cuda.py", "# stub\n")
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := genBuildSourceGraph(t, "LIBRARY()", tc.source, "", tc.x86, tc.prepare)
			producer := buildSourceNodeByOutput(t, g, "$(B)/mod/"+tc.source)
			consumer := buildSourceNodeByOutput(t, g, "$(B)/mod/"+tc.output)

			if consumer.KV.P != tc.kind {
				t.Fatalf("consumer kind = %s, want %s", consumer.KV.P, tc.kind)
			}

			assertBuildInputEdge(t, g, producer, consumer)
		})
	}
}

func TestEmitOneSource_BuildRagel6Input(t *testing.T) {
	g := genBuildSourceGraph(t, "LIBRARY()", "generated.rl6", "", false, func(files map[string]string) {
		writeToolProgram(files, "contrib/tools/ragel6", "ragel6")
	})
	producer := buildSourceNodeByOutput(t, g, "$(B)/mod/generated.rl6")
	r6 := buildSourceNodeByOutput(t, g, "$(B)/mod/generated.rl6.cpp")

	if r6.KV.P != pkR6 {
		t.Fatalf("consumer kind = %s, want %s", r6.KV.P, pkR6)
	}

	assertBuildInputEdge(t, g, producer, r6)
}

func TestEmitOneSource_BuildGeneratorInputs(t *testing.T) {
	cases := []emitBuildSourceCase{
		{name: "bison", source: "generated.y", output: "generated.y.cpp", kind: pkYC, prepare: func(files map[string]string) {
			writeBisonProducer(files)
		}},
		{name: "ragel5", source: "generated.rl", output: "generated.rl5.cpp", kind: pkR5, prepare: func(files map[string]string) {
			writeToolProgram(files, "contrib/tools/ragel5/ragel", "ragel")
			writeToolProgram(files, "contrib/tools/ragel5/rlgen-cd", "rlgen-cd")
		}},
		{name: "fml", source: "generated.fml", output: "generated.fml.inc", kind: pkFM, extra: "SRCS(consumer.cpp)", prepare: prepareGeneratedHeaderConsumer("generated.fml.inc", func(files map[string]string) {
			writeToolProgram(files, "tools/relev_fml_codegen", "relev_fml_codegen")
		})},
		{name: "sfdl", source: "generated.sfdl", output: "generated", kind: pkSF, extra: "SRCS(consumer.cpp)", prepare: prepareGeneratedHeaderConsumer("generated", func(files map[string]string) {
			writeToolProgram(files, "tools/calcstaticopt", "calcstaticopt")
		})},
		{name: "asp", source: "generated.asp", output: "generated.asp.cpp", kind: pkHT, prepare: func(files map[string]string) {
			writeToolProgram(files, "tools/html2cpp", "html2cpp")
		}},
		{name: "flex", source: "generated.lpp", output: "generated.lpp.cpp", kind: pkLX, prepare: func(files map[string]string) {
			writeToolProgram(files, "contrib/tools/flex-old", "flex")
		}},
		{name: "h_in", source: "generated.h.in", output: "generated.h", kind: pkCF, extra: "SRCS(consumer.cpp)", prepare: prepareGeneratedHeaderConsumer("generated.h", nil)},
		{name: "cpp_in", source: "generated.cpp.in", output: "generated.cpp", kind: pkCF},
		{name: "c_in", source: "generated.c.in", output: "generated.c", kind: pkCF},
		{name: "sc", source: "generated.sc", output: "generated.sc.h", kind: pkSC, extra: "SRCS(consumer.cpp)", prepare: prepareGeneratedHeaderConsumer("generated.sc.h", func(files map[string]string) {
			writeToolProgram(files, "tools/domschemec", "domschemec")
			writeTestModuleFile(files, "library/cpp/domscheme/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(domscheme.cpp)\nEND()\n")
			writeTestModuleFile(files, "library/cpp/domscheme/domscheme.cpp", "int domscheme() { return 0; }\n")
			writeTestModuleFile(files, "library/cpp/domscheme/runtime.h", "#pragma once\n")
		})},
		{name: "gperf", source: "generated.gperf", output: "generated.gperf.cpp", kind: pkGP, prepare: func(files map[string]string) {
			writeToolProgram(files, "contrib/tools/gperf", "gperf")
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			extra := "ADDINCL(${ARCADIA_BUILD_ROOT}/mod)\n" + tc.extra
			g := genBuildSourceGraph(t, "LIBRARY()", tc.source, extra, false, tc.prepare)
			producer := buildSourceNodeByOutput(t, g, "$(B)/mod/"+tc.source)
			generator := buildSourceNodeByOutput(t, g, "$(B)/mod/"+tc.output)

			if generator.KV.P != tc.kind {
				t.Fatalf("generator kind = %s, want %s", generator.KV.P, tc.kind)
			}

			assertBuildInputEdge(t, g, producer, generator)
		})
	}
}

func TestEmitOneSource_BuildSchemaInputs(t *testing.T) {
	cases := []emitBuildSourceCase{
		{name: "proto", source: "generated.proto", output: "generated.pb.h", kind: pkPB, prepare: prepareBuildProtoTools},
		{name: "ev", source: "generated.ev", output: "generated.ev.pb.h", kind: pkEV, prepare: func(files map[string]string) {
			prepareBuildProtoTools(files)
			writeToolProgram(files, "tools/event2cpp", "event2cpp")
			writeTestModuleFile(files, "library/cpp/eventlog/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
		}},
		{name: "cfgproto", source: "generated.cfgproto", output: "generated.cfgproto.pb.h", kind: pkPB, prepare: func(files map[string]string) {
			prepareBuildProtoTools(files)
			writeToolProgram(files, "library/cpp/proto_config/plugin", "plugin")
			writeTestModuleFile(files, "library/cpp/proto_config/codegen/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(codegen.cpp)\nEND()\n")
			writeTestModuleFile(files, "library/cpp/proto_config/codegen/codegen.cpp", "int codegen() { return 0; }\n")
			writeTestModuleFile(files, "library/cpp/proto_config/protos/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
		}},
		{name: "fbs", source: "generated.fbs", output: "generated.fbs.h", kind: pkFL, prepare: prepareBuildFlatcTools(false)},
		{name: "fbs64", source: "generated.fbs64", output: "generated.fbs64.h", kind: pkFL64, prepare: prepareBuildFlatcTools(true)},
		{name: "gztproto", source: "generated.gztproto", output: "generated.proto", kind: pkGZ, prepare: func(files map[string]string) {
			prepareBuildProtoTools(files)
			writeToolProgram(files, "dict/gazetteer/converter", "gztconverter")
			writeTestModuleFile(files, "kernel/gazetteer/proto/ya.make", "PROTO_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\nEND()\n")
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := genBuildSourceGraph(t, "PROTO_LIBRARY()", tc.source, "EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)", false, tc.prepare)
			producer := buildSourceNodeByOutput(t, g, "$(B)/mod/"+tc.source)
			generator := buildSourceNodeByOutput(t, g, "$(B)/mod/"+tc.output)

			if generator.KV.P != tc.kind {
				t.Fatalf("generator kind = %s, want %s", generator.KV.P, tc.kind)
			}

			assertBuildInputEdge(t, g, producer, generator)
		})
	}
}

func TestEmitOneSource_BuildGoInput(t *testing.T) {
	g := genBuildSourceGraph(t, "GO_LIBRARY()", "generated.go", "", false, prepareBuildGoTools)
	producer := buildSourceNodeByOutput(t, g, "$(B)/mod/generated.go")
	consumer := buildSourceConsumer(t, g, producer.Outputs[0].string(), pkGO)

	assertBuildInputEdge(t, g, producer, consumer)
}

func TestEmitOneSource_BuildGoAsmInput(t *testing.T) {
	files := map[string]string{}

	prepareBuildGoTools(files)
	writeToolProgram(files, "mod/gen", "gen")
	writeTestModuleFile(files, "mod/stub.go", "package mod\n")
	writeTestModuleFile(files, "mod/ya.make", `GO_LIBRARY()
RUN_PROGRAM(
    mod/gen
    STDOUT_NOAUTO generated.s
)
SRCS(
    stub.go
    ${ARCADIA_BUILD_ROOT}/mod/generated.s
)
END()
`)

	g := testGen(newMemFS(files), "mod")
	producer := buildSourceNodeByOutput(t, g, "$(B)/mod/generated.s")
	consumer := buildSourceConsumer(t, g, producer.Outputs[0].string(), pkGO)

	assertBuildInputEdge(t, g, producer, consumer)
}

func TestEmitOneSource_BuildHeaderIsAccepted(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "mod/gen", "gen")
	writeTestModuleFile(files, "mod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    mod/gen
    STDOUT_NOAUTO generated.h
)
SRCS(${ARCADIA_BUILD_ROOT}/mod/generated.h)
END()
`)

	_, warns := testGenWarns(newMemFS(files), "mod")

	for _, warn := range warns {
		if warn.Kind == WarnUnsupportedSource || warn.Kind == WarnMissingProducer {
			t.Fatalf("rooted generated header was not accepted: %+v", warn)
		}
	}
}

func TestEmitOneSource_BuildGoIsIgnoredByCppModule(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "mod/gen", "gen")
	writeTestModuleFile(files, "mod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    mod/gen
    STDOUT_NOAUTO generated.go
)
SRCS(${ARCADIA_BUILD_ROOT}/mod/generated.go)
END()
`)

	_, warns := testGenWarns(newMemFS(files), "mod")

	for _, warn := range warns {
		if warn.Kind == WarnUnsupportedSource || warn.Kind == WarnMissingProducer {
			t.Fatalf("rooted Go source in a C++ module was not ignored: %+v", warn)
		}
	}
}

func TestEmitOneSource_BuildUnsupportedSourceWarnsWithRootedPath(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "mod/gen", "gen")
	writeTestModuleFile(files, "mod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    mod/gen
    STDOUT_NOAUTO generated.txt
)
SRCS(${ARCADIA_BUILD_ROOT}/mod/generated.txt)
END()
`)

	_, warns := testGenWarns(newMemFS(files), "mod")
	found := false

	for _, warn := range warns {
		if warn.Kind == WarnUnsupportedSource && strings.Contains(warn.Message, "$(B)/mod/generated.txt") {
			found = true
		}
	}

	if !found {
		t.Fatalf("warnings do not identify the rooted unsupported source: %+v", warns)
	}
}

func TestEmitOneSource_BuildPyInput(t *testing.T) {
	files := map[string]string{}

	prepareBuildPyTools(files)
	writeToolProgram(files, "mod/gen", "gen")
	writeTestModuleFile(files, "mod/ya.make", `PY3_LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PYTHON_INCLUDES()
RUN_PROGRAM(
    mod/gen
    STDOUT_NOAUTO generated.py
)
PY_SRCS(${ARCADIA_BUILD_ROOT}/mod/generated.py)
END()
`)

	g := testGen(newMemFS(files), "mod")
	producer := buildSourceNodeByOutput(t, g, "$(B)/mod/generated.py")
	py3cc := buildSourceNodeByOutput(t, g, "$(B)/mod/generated.py.yapyc3")

	assertBuildInputEdge(t, g, producer, py3cc)
}

func TestEmitOneSource_BuildPyProtoInput(t *testing.T) {
	files := map[string]string{}

	prepareBuildPyTools(files)
	prepareBuildProtoTools(files)
	writeToolProgram(files, "mod/gen", "gen")
	writeTestModuleFile(files, "contrib/python/protobuf/ya.make", "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/python/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeTestModuleFile(files, "consumer/ya.make", `PY3_LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PYTHON_INCLUDES()
PEERDIR(mod)
END()
`)
	writeTestModuleFile(files, "mod/ya.make", `PROTO_LIBRARY()
NO_MYPY()
IF (GEN_PROTO)
RUN_PROGRAM(
    mod/gen
    STDOUT_NOAUTO generated.proto
)
ENDIF()
SRCS(${ARCADIA_BUILD_ROOT}/mod/generated.proto)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`)

	g := testGen(newMemFS(files), "consumer")
	producer := buildSourceNodeByOutput(t, g, "$(B)/mod/generated.proto")
	pyProto := buildSourceNodeByOutput(t, g, "$(B)/mod/generated__intpy3___pb2.py")

	assertBuildInputEdge(t, g, producer, pyProto)
}

func buildSourceNodeByOutput(t *testing.T, g *Graph, output string) *Node {
	t.Helper()

	for _, node := range g.Graph {
		if node == nil {
			continue
		}

		for _, out := range node.Outputs {
			if out.string() == output {
				return node
			}
		}
	}

	var outputs []string

	for _, node := range g.Graph {
		if node != nil {
			outputs = append(outputs, vfsStrings(node.Outputs)...)
		}
	}

	t.Fatalf("graph is missing output %q; outputs=%v", output, outputs)

	return nil
}

func genBuildSourceGraph(t *testing.T, module string, sourceRel, extra string, x86 bool, prepare func(map[string]string)) *Graph {
	t.Helper()

	files := map[string]string{}

	writeToolProgram(files, "mod/gen", "gen")

	if prepare != nil {
		prepare(files)
	}

	writeTestModuleFile(files, "mod/ya.make", module+`
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    mod/gen
    STDOUT_NOAUTO `+sourceRel+`
)
SRCS(${ARCADIA_BUILD_ROOT}/mod/`+sourceRel+`)
`+extra+`
END()
`)

	fs := newMemFS(files)

	if x86 {
		return testGenX86(fs, "mod")
	}

	return testGen(fs, "mod")
}

func prepareGeneratedHeaderConsumer(header string, prepare func(map[string]string)) func(map[string]string) {
	return func(files map[string]string) {
		if prepare != nil {
			prepare(files)
		}

		writeTestModuleFile(files, "mod/consumer.cpp", "#include \""+header+"\"\nint consumer() { return 0; }\n")
	}
}

func prepareBuildProtoTools(files map[string]string) {
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "# stub\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf() { return 0; }\n")
}

func prepareBuildPyTools(files map[string]string) {
	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/py3cc", "py3cc")
	writeToolProgram(files, "tools/py3cc/slow", "slow")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeToolProgram(files, "tools/archiver", "archiver")
}

func prepareBuildGoTools(files map[string]string) {
	writeTestModuleFile(files, goToolsPeer+"/ya.make", "RESOURCES_LIBRARY()\nEND()\n")
	writeTestModuleFile(files, goYolintPeer+"/ya.make", "RESOURCES_LIBRARY()\nEND()\n")
}

func prepareBuildFlatcTools(is64 bool) func(map[string]string) {
	return func(files map[string]string) {
		root := "contrib/libs/flatbuffers"

		if is64 {
			root = "contrib/libs/flatbuffers64"
		}

		writeToolProgram(files, root+"/flatc", "flatc")
		writeTestModuleFile(files, root+"/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(runtime.cpp)\nEND()\n")
		writeTestModuleFile(files, root+"/runtime.cpp", "int runtime() { return 0; }\n")
		writeTestModuleFile(files, root+"/include/flatbuffers/flatbuffers.h", "#pragma once\n")
		writeTestModuleFile(files, "build/scripts/cpp_flatc_wrapper.py", "# stub\n")
	}
}

func assertBuildInputEdge(t *testing.T, g *Graph, producer, consumer *Node) {
	t.Helper()

	input := producer.Outputs[0].string()

	if !nodeHasInput(consumer, input) {
		t.Fatalf("%s node does not consume rooted build source %s: %#v", consumer.KV.P, input, consumer.flatInputs())
	}

	if !slices.Contains(graphDeps(g, consumer), producer.Ref) {
		t.Fatalf("%s deps missing generated source producer %d: %v", consumer.KV.P, producer.Ref, graphDeps(g, consumer))
	}
}

func buildSourceConsumer(t *testing.T, g *Graph, input string, kind ProcKind) *Node {
	t.Helper()

	for _, node := range g.Graph {
		if node != nil && node.KV.P == kind && nodeHasInput(node, input) {
			return node
		}
	}

	t.Fatalf("graph has no %s node consuming %s", kind, input)

	return nil
}

func prepareBuildYasmTool(files map[string]string) {
	writeToolProgram(files, "contrib/tools/yasm", "yasm")
}
