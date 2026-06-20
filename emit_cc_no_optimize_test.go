package main

import "testing"

// releaseHostPlatform builds the x86_64 host platform in release mode (BUILD_TYPE
// = release), so its compile C-flag vector is hostCFlags — the one carrying the
// optimize token -O3. The default testGen host is a debug build (no optimize
// token), which would not exercise NO_OPTIMIZE's -O3 → -O0 reassignment.
func releaseHostPlatform() *Platform {
	flags := make(map[string]string, len(testToolchainFlags)+2)
	for k, v := range testToolchainFlags {
		flags[k] = v
	}
	flags["PIC"] = "yes"
	flags["BUILD_TYPE"] = "release"
	return newPlatform(newMemFS(nil), OSLinux, ISAX8664, flags, "", "")
}

func releaseTargetPlatform() *Platform {
	flags := make(map[string]string, len(testToolchainFlags)+1)
	for k, v := range testToolchainFlags {
		flags[k] = v
	}
	flags["PIC"] = "no"
	return newPlatform(newMemFS(nil), OSLinux, ISAAArch64, flags, "", "")
}

func ccArgsForOutput(t *testing.T, g *Graph, output string) []string {
	t.Helper()
	n := mustNodeByOutput(t, g, output)
	return strStrs(n.Cmds[0].CmdArgs.flat())
}

func argsContain(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// gperfToolFiles writes a contrib/tools/gperf PROGRAM (so it builds on the host
// when referenced via a .gperf source) plus a gpmod LIBRARY that pulls it in.
// extraToolMacros is spliced into the tool's ya.make body.
func gperfToolFiles(extraToolMacros string) map[string]string {
	files := map[string]string{}
	files["contrib/tools/gperf/ya.make"] = "PROGRAM(gperf)\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n" +
		extraToolMacros + "SRCS(main.cpp)\nEND()\n"
	files["contrib/tools/gperf/main.cpp"] = "int main(){return 0;}\n"
	files["gpmod/ya.make"] = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(tags.gperf)\nEND()\n"
	files["gpmod/tags.gperf"] = "%{\n%}\n%%\n"
	return files
}

// TestGen_NoOptimizeSuppressesOptimization pins the generic NO_OPTIMIZE()
// mechanism (gnu_compiler.conf: `when ($NO_OPTIMIZE=="yes"){OPTIMIZE=-O0}`):
// a module declaring NO_OPTIMIZE() compiles with -O0 in the optimize slot
// instead of the default release -O3. Representative: contrib/tools/gperf,
// built on the release host.
func TestGen_NoOptimizeSuppressesOptimization(t *testing.T) {
	files := gperfToolFiles("NO_OPTIMIZE()\n")

	g := Gen(newMemFS(files), "gpmod", releaseHostPlatform(), releaseTargetPlatform(), func(Warn) {})

	args := ccArgsForOutput(t, g, "$(B)/contrib/tools/gperf/main.cpp.pic.o")

	if !argsContain(args, "-O0") {
		t.Fatalf("NO_OPTIMIZE compile missing -O0: %v", args)
	}
	if argsContain(args, "-O3") {
		t.Fatalf("NO_OPTIMIZE compile still carries -O3: %v", args)
	}

	// The LD node embeds the __vcs_version__.c compile (composeLDCmdVcsCompile);
	// it shares the module's optimize suppression.
	ld := mustNodeByOutput(t, g, "$(B)/contrib/tools/gperf/gperf")
	vcs := strStrs(ld.Cmds[1].CmdArgs.flat())
	if !argsContain(vcs, "-O0") || argsContain(vcs, "-O3") {
		t.Fatalf("NO_OPTIMIZE vcs compile not suppressed: %v", vcs)
	}
}

// TestGen_DefaultOptimizationIntact is the negative guard: the same module
// WITHOUT NO_OPTIMIZE() keeps the default release optimization (-O3) and gains
// no -O0.
func TestGen_DefaultOptimizationIntact(t *testing.T) {
	files := gperfToolFiles("")

	g := Gen(newMemFS(files), "gpmod", releaseHostPlatform(), releaseTargetPlatform(), func(Warn) {})

	args := ccArgsForOutput(t, g, "$(B)/contrib/tools/gperf/main.cpp.pic.o")

	if !argsContain(args, "-O3") {
		t.Fatalf("default compile missing -O3: %v", args)
	}
	if argsContain(args, "-O0") {
		t.Fatalf("default compile unexpectedly carries -O0: %v", args)
	}
}
