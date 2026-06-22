package main

import "testing"

func TestGen_CopyFileUsesSourceRootInputFromIncludedMacro(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "mod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
INCLUDE(${ARCADIA_ROOT}/shared/copy.ya.make.inc)
END()
`)
	writeTestModuleFile(files, "shared/copy.ya.make.inc", `COPY_FILE(
    TEXT
    shared/generated.txt
    ${BINDIR}/shared/generated.h
)
`)
	writeTestModuleFile(files, "shared/generated.txt", "generated\n")

	g := testGen(newMemFS(files), "mod")

	cp := mustNodeByOutput(t, g, "$(B)/mod/shared/generated.h")

	if !nodeHasInput(cp, "$(S)/shared/generated.txt") {
		t.Fatalf("copy inputs missing source-root generated.txt: %#v", cp.flatInputs())
	}

	if nodeHasInput(cp, "$(S)/mod/shared/generated.txt") {
		t.Fatalf("copy inputs still carry duplicated module-prefixed generated.txt: %#v", cp.flatInputs())
	}
}
