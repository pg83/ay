package main

import (
	"strings"
	"testing"
)

// TestStarlark_ConfigureFile pins the configure_file() builtin → ConfigureFileStmt.
func TestStarlark_ConfigureFile(t *testing.T) {
	env := DefaultIfEnv.clone()

	assertSameStmts(t,
		evalStarStr(t, `library(srcs = ["a.cpp"] + configure_file("tmpl.h.in", "out.h"))`, env),
		parseMakeStr(t, "LIBRARY()\nSRCS(a.cpp)\nCONFIGURE_FILE(tmpl.h.in out.h)\nEND()\n"))
}

// TestEmitCF_GeneratedFromRidesAsClosureLeaf pins the CONFIGURE_FILE emitter's
// generated-from propagation: a cross-module consumer that #includes a configured
// header must carry, in its CC input closure, the generated header, the template
// source (.h.in) and configure_file.py — both riding as registry ClosureLeaves,
// not as fake #includes — plus the template's own #include (registered as the
// generated header's parsed includes).
func TestEmitCF_GeneratedFromRidesAsClosureLeaf(t *testing.T) {
	fs := newMemFS(map[string]string{
		"prod/ya.make":     "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(config.h.in)\nEND()\n",
		"prod/config.h.in": "#include \"marker.h\"\nint x = @V@;\n",
		"prod/marker.h":    "// marker\n",
		"app/ya.make":      "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nPEERDIR(prod)\nSRCS(use.cpp)\nEND()\n",
		"app/use.cpp":      "#include <prod/config.h>\nint use(){return 0;}\n",
	})

	g := testGen(fs, "app")
	cc := mustNodeByOutput(t, g, "$(B)/app/use.cpp.o")

	inputs := map[string]bool{}

	for _, in := range cc.flatInputs() {
		inputs[in.string()] = true
	}

	for _, want := range []string{
		"$(B)/prod/config.h",                   // the generated header itself
		"$(S)/prod/config.h.in",                // template source — generated-from ClosureLeaf
		"$(S)/build/scripts/configure_file.py", // generator script — generated-from ClosureLeaf
		"$(S)/prod/marker.h",                   // the template's own #include, registered on config.h
	} {
		if !inputs[want] {
			t.Errorf("use.cpp.o input closure missing %q", want)
		}
	}
}

func TestGen_CF_SetVarsReachCfgVars(t *testing.T) {
	fs := newMemFS(map[string]string{
		"thelib/ya.make":  "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSET(MYVAR hello)\nDEFAULT(MYDEF world)\nSRCS(lib.cpp x.cpp.in)\nEND()\n",
		"thelib/lib.cpp":  "int f(){return 0;}\n",
		"thelib/x.cpp.in": "int a = @MYVAR@;\nint b = @MYDEF@;\n",
	})

	g := testGen(fs, "thelib")
	var cf *Node
	for _, n := range g.Graph {
		if len(n.Outputs) > 0 && n.Outputs[0].string() == "$(B)/thelib/x.cpp" {
			cf = n
			break
		}
	}
	if cf == nil {
		t.Fatal("no CF node emitted for thelib/x.cpp")
	}
	args := strings.Join(strStrs(cf.Cmds[0].CmdArgs.flat()), " ")
	if !strings.Contains(args, "MYVAR=hello") {
		t.Errorf("CF cmd_args missing SET var MYVAR=hello; got: %s", args)
	}
	if !strings.Contains(args, "MYDEF=world") {
		t.Errorf("CF cmd_args missing DEFAULT var MYDEF=world; got: %s", args)
	}
}
