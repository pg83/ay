package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestGen_BisonGeneratedHeaderPreprocessAndPeerBuildRootInclude(t *testing.T) {
	files := map[string]string{}
	addToolchainPeers(files)

	writeBisonTool(files)
	writeToolProgram(files, "contrib/tools/m4", "m4")
	writeTestModuleFile(files, bisonPreprocessPyVFS.rel(), "print('stub')\n")

	for _, input := range bisonCppSkeletonInputs {
		body := ""

		if strings.HasSuffix(input.rel(), "/stack.hh") {
			body = `#include "skeleton-helper.h"` + "\n"
		}

		writeTestModuleFile(files, input.rel(), body)
	}

	writeTestModuleFile(files, "contrib/tools/bison/data/skeletons/skeleton-helper.h", "")

	writeTestModuleFile(files, "genlib/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(pire/re_parser.y)
END()
`)
	writeTestModuleFile(files, "genlib/pire/re_parser.y", `%{
#include "re_lexer.h"
#include "extra.h"
%}
%%
`)
	writeTestModuleFile(files, "genlib/pire/re_lexer.h", `#pragma once
#include "deep.h"
`)
	writeTestModuleFile(files, "genlib/pire/extra.h", "#pragma once\n")
	writeTestModuleFile(files, "genlib/pire/deep.h", "#pragma once\n")

	writeTestModuleFile(files, "app/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(genlib)
SRCS(use.cpp)
END()
`)
	writeTestModuleFile(files, "app/use.cpp", "int use() { return 0; }\n")

	g := testGen(newMemFS(files), "app")

	yc := mustNodeByOutput(t, g, "$(B)/genlib/pire/re_parser.h")

	if got := len(yc.Cmds); got != 2 {
		t.Fatalf("bison YC cmd count = %d, want 2", got)
	}

	if !strings.HasSuffix(yc.Cmds[1].CmdArgs.flat()[0].string(), "/python3") {
		t.Fatalf("bison preprocess tool = %q, want a python3 binary", yc.Cmds[1].CmdArgs.flat()[0])
	}

	wantPreprocess := []string{
		"$(S)/build/scripts/preprocess.py",
		"$(B)/genlib/pire/re_parser.h",
	}

	if got := strStrs(yc.Cmds[1].CmdArgs.flat()[1:]); !reflect.DeepEqual(got, wantPreprocess) {
		t.Fatalf("bison preprocess cmd_args mismatch:\n  got:  %#v\n  want: %#v", got, wantPreprocess)
	}

	for _, want := range []string{
		"$(S)/build/scripts/preprocess.py",
		"$(S)/genlib/pire/re_parser.y",
		"$(S)/contrib/tools/bison/data/skeletons/skeleton-helper.h",
	} {
		if !nodeHasInput(yc, want) {
			t.Fatalf("bison YC inputs missing %q: %#v", want, yc.flatInputs())
		}
	}

	for _, unwanted := range []string{
		"$(S)/genlib/pire/re_lexer.h",
		"$(S)/genlib/pire/extra.h",
		"$(S)/genlib/pire/deep.h",
	} {
		if nodeHasInput(yc, unwanted) {
			t.Fatalf("bison YC inputs unexpectedly include grammar-local header %q: %#v", unwanted, yc.flatInputs())
		}
	}

	for _, want := range vfsStrings(bisonCppSkeletonInputs) {
		if !nodeHasInput(yc, want) {
			t.Fatalf("bison YC inputs missing skeleton %q", want)
		}
	}

	use := mustNodeByOutput(t, g, "$(B)/app/use.cpp.o")

	if indexOfArg(use.Cmds[0].CmdArgs.flat(), "-I$(B)/genlib/pire") < 0 {
		t.Fatalf("peer CC cmd_args missing generated bison build-root addincl: %#v", use.Cmds[0].CmdArgs.flat())
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
			t.Fatalf("generated parser object inputs missing %q: %#v", want, parserObj.flatInputs())
		}
	}
}

