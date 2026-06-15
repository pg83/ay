package main

import (
	"sort"
	"strings"
	"testing"
)

func TestGen_ResourceRelativeOutputFeedsObjcopyFromBuildRoot(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeToolProgram(files, "tools/json_gen/bin", "json_gen")

	writeTestModuleFile(files, "db/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    tools/json_gen/bin
        --output
        data.json
    OUT_NOAUTO data.json
)
RESOURCE(
    data.json /data.json
)
END()
`)

	g := testGen(newMemFS(files), "db")

	objcopy := findNodeByOutputPrefix(g, "$(B)/db/objcopy_")
	if objcopy == nil {
		t.Fatal("graph is missing db objcopy output")
	}
	if !nodeHasInput(objcopy, "$(B)/db/data.json") {
		t.Fatalf("objcopy inputs missing build-root data.json: %#v", objcopy.flatInputs())
	}
	if nodeHasInput(objcopy, "$(S)/db/data.json") {
		t.Fatalf("objcopy inputs still use source-root data.json: %#v", objcopy.flatInputs())
	}
}

func TestGen_ResourceBindirOutputFeedsObjcopyFromBuildRoot(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeToolProgram(files, "tools/json_gen/bin", "json_gen")

	writeTestModuleFile(files, "db/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    tools/json_gen/bin
        --output
        ${BINDIR}/data.json
    OUT_NOAUTO ${BINDIR}/data.json
)
RESOURCE(
    ${BINDIR}/data.json /data.json
)
END()
`)

	g := testGen(newMemFS(files), "db")

	objcopy := findNodeByOutputPrefix(g, "$(B)/db/objcopy_")
	if objcopy == nil {
		t.Fatal("graph is missing db objcopy output")
	}
	if !nodeHasInput(objcopy, "$(B)/db/data.json") {
		t.Fatalf("objcopy inputs missing build-root data.json: %#v", objcopy.flatInputs())
	}
	for _, in := range objcopy.flatInputs() {
		if strings.Contains(in.string(), "${BINDIR}") {
			t.Fatalf("objcopy inputs still leak ${BINDIR}: %#v", objcopy.flatInputs())
		}
	}

	// Upstream's TObjCopyResourcePacker hashes RESOURCE() pair.Path raw, i.e.
	// '${BINDIR}/data.json', NOT the expanded '$(B)/db/data.json'. Pre-
	// expanding here drifts the objcopy_<hash> filename vs REF (caught on
	// sg5: yt/yql/.../yt/provider/objcopy_da30... was 0288...). Lock the
	// hash inputs we sort+md5 so a future "helpful" expansion regresses
	// fast.
	wantHashInputs := []string{
		"${BINDIR}/data.json",
		// base64 of "/data.json" — RESOURCE() Key is literal, not
		// resfs/file/-prefixed (unlike RESOURCE_FILES).
		"L2RhdGEuanNvbg==",
		"$S/db",
	}
	sort.Strings(wantHashInputs)
	wantHash := md5Hex(strings.Join(wantHashInputs, ","))[:hashLen]
	wantOutput := "$(B)/db/objcopy_" + wantHash + ".o"
	gotOutput := objcopy.Outputs[0].string()
	if gotOutput != wantOutput {
		t.Fatalf("objcopy output = %q, want %q (REF hashes RESOURCE Path RAW)", gotOutput, wantOutput)
	}
}
