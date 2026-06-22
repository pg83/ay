package main

import (
	"strings"
	"testing"
)

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
		"$(B)/prod/config.h",
		"$(S)/prod/config.h.in",
		"$(S)/build/scripts/configure_file.py",
		"$(S)/prod/marker.h",
	} {
		if !inputs[want] {
			t.Errorf("use.cpp.o input closure missing %q", want)
		}
	}
}

func TestBuildCFGVars_BuildTypeFromPlatform(t *testing.T) {
	fs := newMemFS(map[string]string{"m/tmpl.in": "type = @BUILD_TYPE@\n"})

	if got := buildCFGVars(fs, "m/tmpl.in", nil, nil, "RELEASE"); !containsString(got, "BUILD_TYPE=RELEASE") {
		t.Errorf("release-platform CONFIGURE_FILE vars = %v, want BUILD_TYPE=RELEASE", got)
	}

	if got := buildCFGVars(fs, "m/tmpl.in", nil, nil, "DEBUG"); !containsString(got, "BUILD_TYPE=DEBUG") {
		t.Errorf("debug-platform CONFIGURE_FILE vars = %v, want BUILD_TYPE=DEBUG", got)
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