func TestGen_BisonYppFlatOutputAndSiblingInclude(t *testing.T) {
	files := map[string]string{}

	writeBisonTool(files)
	writeToolProgram(files, "contrib/tools/m4", "m4")
	writeTestModuleFile(files, "contrib/tools/ragel6/ya.make", "PROGRAM(ragel6)\nSRCS(main.cpp)\nEND()\n")
	writeTestModuleFile(files, bisonPreprocessPyVFS.rel(), "print('stub')\n")

	for _, input := range bisonCppSkeletonInputs {
		writeTestModuleFile(files, input.rel(), "")
	}

	writeTestModuleFile(files, "a/b/qbase/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(lexer.rl6 parser.ypp)
END()
`)
	writeTestModuleFile(files, "a/b/qbase/parser.ypp", "%%\n")
	writeTestModuleFile(files, "a/b/qbase/lexer.h", "#include <a/b/qbase/parser.h>\n")
	writeTestModuleFile(files, "a/b/qbase/lexer.rl6", "#include \"lexer.h\"\n")

	g := testGen(newMemFS(files), "a/b/qbase")

	yc := mustNodeByOutput(t, g, "$(B)/a/b/qbase/parser.h")
	mustNodeByAnyOutput(t, g, "$(B)/a/b/qbase/parser.ypp.cpp")

	for _, out := range yc.Outputs {
		if strings.Contains(out.string(), "/_/") {
			t.Fatalf("bison YC output unexpectedly under _/ namespace: %q", out)
		}
	}

	mustNodeByOutput(t, g, "$(B)/a/b/qbase/parser.ypp.cpp.o")

	lexerObj := mustNodeByOutput(t, g, "$(B)/a/b/qbase/lexer.rl6.cpp.o")

	if indexOfArg(lexerObj.Cmds[0].CmdArgs.flat(), "-I$(B)/a/b/qbase") < 0 {
		t.Fatalf("sibling CC missing generated bison addincl -I$(B)/a/b/qbase: %#v", lexerObj.Cmds[0].CmdArgs.flat())
	}

	for _, want := range []string{"$(B)/a/b/qbase/parser.h", "$(S)/a/b/qbase/parser.ypp"} {
		if !nodeHasInput(lexerObj, want) {
			t.Fatalf("sibling CC missing %q in closure: %#v", want, lexerObj.flatInputs())
		}
	}
}

func TestGen_BisonYFlatOutputPath(t *testing.T) {
	files := map[string]string{}

	writeBisonTool(files)
	writeToolProgram(files, "contrib/tools/m4", "m4")
	writeTestModuleFile(files, bisonPreprocessPyVFS.rel(), "print('stub')\n")

	for _, input := range bisonCppSkeletonInputs {
		writeTestModuleFile(files, input.rel(), "")
	}

	writeTestModuleFile(files, "req/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(req_pars.y)
END()
`)
	writeTestModuleFile(files, "req/req_pars.y", "%%\n")

	g := testGen(newMemFS(files), "req")

	mustNodeByOutput(t, g, "$(B)/req/req_pars.h")
	mustNodeByAnyOutput(t, g, "$(B)/req/req_pars.y.cpp")
	mustNodeByOutput(t, g, "$(B)/req/req_pars.y.cpp.o")
}

func TestGen_BisonFlagsReachProducerCommand(t *testing.T) {
	files := map[string]string{}

	writeBisonTool(files)
	writeToolProgram(files, "contrib/tools/m4", "m4")
	writeTestModuleFile(files, bisonPreprocessPyVFS.rel(), "print('stub')\n")

	for _, input := range bisonCppSkeletonInputs {
		writeTestModuleFile(files, input.rel(), "")
	}

	writeTestModuleFile(files, "qbase/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
BISON_FLAGS(-Wcounterexamples)
SRCS(parser.ypp)
END()
`)
	writeTestModuleFile(files, "qbase/parser.ypp", "%%\n")

	g := testGen(newMemFS(files), "qbase")

	yc := mustNodeByOutput(t, g, "$(B)/qbase/parser.h")
	args := strStrs(yc.Cmds[0].CmdArgs.flat())

	flagIdx := -1

	for i, a := range args {
		if a == "-Wcounterexamples" {
			flagIdx = i

			break
		}
	}

	if flagIdx < 0 {
		t.Fatalf("bison producer cmd_args missing BISON_FLAGS -Wcounterexamples: %#v", args)
	}

	vIdx := indexOfArg(yc.Cmds[0].CmdArgs.flat(), "-v")
	definesIdx := -1

	for i, a := range args {
		if strings.HasPrefix(a, "--defines=") {
			definesIdx = i

			break
		}
	}

	if !(vIdx >= 0 && vIdx < flagIdx && flagIdx < definesIdx) {
		t.Fatalf("BISON_FLAGS not positioned after -v and before --defines (v=%d flag=%d defines=%d): %#v", vIdx, flagIdx, definesIdx, args)
	}
}

func TestGen_BisonCppFlags(t *testing.T) {
	files := map[string]string{}

	writeBisonTool(files)
	writeToolProgram(files, "contrib/tools/m4", "m4")
	writeTestModuleFile(files, bisonPreprocessPyVFS.rel(), "print('stub')\n")

	for _, input := range bisonCppSkeletonInputs {
		writeTestModuleFile(files, input.rel(), "")
	}

	writeTestModuleFile(files, "genlib/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(pire/re_parser.y)
END()
`)
	writeTestModuleFile(files, "genlib/pire/re_parser.y", "%%\n")

	g := testGen(newMemFS(files), "genlib")

	parserObj := mustNodeByOutput(t, g, "$(B)/genlib/_/_/pire/re_parser.y.cpp.o")

	for _, want := range []string{"-Wno-unused-but-set-variable", "-Wno-deprecated-copy"} {
		if indexOfArg(parserObj.Cmds[0].CmdArgs.flat(), want) < 0 {
			t.Fatalf("bison-generated CC cmd_args missing %q: %#v", want, parserObj.Cmds[0].CmdArgs.flat())
		}
	}
}

