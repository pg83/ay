package main

import (
	encb64 "encoding/base64"
	"slices"
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

// A FROM_SANDBOX OUT/OUT_NOAUTO file is a Sandbox-fetched build output. When a
// RESOURCE in the same module embeds it via an arcadia-root-relative path rooted
// at the module dir (yt/yt/library/ytprof/bundle/llvm-symbolizer), the objcopy
// packer must read the artifact from $(B) and depend on the SB fetch node — never
// resolve it to a $(S) source path (which faults the UID finalizer's content hash
// under --sandboxing) nor to the module-dir-doubled $(B) path. The resfs key
// (base64 of the literal RESOURCE key) is unchanged from a source resource.
func TestGen_ResourceRootRelativeFromSandboxOutputFeedsObjcopyFromBuildRoot(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

	writeTestModuleFile(files, "yt/bundle/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
FROM_SANDBOX(
    FILE 2531143113
    OUT_NOAUTO llvm-symbolizer
)
RESOURCE(
    yt/bundle/llvm-symbolizer /ytprof/llvm-symbolizer
)
END()
`)

	g := testGen(newMemFS(files), "yt/bundle")

	// FROM_SANDBOX emits an SB fetch node producing the OUT_NOAUTO file in $(B).
	sb := mustNodeByOutput(t, g, "$(B)/yt/bundle/llvm-symbolizer")
	if sb.KV.P != pkSB {
		t.Fatalf("llvm-symbolizer producer kind = %q, want SB", sb.KV.P.string())
	}

	objcopy := findNodeByOutputPrefix(g, "$(B)/yt/bundle/objcopy_")
	if objcopy == nil {
		t.Fatal("graph is missing yt/bundle objcopy output")
	}

	// The objcopy embeds the fetched artifact from $(B), not a source path and not
	// the module-dir-doubled build path.
	if !nodeHasInput(objcopy, "$(B)/yt/bundle/llvm-symbolizer") {
		t.Fatalf("objcopy inputs missing build-root fetched artifact: %#v", objcopy.flatInputs())
	}
	if nodeHasInput(objcopy, "$(S)/yt/bundle/llvm-symbolizer") {
		t.Fatalf("objcopy inputs use the source path for a fetched artifact: %#v", objcopy.flatInputs())
	}
	if nodeHasInput(objcopy, "$(B)/yt/bundle/yt/bundle/llvm-symbolizer") {
		t.Fatalf("objcopy inputs double the module dir: %#v", objcopy.flatInputs())
	}

	// The objcopy depends on the SB fetch node that produces the artifact.
	if !slices.Contains(graphDeps(g, objcopy), sb.UID) {
		t.Fatalf("objcopy deps missing SB fetch uid %q: %v", sb.UID, graphDeps(g, objcopy))
	}

	// The resfs key (the literal RESOURCE key, base64) is unaffected by the
	// build-root resolution — only the input VFS and the producer dep change.
	wantKey := encb64.StdEncoding.EncodeToString([]byte("/ytprof/llvm-symbolizer"))
	if !slices.Contains(prCmdArgStrings(objcopy), wantKey) {
		t.Fatalf("objcopy --keys missing base64 RESOURCE key %q: %v", wantKey, prCmdArgStrings(objcopy))
	}
}

// A RESOURCE_FILES path may name an ordinary source file that lives at the
// arcadia root OUTSIDE the consuming module (e.g. yabs/server/libs/banner_flags
// embedding modadvert/dyn_disclaimers/disclaimers_config.pb.txt). res.py expands
// it into a payload member AND a resfs/src kv member
// (${rootrel;input=TEXT:"<path>"}); ymake resolves that input the same way as
// the payload — to $(S)/<path>, the source-root file — because the file exists
// there. Before the fix the resfs/src kv fallback naively joined module dir +
// path, fabricating a phantom $(S)/<module>/<path> input that sg7 then aborted
// while content-hashing. Both members must bind to the same root source path,
// with no producer dep for an ordinary source.
func TestGen_ResourceFilesRootRelativeSourceFromOtherModule(t *testing.T) {
	files := map[string]string{
		"contrib/libs/python/ya.make":     "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n",
		"library/python/resource/ya.make": "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n",
	}
	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

	// The embedded source lives at the arcadia root, NOT under the module dir.
	files["modadvert/dyn_disclaimers/disclaimers_config.pb.txt"] = "config\n"

	writeTestModuleFile(files, "yabs/server/libs/banner_flags/ya.make", `PY3_LIBRARY()
PEERDIR(library/python/resource)
RESOURCE_FILES(
    modadvert/dyn_disclaimers/disclaimers_config.pb.txt
)
END()
`)

	g := testGen(newMemFS(files), "yabs/server/libs/banner_flags")

	objcopy := findNodeByOutputPrefix(g, "$(B)/yabs/server/libs/banner_flags/objcopy_")
	if objcopy == nil {
		t.Fatal("graph is missing banner_flags objcopy output")
	}

	const rootSrc = "$(S)/modadvert/dyn_disclaimers/disclaimers_config.pb.txt"
	const phantom = "$(S)/yabs/server/libs/banner_flags/modadvert/dyn_disclaimers/disclaimers_config.pb.txt"

	if !nodeHasInput(objcopy, rootSrc) {
		t.Fatalf("objcopy inputs missing root source %q: %#v", rootSrc, objcopy.flatInputs())
	}
	if nodeHasInput(objcopy, phantom) {
		t.Fatalf("objcopy inputs carry the module-prefixed phantom %q: %#v", phantom, objcopy.flatInputs())
	}

	// resfs/file key (literal RESOURCE_FILES key, base64) is unchanged.
	wantKey := encb64.StdEncoding.EncodeToString([]byte("resfs/file/modadvert/dyn_disclaimers/disclaimers_config.pb.txt"))
	if !slices.Contains(prCmdArgStrings(objcopy), wantKey) {
		t.Fatalf("objcopy --keys missing base64 resfs/file key %q: %v", wantKey, prCmdArgStrings(objcopy))
	}

	// resfs/src kv command value carries the root source path, not the phantom.
	wantKv := "resfs/src/resfs/file/modadvert/dyn_disclaimers/disclaimers_config.pb.txt=modadvert/dyn_disclaimers/disclaimers_config.pb.txt"
	phantomKv := "resfs/src/resfs/file/modadvert/dyn_disclaimers/disclaimers_config.pb.txt=yabs/server/libs/banner_flags/modadvert/dyn_disclaimers/disclaimers_config.pb.txt"
	args := prCmdArgStrings(objcopy)
	if !slices.Contains(args, wantKv) {
		t.Fatalf("objcopy --kvs missing root-relative resfs/src %q: %v", wantKv, args)
	}
	if slices.Contains(args, phantomKv) {
		t.Fatalf("objcopy --kvs carries module-prefixed phantom resfs/src %q: %v", phantomKv, args)
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
