package main

import (
	"strings"
	"testing"
)

// TestArchive_PlainPropagatesSourceMembers pins that a plain ARCHIVE (no KEYS)
// rides its direct source members into the include closure of a C++ unit that
// #includes the generated archive header — as ARCHIVE_BY_KEYS already does.
// Before the fix plain ARCHIVE left PropagateSourceMembers unset, so the member
// never reached the consumer.
func TestArchive_PlainPropagatesSourceMembers(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "tools/archiver", "archiver")

	writeTestModuleFile(files, "mod/ya.make",
		"LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"+
			"SRCS(\n    use.cpp\n)\n"+
			"ARCHIVE(\n    NAME data.inc\n    payload.lst\n)\nEND()\n")
	writeTestModuleFile(files, "mod/payload.lst", "row\n")
	writeTestModuleFile(files, "mod/use.cpp", "#include \"data.inc\"\n")

	g := testGen(newMemFS(files), "mod")

	var use *Node
	for _, n := range g.Graph {
		if n.KV.P == pkCC && len(n.Outputs) > 0 && strings.HasSuffix(n.Outputs[0].string(), "/use.cpp.o") {
			use = n
			break
		}
	}
	if use == nil {
		t.Fatal("no CC node for use.cpp.o")
	}
	if !nodeHasInput(use, "$(S)/mod/payload.lst") {
		t.Errorf("use.cpp.o inputs %v missing plain-ARCHIVE source member %q", vfsStringsT3(use.flatInputs()), "$(S)/mod/payload.lst")
	}
}

// TestArchiveByKeys_TopLevel covers an ordinary top-level ARCHIVE_BY_KEYS: the
// keyed archive lists its SRCDIR-backed members plain, passes the colon-joined
// key list through `-k`, and writes the addincl output; a C++ unit that
// #includes the generated archive header gets -I$(B)/<moddir> and the archived
// source members as input-closure leaves.
func TestArchiveByKeys_TopLevel(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "tools/archiver", "archiver")

	writeTestModuleFile(files, "mod/ya.make",
		"LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"+
			"SRCS(\n    use.cpp\n)\n"+
			"ARCHIVE_BY_KEYS(\n    NAME data.inc\n    KEYS /k1:/k2\n    a.txt\n    sub/b.txt\n)\nEND()\n")
	writeTestModuleFile(files, "mod/a.txt", "alpha\n")
	writeTestModuleFile(files, "mod/sub/b.txt", "beta\n")
	writeTestModuleFile(files, "mod/use.cpp", "#include \"data.inc\"\n")

	g := testGen(newMemFS(files), "mod")

	// (1) the archive node: keyed command shape — members plain, -k <keys>, -o <archive>.
	ar := mustNodeByOutput(t, g, "$(B)/mod/data.inc")
	if ar.KV.P != pkAR || ar.KV.PC != pcLightRed {
		t.Errorf("archive kv = {p:%q pc:%q}, want {AR light-red}", ar.KV.P.string(), ar.KV.PC.string())
	}
	arCmd := strings.Join(strStrs(ar.Cmds[0].CmdArgs.flat()), " ")
	for _, want := range []string{
		"$(S)/mod/a.txt $(S)/mod/sub/b.txt",
		"-k /k1:/k2",
		"-o $(B)/mod/data.inc",
	} {
		if !strings.Contains(arCmd, want) {
			t.Errorf("archive cmd %q missing %q", arCmd, want)
		}
	}
	if strings.Contains(arCmd, "$(S)/mod/a.txt:") {
		t.Errorf("ARCHIVE_BY_KEYS must list members plain, got colon-suffixed: %q", arCmd)
	}

	// members resolve SRCDIR-backed and are the archive's action inputs.
	for _, in := range []string{"$(S)/mod/a.txt", "$(S)/mod/sub/b.txt"} {
		if !nodeHasInput(ar, in) {
			t.Errorf("archive inputs %v missing member %q", vfsStringsT3(ar.flatInputs()), in)
		}
	}

	// (2) the C++ consumer: -I$(B)/mod plus the archived members as closure leaves.
	var use *Node
	for _, n := range g.Graph {
		if n.KV.P == pkCC && len(n.Outputs) > 0 && strings.HasSuffix(n.Outputs[0].string(), "/use.cpp.o") {
			use = n
			break
		}
	}
	if use == nil {
		t.Fatal("no CC node for use.cpp.o")
	}
	if !contains(use.Cmds[0].CmdArgs.flat(), "-I$(B)/mod") {
		t.Errorf("use.cpp.o cmd missing -I$(B)/mod; got %v", strStrs(use.Cmds[0].CmdArgs.flat()))
	}
	for _, leaf := range []string{"$(S)/mod/a.txt", "$(S)/mod/sub/b.txt"} {
		if !nodeHasInput(use, leaf) {
			t.Errorf("use.cpp.o inputs %v missing archive closure leaf %q", vfsStringsT3(use.flatInputs()), leaf)
		}
	}
}