func TestGen_BisonHeaderConsumerIncludesSourceY(t *testing.T) {
	files := map[string]string{}

	writeBisonTool(files)
	writeToolProgram(files, "contrib/tools/m4", "m4")
	writeTestModuleFile(files, bisonPreprocessPyVFS.rel(), "print('stub')\n")

	for _, input := range bisonCppSkeletonInputs {
		writeTestModuleFile(files, input.rel(), "")
	}

	writeTestModuleFile(files, "genlib/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(pire/re_parser.y)
END()
`)
	writeTestModuleFile(files, "genlib/pire/re_parser.y", "%%\n")

	writeTestModuleFile(files, "app/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(genlib)
SRCS(re_lexer.cpp)
END()
`)

	writeTestModuleFile(files, "app/re_lexer.cpp", `#include <re_parser.h>
int lex() { return 0; }
`)

	g := testGen(newMemFS(files), "app")

	lexerObj := mustNodeByOutput(t, g, "$(B)/app/re_lexer.cpp.o")
	want := "$(S)/genlib/pire/re_parser.y"

	if !nodeHasInput(lexerObj, want) {
		t.Fatalf("re_lexer.cpp.o inputs missing %q (bison source); got: %#v", want, lexerObj.flatInputs())
	}
}

func TestGen_BisonGeneratedSourceCarriesGeneratedProtoProducerDep(t *testing.T) {
	files := map[string]string{}

	writeBisonTool(files)
	writeToolProgram(files, "contrib/tools/m4", "m4")
	writeTestModuleFile(files, bisonPreprocessPyVFS.rel(), "print('stub')\n")

	for _, input := range bisonCppSkeletonInputs {
		writeTestModuleFile(files, input.rel(), "")
	}

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	writeTestModuleFile(files, "pmod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(foo.proto parser.y)
END()
`)
	writeTestModuleFile(files, "pmod/foo.proto", "syntax = \"proto3\";\npackage pmod;\nmessage Foo {}\n")
	writeTestModuleFile(files, "pmod/parser.y", `%{
#include "foo.pb.h"
%}
%%
`)

	g := testGen(newMemFS(files), "pmod")

	parserObj := mustNodeByOutput(t, g, "$(B)/pmod/parser.y.cpp.o")

	if !nodeHasInput(parserObj, "$(B)/pmod/foo.pb.h") {
		t.Fatalf("generated parser object closure missing $(B)/pmod/foo.pb.h: %#v", parserObj.flatInputs())
	}

	pb := mustNodeByOutput(t, g, "$(B)/pmod/foo.pb.h")

	found := false

	for _, dep := range graphDeps(g, parserObj) {
		if dep == pb.UID {
			found = true

			break
		}
	}

	if !found {
		t.Fatalf("bison-generated parser object missing dep on proto producer %q; deps=%v", pb.UID, graphDeps(g, parserObj))
	}
}

func writeBisonTool(files map[string]string) {
	writeToolProgram(files, "contrib/tools/bison", "bison")
	files["build/induced/by_bison/ya.make"] = "LIBRARY()\nNO_UTIL()\nNO_RUNTIME()\nEND()\n"
}
