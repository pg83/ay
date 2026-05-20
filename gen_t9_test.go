package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGen_CF_SetVarsReachCfgVars reproduces T9: SET(...)-derived vars must
// reach the CFG_VARS of a CF node emitted through the SRCS .cpp.in path.
func TestGen_CF_SetVarsReachCfgVars(t *testing.T) {
	root := t.TempDir()
	libDir := filepath.Join(root, "thelib")
	Throw(os.MkdirAll(libDir, 0o755))
	Throw(os.WriteFile(filepath.Join(libDir, "ya.make"),
		[]byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSET(MYVAR hello)\nDEFAULT(MYDEF world)\nSRCS(lib.cpp x.cpp.in)\nEND()\n"), 0o644))
	Throw(os.WriteFile(filepath.Join(libDir, "lib.cpp"), []byte("int f(){return 0;}\n"), 0o644))
	Throw(os.WriteFile(filepath.Join(libDir, "x.cpp.in"), []byte("int a = @MYVAR@;\nint b = @MYDEF@;\n"), 0o644))

	g := testGen(root, "thelib")
	var cf *Node
	for _, n := range g.Graph {
		if len(n.Outputs) > 0 && n.Outputs[0].String() == "$(B)/thelib/x.cpp" {
			cf = n
			break
		}
	}
	if cf == nil {
		t.Fatal("no CF node emitted for thelib/x.cpp")
	}
	args := strings.Join(cf.Cmds[0].CmdArgs, " ")
	if !strings.Contains(args, "MYVAR=hello") {
		t.Errorf("CF cmd_args missing SET var MYVAR=hello; got: %s", args)
	}
	if !strings.Contains(args, "MYDEF=world") {
		t.Errorf("CF cmd_args missing DEFAULT var MYDEF=world; got: %s", args)
	}
}
