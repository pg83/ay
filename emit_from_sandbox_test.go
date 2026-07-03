package main

import (
	"slices"
	"testing"
)

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

	sb := mustNodeByOutput(t, g, "$(B)/mod/trie")

	if sb.KV.P != pkSB {
		t.Fatalf("trie producer kind = %q, want SB", sb.KV.P.string())
	}

	pr := mustNodeByOutput(t, g, "$(B)/mod/pack.bin")

	if !nodeHasInput(pr, "$(B)/mod/trie") {
		t.Fatalf("pack.bin inputs missing $(B)/mod/trie: %#v", pr.flatInputs())
	}

	if nodeHasInput(pr, "$(S)/mod/trie") {
		t.Fatalf("pack.bin must not list the source path $(S)/mod/trie: %#v", pr.flatInputs())
	}

	if !slices.Contains(prCmdArgStrings(pr), "$(B)/mod/trie") {
		t.Fatalf("pack.bin command missing $(B)/mod/trie arg: %v", prCmdArgStrings(pr))
	}

	if !slices.Contains(graphDeps(g, pr), sb.Ref) {
		t.Fatalf("pack.bin deps missing SB fetch ref %d: %v", sb.Ref, graphDeps(g, pr))
	}
}

func TestGen_FromSandboxAutoArchiveBecomesLibraryMember(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "mkllike/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
FROM_SANDBOX(420003523 OUT libmkl_core.a)
FROM_SANDBOX(420003524 OUT_NOAUTO scratch.a)
END()
`)

	writeTestModuleFile(files, "plain/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(plain.cpp)
END()
`)
	writeTestModuleFile(files, "plain/plain.cpp", "int plain(){return 0;}\n")

	writeTestModuleFile(files, "app/ya.make", `PROGRAM()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(mkllike)
PEERDIR(plain)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

	g := testGen(newMemFS(files), "app")

	mustNodeByOutput(t, g, "$(B)/mkllike/libmkl_core.a")

	ar := mustNodeByOutput(t, g, "$(B)/mkllike/libmkllike.a")

	if ar.KV.P != pkAR {
		t.Fatalf("libmkllike.a producer kind = %q, want AR", ar.KV.P.string())
	}

	if !nodeHasInput(ar, "$(B)/mkllike/libmkl_core.a") {
		t.Fatalf("library archive inputs missing fetched member $(B)/mkllike/libmkl_core.a: %#v", ar.flatInputs())
	}

	if !slices.Contains(prCmdArgStrings(ar), "$(B)/mkllike/libmkl_core.a") {
		t.Fatalf("library archive cmd missing fetched member: %v", prCmdArgStrings(ar))
	}

	if nodeHasInput(ar, "$(B)/mkllike/scratch.a") {
		t.Fatalf("OUT_NOAUTO scratch.a must not be a library archive member: %#v", ar.flatInputs())
	}

	var ld *Node

	for _, n := range g.Graph {
		if n.KV.P == pkLD {
			ld = n
		}
	}

	if ld == nil {
		t.Fatal("no LD node found in graph")
	}

	if !nodeHasInput(ld, "$(B)/mkllike/libmkllike.a") {
		t.Fatalf("program LD inputs missing peer archive $(B)/mkllike/libmkllike.a: %v", ld.flatInputs())
	}

	if !slices.Contains(prCmdArgStrings(ld), "mkllike/libmkllike.a") {
		t.Fatalf("program LD cmd missing peer archive token mkllike/libmkllike.a")
	}

	plainAR := mustNodeByOutput(t, g, "$(B)/plain/libplain.a")

	if plainAR.KV.P != pkAR {
		t.Fatalf("libplain.a producer kind = %q, want AR", plainAR.KV.P.string())
	}

	if !nodeHasInput(plainAR, "$(B)/plain/plain.cpp.o") {
		t.Fatalf("plain library archive missing compiled member plain.cpp.o: %#v", plainAR.flatInputs())
	}
}

func TestGen_FromSandboxScriptInputsExplicitThree(t *testing.T) {
	files := map[string]string{}

	files["build/scripts/fetch_from_sandbox.py"] = "import process_command_files as pcf\nimport fetch_from\n"
	files["build/scripts/fetch_from.py"] = "import retry\n"
	files["build/scripts/process_command_files.py"] = "\n"
	files["build/scripts/retry.py"] = "\n"
	files["build/scripts/error.py"] = "\n"

	writeTestModuleFile(files, "mod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
FROM_SANDBOX(1038029059 OUT_NOAUTO trie)
END()
`)

	g := testGen(newMemFS(files), "mod")

	sb := mustNodeByOutput(t, g, "$(B)/mod/trie")

	if sb.KV.P != pkSB {
		t.Fatalf("trie producer kind = %q, want SB", sb.KV.P.string())
	}

	want := []string{
		"$(S)/build/scripts/fetch_from.py",
		"$(S)/build/scripts/fetch_from_sandbox.py",
		"$(S)/build/scripts/process_command_files.py",
	}

	for _, w := range want {
		if !nodeHasInput(sb, w) {
			t.Fatalf("SB node missing explicit script input %q: %#v", w, sb.flatInputs())
		}
	}

	for _, bad := range []string{"$(S)/build/scripts/error.py", "$(S)/build/scripts/retry.py"} {
		if nodeHasInput(sb, bad) {
			t.Fatalf("SB node must not carry helper-closure input %q: %#v", bad, sb.flatInputs())
		}
	}

	counts := map[string]int{}

	for _, in := range sb.flatInputs() {
		counts[in.string()]++
	}

	for _, w := range want {
		if counts[w] != 1 {
			t.Fatalf("script input %q appears %d times, want exactly 1: %#v", w, counts[w], sb.flatInputs())
		}
	}
}

