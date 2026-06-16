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

// TestGen_BisonCppFlags verifies that the CC node compiling a bison-generated
// C++ file carries the -Wno-unused-but-set-variable and -Wno-deprecated-copy
// flags (upstream _LANG_CFLAGS_BISON).
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

// TestGen_BisonHeaderConsumerIncludesSourceY verifies that a CC node compiling
// a file that includes a bison-generated header also receives the source .y
// file as an input (upstream adds it transitively because the bison node that
// produces the header depends on it).
func TestGen_BisonHeaderConsumerIncludesSourceY(t *testing.T) {
	files := map[string]string{}

	writeBisonTool(files)
	writeToolProgram(files, "contrib/tools/m4", "m4")
	writeTestModuleFile(files, bisonPreprocessPyVFS.rel(), "print('stub')\n")
	for _, input := range bisonCppSkeletonInputs {
		writeTestModuleFile(files, input.rel(), "")
	}

	// genlib produces re_parser.y.h from re_parser.y
	writeTestModuleFile(files, "genlib/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(pire/re_parser.y)
END()
`)
	writeTestModuleFile(files, "genlib/pire/re_parser.y", "%%\n")

	// app/re_lexer.cpp includes the generated re_parser.y.h header
	writeTestModuleFile(files, "app/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(genlib)
SRCS(re_lexer.cpp)
END()
`)
	// The bison-generated header is $(B)/genlib/pire/re_parser.h; the peer
	// addincl from genlib is $(B)/genlib/pire, so the include uses just the
	// basename without the pire/ prefix.
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
