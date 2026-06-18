package main

import (
	encb64 "encoding/base64"
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

// A RUN_PROGRAM STDOUT(_NOAUTO) output embedded via RESOURCE_FILES is a build
// artifact, not a source file. RESOURCE_FILES expands each file into a payload
// entry AND a resfs/src kv entry whose ${rootrel;input=TEXT:"..."} names the
// same file; both must resolve to $(B) with the producer dependency. Mirrors the
// sg7 unit yabs/models_services/feature_store/mappers/common/catalog (STDOUT_NOAUTO
// common.json -> RESOURCE_FILES). Before the fix the resfs/src input resolved to
// a phantom $(S)/db/common.json that does not exist and carries no producer edge.
func TestGen_ResourceFilesStdoutOutputFeedsObjcopyFromBuildRoot(t *testing.T) {
	files := map[string]string{
		"contrib/libs/python/ya.make":     "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n",
		"library/python/resource/ya.make": "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n",
	}
	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeToolProgram(files, "db/dumper", "dumper")

	writeTestModuleFile(files, "db/ya.make", `PY3_LIBRARY()
PEERDIR(library/python/resource)
RUN_PROGRAM(
    db/dumper
    CWD ${ARCADIA_BUILD_ROOT}
    STDOUT_NOAUTO common.json
)
RESOURCE_FILES(
    PREFIX feature_store/catalog/
    common.json
)
END()
`)

	g := testGen(newMemFS(files), "db")

	objcopy := findNodeByOutputPrefix(g, "$(B)/db/objcopy_")
	if objcopy == nil {
		t.Fatal("graph is missing db objcopy output")
	}

	// (1) build-tree input, no phantom $(S) source for the generated json — both
	// the payload entry and the resfs/src kv entry point at $(B).
	if !nodeHasInput(objcopy, "$(B)/db/common.json") {
		t.Fatalf("objcopy inputs missing build-root common.json: %#v", objcopy.flatInputs())
	}
	if nodeHasInput(objcopy, "$(S)/db/common.json") {
		t.Fatalf("objcopy inputs still carry phantom source-root common.json: %#v", objcopy.flatInputs())
	}

	// (2) producer dependency: the objcopy node depends on the PR node that
	// produces common.json via STDOUT_NOAUTO.
	producer := findNodeByOutputPrefix(g, "$(B)/db/common.json")
	if producer == nil {
		t.Fatal("graph is missing the RUN_PROGRAM producer of common.json")
	}
	foundDep := false
	for _, d := range graphDeps(g, objcopy) {
		if d == producer.UID {
			foundDep = true
			break
		}
	}
	if !foundDep {
		t.Fatalf("objcopy deps %v do not include the common.json producer uid %q", graphDeps(g, objcopy), producer.UID)
	}

	// (3) the expected RESOURCE_FILES key survives: resfs/file/<PREFIX><file>,
	// base64-encoded, present in the objcopy command.
	wantKey := encb64.StdEncoding.EncodeToString([]byte("resfs/file/feature_store/catalog/common.json"))
	foundKey := false
	for _, c := range objcopy.Cmds {
		for _, a := range c.CmdArgs.flat() {
			if a.string() == wantKey {
				foundKey = true
			}
		}
	}
	if !foundKey {
		t.Fatalf("objcopy command missing base64 resource key %q", wantKey)
	}
}