func TestGen_FromSandboxScriptsPropagateThroughRunProgramAndArchiveAsm(t *testing.T) {
	files := map[string]string{}

	files["build/scripts/fetch_from_sandbox.py"] = "\n"
	files["build/scripts/fetch_from.py"] = "\n"
	files["build/scripts/process_command_files.py"] = "\n"

	writeToolProgram(files, "tools/morph2blob", "morph2blob")
	writeToolProgram(files, "tools/archiver", "archiver")
	writeToolProgram(files, "contrib/tools/yasm", "yasm")

	writeTestModuleFile(files, "mod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
FROM_SANDBOX(1038029059 OUT_NOAUTO trie)
FROM_SANDBOX(1229967763 OUT_NOAUTO suffixes)
FROM_SANDBOX(1038025065 OUT_NOAUTO paradigms)
RUN_PROGRAM(
    tools/morph2blob trie suffixes paradigms
    IN trie suffixes paradigms
    STDOUT ${BINDIR}/pack.bin
)
ARCHIVE_ASM(
    NAME Pack
    DONTCOMPRESS
    ${BINDIR}/pack.bin
)
END()
`)

	g := testGenX86(newMemFS(files), "mod")

	scripts := []string{
		"$(S)/build/scripts/fetch_from.py",
		"$(S)/build/scripts/fetch_from_sandbox.py",
		"$(S)/build/scripts/process_command_files.py",
	}

	pr := mustNodeByOutput(t, g, "$(B)/mod/pack.bin")

	if pr.KV.P != pkPR {
		t.Fatalf("pack.bin kv.p = %q, want PR", pr.KV.P.string())
	}

	for _, s := range scripts {
		if !nodeHasInput(pr, s) {
			t.Errorf("PR pack.bin missing propagated fetch script %q: %v", s, vfsStringsT3(pr.flatInputs()))
		}
	}

	rd := mustNodeByAnyOutput(t, g, "$(B)/mod/Pack.rodata.o")

	if rd.KV.P != pkRD {
		t.Fatalf("Pack.rodata.o kv.p = %q, want RD", rd.KV.P.string())
	}

	for _, s := range scripts {
		if !nodeHasInput(rd, s) {
			t.Errorf("RD Pack.rodata.o missing propagated fetch script %q: %v", s, vfsStringsT3(rd.flatInputs()))
		}
	}

	for _, out := range []string{"$(B)/mod/trie", "$(B)/mod/suffixes", "$(B)/mod/paradigms"} {
		sb := mustNodeByOutput(t, g, out)

		if sb.KV.P != pkSB {
			t.Fatalf("%s kv.p = %q, want SB", out, sb.KV.P.string())
		}

		if got := len(sb.flatInputs()); got != len(scripts) {
			t.Errorf("SB %s has %d inputs, want exactly %d: %v", out, got, len(scripts), vfsStringsT3(sb.flatInputs()))
		}

		for _, s := range scripts {
			if !nodeHasInput(sb, s) {
				t.Errorf("SB %s missing explicit script %q: %v", out, s, vfsStringsT3(sb.flatInputs()))
			}
		}
	}
}

func TestGen_FromSandboxRenameResourceCommand(t *testing.T) {
	cases := []struct {
		name   string
		decl   string
		out    string
		outArg string
	}{
		{
			name:   "OUT_NOAUTO",
			decl:   "FROM_SANDBOX(FILE 123 RENAME RESOURCE OUT_NOAUTO payload.json)",
			out:    "$(B)/mod/payload.json",
			outArg: "payload.json",
		},
		{
			name:   "OUT",
			decl:   "FROM_SANDBOX(FILE 456 RENAME RESOURCE OUT payload.txt)",
			out:    "$(B)/mod/payload.txt",
			outArg: "payload.txt",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{}
			files["build/scripts/fetch_from_sandbox.py"] = "\n"
			files["build/scripts/fetch_from.py"] = "\n"
			files["build/scripts/process_command_files.py"] = "\n"

			writeTestModuleFile(files, "mod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
`+tc.decl+`
END()
`)

			g := testGen(newMemFS(files), "mod")
			sb := mustNodeByOutput(t, g, tc.out)

			if sb.KV.P != pkSB {
				t.Fatalf("%s producer kind = %q, want SB", tc.outArg, sb.KV.P.string())
			}

			args := prCmdArgStrings(sb)
			want := []string{"--copy-to-dir", ".", "--rename", "RESOURCE", "--", tc.outArg}

			if !containsSubsequence(args, want) {
				t.Fatalf("SB command missing subsequence %v: %v", want, args)
			}

			wantInputs := []string{
				"$(S)/build/scripts/fetch_from.py",
				"$(S)/build/scripts/fetch_from_sandbox.py",
				"$(S)/build/scripts/process_command_files.py",
			}

			for _, w := range wantInputs {
				if !nodeHasInput(sb, w) {
					t.Fatalf("SB node missing explicit script input %q: %#v", w, sb.flatInputs())
				}
			}

			if got := len(sb.flatInputs()); got != len(wantInputs) {
				t.Fatalf("SB node has %d inputs, want exactly %d: %#v", got, len(wantInputs), sb.flatInputs())
			}
		})
	}
}

func containsSubsequence(args, want []string) bool {
	for i := 0; i+len(want) <= len(args); i++ {
		if slices.Equal(args[i:i+len(want)], want) {
			return true
		}
	}

	return false
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
