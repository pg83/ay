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

// TestGen_BisonYppFlatOutputAndSiblingInclude verifies that a .ypp source —
// the C++ bison family, same as .y — produces flat module-build-dir outputs
// ($(B)/<mod>/parser.h, $(B)/<mod>/parser.ypp.cpp, flat object) and that a
// sibling source including the generated <parser.h> picks up the build-root
// addincl and the .ypp source in its include closure.
func TestGen_BisonYppFlatOutputAndSiblingInclude(t *testing.T) {
	files := map[string]string{}

	writeBisonTool(files)
	writeToolProgram(files, "contrib/tools/m4", "m4")
	writeTestModuleFile(files, "contrib/tools/ragel6/ya.make", "PROGRAM(ragel6)\nSRCS(main.cpp)\nEND()\n")
	writeTestModuleFile(files, bisonPreprocessPyVFS.rel(), "print('stub')\n")
	for _, input := range bisonCppSkeletonInputs {
		writeTestModuleFile(files, input.rel(), "")
	}

	// The sibling lexer.rl6 is listed BEFORE parser.ypp in SRCS and reaches the
	// generated header through an intermediate hand-written header by full
	// $(B)-rooted angle include — the yt query base layout (lexer.rl6 → lexer.h
	// → <a/b/qbase/parser.h>). The .rl6 closure is walked in Pass 1; without
	// registering the bison producer before that walk, lexer.h's stale (no
	// parser.h) children are cached in the file-id-keyed scanCache and reused by
	// every later consumer of lexer.h.
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

	// Flat header + generated source in the module build dir (no _/).
	yc := mustNodeByOutput(t, g, "$(B)/a/b/qbase/parser.h")
	mustNodeByAnyOutput(t, g, "$(B)/a/b/qbase/parser.ypp.cpp")
	for _, out := range yc.Outputs {
		if strings.Contains(out.string(), "/_/") {
			t.Fatalf("bison YC output unexpectedly under _/ namespace: %q", out)
		}
	}

	// Flat compiled object (no _/), proving the .ypp generated source rebased
	// to the module build dir, not $(B)/a/b/qbase/_/_/parser.ypp.cpp.o.
	mustNodeByOutput(t, g, "$(B)/a/b/qbase/parser.ypp.cpp.o")

	// Sibling lexer.rl6's compiled object gets the generated build-root addincl,
	// the generated parser.h itself, and the .ypp source — all pulled into its
	// include closure through the bison header producer registered in the
	// pre-pass (before the .rl6 closure is walked).
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

// TestGen_BisonYFlatOutputPath verifies that a flat (in-module) .y source
// produces its generated .cpp and compiled object directly in the module build
// dir — upstream's nopath output placement — not under an _/ namespace.
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
	// The compiled object must be flat too.
	mustNodeByOutput(t, g, "$(B)/req/req_pars.y.cpp.o")
}

// TestGen_BisonFlagsReachProducerCommand verifies that a module-level
// BISON_FLAGS(<flags>) reaches the YC producer's bison invocation, positioned
// after the default -v (the BISON_FLAGS variable default) and before --defines
// (upstream bison_lex.conf: `${tool} $BISON_FLAGS … $_BISON_HEADER`, with the
// macro doing SET_APPEND(BISON_FLAGS $Flags)).
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

// TestGen_BisonGeneratedSourceCarriesGeneratedProtoProducerDep reproduces the
// sg7 deps-only gap on bison/ypp parser compile roots (e.g.
// $(B)/kernel/remorph/tokenlogic/parser.y.cpp.o). The bison-generated source's
// prologue reaches a generated <foo>.pb.h; upstream's flat dep model records a
// dep edge to that .pb.h's protoc producer on the compile node, exactly as for
// a handwritten .cpp that includes the same header. emitBisonY was the only
// generated-source CC emitter not routing its walked closure through
// resolveCodegenDepRefs, so the producer dep was dropped.
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

	// Same module owns the proto and the bison grammar; the grammar's prologue
	// includes the generated <foo.pb.h> via the module's own $(B) addincl.
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

	// Sanity: the generated proto header IS in the compile closure.
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
