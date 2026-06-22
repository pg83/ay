package main

import (
	"strings"
	"testing"
)

// TestEmitDecimalMD5_GeneratedSourceEntersArchive pins the
// DECIMAL_MD5_LOWER_32_BITS emitter: the macro produces an SV node whose stdout
// output is a build-root .cpp, that .cpp is compiled by a downstream CC node
// depending on the SV producer, and the resulting .o joins the module archive
// through ordinary generated-source handling.
func TestEmitDecimalMD5_GeneratedSourceEntersArchive(t *testing.T) {
	fs := newMemFS(map[string]string{
		"mod/ya.make": "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n" +
			"SET(HASH_INPUTS data.txt helper.hpp)\n" +
			"DECIMAL_MD5_LOWER_32_BITS(hash.auto.cpp FUNCNAME get_hash ${HASH_INPUTS})\n" +
			"SRCS(main.cpp)\nEND()\n",
		"mod/data.txt":   "payload\n",
		"mod/helper.hpp": "// helper\n",
		"mod/main.cpp":   "int main(){return 0;}\n",
	})

	g := testGen(fs, "mod")

	// SV producer node.
	sv := mustNodeByOutput(t, g, "$(B)/mod/hash.auto.cpp")

	if got := sv.KV.P.string(); got != "SV" {
		t.Errorf("producer kv.p = %q, want SV", got)
	}

	if sv.KV.PC.string() != "yellow" || !sv.KV.ShowOut {
		t.Errorf("producer kv = %+v, want pc=yellow show_out=yes", sv.KV)
	}

	args := strings.Join(strStrs(sv.Cmds[0].CmdArgs.flat()), " ")

	for _, want := range []string{
		"build/scripts/decimal_md5.py",
		"--fixed-output=",
		"--func-name=get_hash",
		"--lower-bits 32",
		"--source-root=$(S)",
		"$(S)/mod/data.txt",
		"$(S)/mod/helper.hpp",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("SV cmd_args missing %q; got: %s", want, args)
		}
	}

	svInputs := map[string]bool{}
	for _, in := range sv.flatInputs() {
		svInputs[in.string()] = true
	}

	for _, want := range []string{
		"$(S)/mod/data.txt",
		"$(S)/mod/helper.hpp",
		"$(S)/build/scripts/decimal_md5.py",
	} {
		if !svInputs[want] {
			t.Errorf("SV inputs missing %q", want)
		}
	}

	// Downstream CC compiling the generated source, depending on the SV producer.
	cc := mustNodeByOutput(t, g, "$(B)/mod/hash.auto.cpp.o")

	if got := cc.KV.P.string(); got != "CC" {
		t.Errorf("compile kv.p = %q, want CC", got)
	}

	ccInputs := map[string]bool{}
	for _, in := range cc.flatInputs() {
		ccInputs[in.string()] = true
	}

	for _, want := range []string{
		"$(B)/mod/hash.auto.cpp",            // the generated source
		"$(S)/mod/data.txt",                 // SV source inputs ride via closure leaves
		"$(S)/mod/helper.hpp",               //
		"$(S)/build/scripts/decimal_md5.py", //
	} {
		if !ccInputs[want] {
			t.Errorf("CC input closure missing %q", want)
		}
	}

	foundDep := false
	for _, dep := range graphDeps(g, cc) {
		if dep == sv.UID {
			foundDep = true

			break
		}
	}

	if !foundDep {
		t.Errorf("graphDeps(g, CC) = %v, want to contain SV UID %q", graphDeps(g, cc), sv.UID)
	}

	// Archive member.
	ar := mustNodeByOutput(t, g, "$(B)/mod/libmod.a")

	arArgs := strings.Join(strStrs(ar.Cmds[0].CmdArgs.flat()), " ")
	if !strings.Contains(arArgs, "$(B)/mod/hash.auto.cpp.o") {
		t.Errorf("archive cmd_args missing $(B)/mod/hash.auto.cpp.o; got: %s", arArgs)
	}

	arInputs := false
	for _, in := range ar.flatInputs() {
		if in.string() == "$(B)/mod/hash.auto.cpp.o" {
			arInputs = true

			break
		}
	}

	if !arInputs {
		t.Error("archive inputs missing $(B)/mod/hash.auto.cpp.o")
	}
}
