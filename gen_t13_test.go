package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGen_HInGeneratedHeader_RealizedInConsumer reproduces T13: a .h.in
// generated header declared in SRCS of one module but #included only by a
// PEERDIR consumer must have module_dir = the consuming module (not the
// declaring one) and must NOT be archived into the declaring module's .a.
func TestGen_HInGeneratedHeader_RealizedInConsumer(t *testing.T) {
	root := t.TempDir()
	genh := filepath.Join(root, "genh")
	cons := filepath.Join(root, "cons")
	app := filepath.Join(root, "app")
	for _, d := range []string{genh, cons, app} {
		Throw(os.MkdirAll(d, 0o755))
	}
	// declaring module: config.h.in in SRCS, plus a .cpp that does NOT include config.h
	Throw(os.WriteFile(filepath.Join(genh, "ya.make"),
		[]byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSET(MYVAR hello)\nSRCS(config.h.in own.cpp)\nEND()\n"), 0o644))
	Throw(os.WriteFile(filepath.Join(genh, "config.h.in"), []byte("#define X @MYVAR@\n"), 0o644))
	Throw(os.WriteFile(filepath.Join(genh, "own.cpp"), []byte("int g(){return 0;}\n"), 0o644))
	// consuming module: #includes the generated header across PEERDIR
	Throw(os.WriteFile(filepath.Join(cons, "ya.make"),
		[]byte("LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nPEERDIR(genh)\nSRCS(use.cpp)\nEND()\n"), 0o644))
	Throw(os.WriteFile(filepath.Join(cons, "use.cpp"),
		[]byte("#include <genh/config.h>\nint u(){return 0;}\n"), 0o644))
	// root program
	Throw(os.WriteFile(filepath.Join(app, "ya.make"),
		[]byte("PROGRAM()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nPEERDIR(cons)\nSRCS(main.cpp)\nEND()\n"), 0o644))
	Throw(os.WriteFile(filepath.Join(app, "main.cpp"), []byte("int main(){return 0;}\n"), 0o644))

	g := testGen(root, "app")

	byOut := map[string]*Node{}
	for _, n := range g.Graph {
		if len(n.Outputs) > 0 {
			byOut[n.Outputs[0].String()] = n
		}
	}

	cf := byOut["$(B)/genh/config.h"]
	if cf == nil {
		t.Fatal("no CF node emitted for genh/config.h")
	}
	if got := cf.TargetProperties["module_dir"]; got != "cons" {
		t.Errorf("config.h module_dir = %q, want %q (consuming module)", got, "cons")
	}

	ar := byOut["$(B)/genh/libgenh.a"]
	if ar == nil {
		t.Fatal("no AR node for genh")
	}
	for _, c := range ar.Cmds {
		for _, a := range c.CmdArgs {
			if a == "$(B)/genh/config.h" {
				t.Errorf("genh AR cmd_args archives config.h as a member: %v", c.CmdArgs)
			}
		}
	}
	for _, in := range ar.Inputs {
		if in.String() == "$(B)/genh/config.h" || in.String() == "$(S)/genh/config.h.in" {
			t.Errorf("genh AR inputs include %q (generated header must not be archived)", in.String())
		}
	}

	use := byOut["$(B)/cons/use.cpp.o"]
	if use == nil {
		t.Fatal("no CC node for cons/use.cpp")
	}
	found := false
	for _, d := range use.Deps {
		if d == cf.UID {
			found = true
		}
	}
	if !found {
		t.Errorf("use.cpp.o deps %v missing config.h CF uid %q", use.Deps, cf.UID)
	}
}
