package main

import (
	"slices"
	"testing"
)

// A FROM_SANDBOX OUT/OUT_NOAUTO file is a Sandbox-fetched build output, not a
// source file. When a RUN_PROGRAM in the same module consumes it via IN, the
// generator must resolve it to the fetch (SB) node's $(B) output — listing that
// output as an input and depending on the SB node — never to an on-disk source
// path. (Under --sandboxing the latter faults the UID finalizer's source-content
// hash on the nonexistent file; see ads/clemmer/automorphology/uzb in sg7.)
func TestGen_FromSandboxOutputConsumedAsRunProgramInput(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "tools/morph2blob", "morph2blob")

	writeTestModuleFile(files, "mod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
FROM_SANDBOX(1038029059 OUT_NOAUTO trie)
RUN_PROGRAM(
    tools/morph2blob trie
    IN trie
    STDOUT ${BINDIR}/pack.bin
)
SRCS(reg.cpp)
END()
`)
	writeTestModuleFile(files, "mod/reg.cpp", "int reg(){return 0;}\n")

	writeTestModuleFile(files, "app/ya.make", `PROGRAM()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(mod)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

	g := testGen(newMemFS(files), "app")

	// FROM_SANDBOX emits an SB fetch node producing the OUT_NOAUTO file in $(B).
	sb := mustNodeByOutput(t, g, "$(B)/mod/trie")
	if sb.KV.P != pkSB {
		t.Fatalf("trie producer kind = %q, want SB", sb.KV.P.string())
	}

	// The RUN_PROGRAM (pack.bin) consumes trie as the fetch output, not a source.
	pr := mustNodeByOutput(t, g, "$(B)/mod/pack.bin")
	if !nodeHasInput(pr, "$(B)/mod/trie") {
		t.Fatalf("pack.bin inputs missing $(B)/mod/trie: %#v", pr.flatInputs())
	}
	if nodeHasInput(pr, "$(S)/mod/trie") {
		t.Fatalf("pack.bin must not list the source path $(S)/mod/trie: %#v", pr.flatInputs())
	}

	// The positional `trie` arg resolves to the same $(B) fetch output.
	if !slices.Contains(prCmdArgStrings(pr), "$(B)/mod/trie") {
		t.Fatalf("pack.bin command missing $(B)/mod/trie arg: %v", prCmdArgStrings(pr))
	}

	// pack.bin depends on the SB fetch node that produces trie.
	if !slices.Contains(graphDeps(g, pr), sb.UID) {
		t.Fatalf("pack.bin deps missing SB fetch uid %q: %v", sb.UID, graphDeps(g, pr))
	}
}

func prCmdArgStrings(n *Node) []string {
	var out []string

	for _, c := range n.Cmds {
		for _, a := range c.CmdArgs.flat() {
			out = append(out, a.string())
		}
	}

	return out
}
