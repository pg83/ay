package main

import (
	encb64 "encoding/base64"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func objcopyHash(paths []string, keysB64 []string, kvs []string, unitPath string, moduleTag STR) string {
	list := make([]string, 0, len(paths)+len(keysB64)+len(kvs)+1)

	list = append(list, paths...)
	list = append(list, keysB64...)
	list = append(list, kvs...)
	list = append(list, "$S/"+unitPath)

	tag := ""

	if moduleTag != 0 {
		tag = moduleTag.string()
	}

	hash, _ := resourceHashInto(nil, list, tag)

	return hash
}

func TestObjcopyHashCerts(t *testing.T) {
	paths := []string{"cacert.pem"}
	keysB64 := []string{encb64.StdEncoding.EncodeToString([]byte("/builtin/cacert"))}
	kvs := []string{}
	unitPath := "certs"
	got := objcopyHash(paths, keysB64, kvs, unitPath, 0)
	want := "c27c99b2d9d5eade92fd72d0aa"

	if got != want {
		t.Fatalf("certs objcopy hash: got %q, want %q", got, want)
	}
}

func TestObjcopyHashRapidjson(t *testing.T) {
	paths := []string{
		".dist-info/METADATA",
		".dist-info/top_level.txt",
		"rapidjson/license.txt",
		"rapidjson/readme.md",
	}
	prefix := "devtools/ymake/contrib/python-rapidjson/"
	keysRaw := make([]string, len(paths))
	kvs := make([]string, len(paths))

	for i, p := range paths {
		fileKey := "resfs/file/" + prefix + p
		keysRaw[i] = fileKey
		kvs[i] = "resfs/src/" + fileKey + "=${rootrel;context=TEXT;input=TEXT:\"" + p + "\"}"
	}

	keysB64 := make([]string, len(keysRaw))

	for i, k := range keysRaw {
		keysB64[i] = encb64.StdEncoding.EncodeToString([]byte(k))
	}

	got := objcopyHash(paths, keysB64, kvs, "devtools/ymake/contrib/python-rapidjson", unitTagPy3)
	want := "55c44b1fdbfda511798cd895e2"

	if got != want {
		t.Fatalf("rapidjson objcopy hash: got %q, want %q", got, want)
	}
}

func TestPyNamespaceObjcopyHashRuntimePy3(t *testing.T) {
	kv := `py/namespace/bd17cfe3d9af11d01ff7b15ebc3786a7/library/python/runtime_py3="library.python.runtime_py3."`

	got := objcopyHash(nil, nil, []string{kv}, "library/python/runtime_py3", unitTagPy3)
	want := "3b0561f75631281b973aa8b64e"

	if got != want {
		t.Fatalf("runtime_py3 namespace objcopy hash: got %q, want %q", got, want)
	}
}

func TestNoCheckImportsObjcopyHashLib2Py(t *testing.T) {
	value := "_ios_support _pyrepl.* antigravity asyncio.unix_events asyncio.windows_events asyncio.windows_utils ctypes.wintypes curses.* dbm.gnu dbm.ndbm dbm.sqlite3 encodings.mbcs encodings.oem lzma multiprocessing.popen_fork multiprocessing.popen_forkserver multiprocessing.popen_spawn_posix multiprocessing.popen_spawn_win32 sqlite3.* turtle pty tty"
	kv := `py/no_check_imports/2fepmfaacurvvaalmzqchmko4a="` + value + `"`

	got := objcopyHash(nil, nil, []string{kv}, "contrib/tools/python3/lib2/py", unitTagPy3)
	want := "cd47bcaec327e5eb9db4641ec8"

	if got != want {
		t.Fatalf("contrib/tools/python3/lib2/py no_check_imports objcopy hash: got %q, want %q", got, want)
	}
}

func TestPyMainObjcopyHashPy3ccSlow(t *testing.T) {
	kv := "PY_MAIN=tools.py3cc.slow.main:main"

	got := objcopyHash(nil, nil, []string{kv}, "tools/py3cc/slow", unitTagPy3)
	want := "4b1c18d0dc6973976969ad23be"

	if got != want {
		t.Fatalf("tools/py3cc/slow PY_MAIN objcopy hash: got %q, want %q", got, want)
	}
}

func TestRootrelInputPath(t *testing.T) {
	cases := []struct {
		name   string
		kv     string
		want   string
		wantOK bool
	}{
		{
			name:   "resource_files srcKv",
			kv:     "resfs/src/resfs/file/contrib/python/pytz/py3/pytz/zoneinfo/Asia/Muscat=${rootrel;context=TEXT;input=TEXT:\"pytz/zoneinfo/Asia/Muscat\"}",
			want:   "pytz/zoneinfo/Asia/Muscat",
			wantOK: true,
		},
		{
			name:   "py_main kv (no marker)",
			kv:     "PY_MAIN=tools.x.main:main",
			want:   "",
			wantOK: false,
		},
		{
			name:   "namespace kv (no marker)",
			kv:     "py/namespace/contrib/python/pytz/py3=\"pytz.\"",
			want:   "",
			wantOK: false,
		},
		{
			name:   "malformed marker (no closing)",
			kv:     "resfs/src/x=${rootrel;context=TEXT;input=TEXT:\"pytz/zoneinfo/Asia/Muscat",
			want:   "",
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := rootrelInputPath(tc.kv)

			if got != tc.want || ok != tc.wantOK {
				t.Fatalf("rootrelInputPath(%q) = (%q, %v), want (%q, %v)", tc.kv, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

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

	sb := mustNodeByOutput(t, g, "$(B)/yt/bundle/llvm-symbolizer")

	if sb.KV.P != pkSB {
		t.Fatalf("llvm-symbolizer producer kind = %q, want SB", sb.KV.P.string())
	}

	objcopy := findNodeByOutputPrefix(g, "$(B)/yt/bundle/objcopy_")

	if objcopy == nil {
		t.Fatal("graph is missing yt/bundle objcopy output")
	}

	if !nodeHasInput(objcopy, "$(B)/yt/bundle/llvm-symbolizer") {
		t.Fatalf("objcopy inputs missing build-root fetched artifact: %#v", objcopy.flatInputs())
	}

	if nodeHasInput(objcopy, "$(S)/yt/bundle/llvm-symbolizer") {
		t.Fatalf("objcopy inputs use the source path for a fetched artifact: %#v", objcopy.flatInputs())
	}

	if nodeHasInput(objcopy, "$(B)/yt/bundle/yt/bundle/llvm-symbolizer") {
		t.Fatalf("objcopy inputs double the module dir: %#v", objcopy.flatInputs())
	}

	if !slices.Contains(graphDeps(g, objcopy), sb.Ref) {
		t.Fatalf("objcopy deps missing SB fetch ref %d: %v", sb.Ref, graphDeps(g, objcopy))
	}

	wantKey := encb64.StdEncoding.EncodeToString([]byte("/ytprof/llvm-symbolizer"))

	if !slices.Contains(prCmdArgStrings(objcopy), wantKey) {
		t.Fatalf("objcopy --keys missing base64 RESOURCE key %q: %v", wantKey, prCmdArgStrings(objcopy))
	}
}

func TestGen_ResourceFilesRootRelativeSourceFromOtherModule(t *testing.T) {
	files := map[string]string{
		"contrib/libs/python/ya.make":     "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n",
		"library/python/resource/ya.make": "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n",
	}
	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

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

	wantKey := encb64.StdEncoding.EncodeToString([]byte("resfs/file/modadvert/dyn_disclaimers/disclaimers_config.pb.txt"))

	if !slices.Contains(prCmdArgStrings(objcopy), wantKey) {
		t.Fatalf("objcopy --keys missing base64 resfs/file key %q: %v", wantKey, prCmdArgStrings(objcopy))
	}

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

func TestGen_ResourceBindirOutputCarriesProducerBuildClosure(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeToolProgram(files, "tools/gen/bin", "gen")
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	writeTestModuleFile(files, "p/ya.make", "PROTO_LIBRARY()\nSRCS(dep.proto)\nEND()\n")
	writeTestModuleFile(files, "p/dep.proto", "syntax = \"proto3\";\nmessage Dep {}\n")

	writeTestModuleFile(files, "db/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(p)
RUN_PROGRAM(
    tools/gen/bin
        --out
        ${BINDIR}/payload.pb
    OUTPUT_INCLUDES
        p/dep.pb.h
    OUT ${BINDIR}/payload.pb
)
RESOURCE(
    ${BINDIR}/payload.pb payload.pb
)
END()
`)

	g := testGen(newMemFS(files), "db")

	objcopy := findNodeByOutputPrefix(g, "$(B)/db/objcopy_")

	if objcopy == nil {
		t.Fatal("graph is missing db objcopy output")
	}

	if !nodeHasInput(objcopy, "$(B)/db/payload.pb") {
		t.Fatalf("objcopy inputs missing build-root payload.pb: %#v", objcopy.flatInputs())
	}

	const depPbH = "$(B)/p/dep.pb.h"

	if !nodeHasInput(objcopy, depPbH) {
		t.Fatalf("objcopy inputs missing producer build-root closure %q: %#v", depPbH, objcopy.flatInputs())
	}

	if nodeHasInput(objcopy, "$(S)/p/dep.proto") {
		t.Fatalf("objcopy must not carry source-tree .proto leaf: %#v", objcopy.flatInputs())
	}

	depProducer := mustNodeByOutput(t, g, depPbH)
	foundDep := false

	for _, d := range graphDeps(g, objcopy) {
		if d == depProducer.Ref {
			foundDep = true

			break
		}
	}

	if !foundDep {
		t.Fatalf("objcopy deps %v do not include dep.pb.h producer ref %d", graphDeps(g, objcopy), depProducer.Ref)
	}

	args := prCmdArgStrings(objcopy)

	for _, a := range args {
		if strings.Contains(a, "dep.pb.h") {
			t.Fatalf("objcopy command must not name the closure header: %v", args)
		}
	}

	wantHashInputs := []string{
		"${BINDIR}/payload.pb",
		encb64.StdEncoding.EncodeToString([]byte("payload.pb")),
		"$S/db",
	}
	sort.Strings(wantHashInputs)
	wantHash := md5Hex(strings.Join(wantHashInputs, ","))[:hashLen]
	wantOutput := "$(B)/db/objcopy_" + wantHash + ".o"

	if got := objcopy.Outputs[0].string(); got != wantOutput {
		t.Fatalf("objcopy output = %q, want %q (hash must ignore the closure)", got, wantOutput)
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

	wantHashInputs := []string{
		"${BINDIR}/data.json",

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

	if !nodeHasInput(objcopy, "$(B)/db/common.json") {
		t.Fatalf("objcopy inputs missing build-root common.json: %#v", objcopy.flatInputs())
	}

	if nodeHasInput(objcopy, "$(S)/db/common.json") {
		t.Fatalf("objcopy inputs still carry phantom source-root common.json: %#v", objcopy.flatInputs())
	}

	producer := findNodeByOutputPrefix(g, "$(B)/db/common.json")

	if producer == nil {
		t.Fatal("graph is missing the RUN_PROGRAM producer of common.json")
	}

	foundDep := false

	for _, d := range graphDeps(g, objcopy) {
		if d == producer.Ref {
			foundDep = true

			break
		}
	}

	if !foundDep {
		t.Fatalf("objcopy deps %v do not include the common.json producer ref %d", graphDeps(g, objcopy), producer.Ref)
	}

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

func TestGen_FromSandboxVarOutNoautoResourceFilesFeedsObjcopyFromBuildRoot(t *testing.T) {
	files := map[string]string{}
	files["build/scripts/fetch_from_sandbox.py"] = "\n"
	files["build/scripts/fetch_from.py"] = "\n"
	files["build/scripts/process_command_files.py"] = "\n"

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

	var setBody strings.Builder
	const nDicts = 48

	for i := 2; i <= nDicts; i++ {
		setBody.WriteString("    plutonium_dicts/" + strconv.Itoa(i) + ".dict\n")
	}

	writeTestModuleFile(files, "dictmod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SET_APPEND(DICTS
`+setBody.String()+`)
FROM_SANDBOX(12019934890 OUT_NOAUTO
    ${DICTS}
)
RESOURCE_FILES(${DICTS})
END()
`)

	g := testGen(newMemFS(files), "dictmod")

	mainOut := "$(B)/dictmod/plutonium_dicts/2.dict"
	sb := mustNodeByAnyOutput(t, g, mainOut)

	if sb.KV.P != pkSB {
		t.Fatalf("main output producer kind = %q, want SB", sb.KV.P.string())
	}

	var chunks []*Node

	for _, n := range g.Graph {
		if n == nil {
			continue
		}

		if len(n.Outputs) > 0 && strings.HasPrefix(n.Outputs[0].string(), "$(B)/dictmod/objcopy_") {
			chunks = append(chunks, n)
		}
	}

	if len(chunks) < 2 {
		t.Fatalf("expected several objcopy chunks, got %d", len(chunks))
	}

	embedsMain := func(n *Node) bool {
		args := prCmdArgStrings(n)
		inInputs := false

		for _, a := range args {
			switch a {
			case "--inputs":
				inInputs = true
			case "--keys", "--kvs":
				inInputs = false
			default:
				if inInputs && a == mainOut {
					return true
				}
			}
		}

		return false
	}

	sawNonFirst := false

	for _, c := range chunks {
		if nodeHasInput(c, "$(S)/dictmod/plutonium_dicts/2.dict") {
			t.Fatalf("objcopy %s carries phantom source-root 2.dict: %#v", c.Outputs[0].string(), vfsStringsT3(c.flatInputs()))
		}

		if !nodeHasInput(c, mainOut) {
			t.Fatalf("objcopy %s missing SB main-output input %s: %#v", c.Outputs[0].string(), mainOut, vfsStringsT3(c.flatInputs()))
		}

		if !embedsMain(c) {
			sawNonFirst = true
		}
	}

	if !sawNonFirst {
		t.Fatal("test did not exercise a non-first chunk (one that does not embed 2.dict); increase nDicts")
	}
}

func TestGen_ResourceGeneratedPayloadCarriesProducerSourceInputs(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeToolProgram(files, "tools/gen/bin", "gen")

	files["mod/a.remorph"] = "rules\n"
	files["mod/gz.gzt"] = "gazetteer\n"
	files["mod/c.gztproto"] = "proto-ish\n"
	files["mod/base.proto"] = "syntax = \"proto3\";\nmessage Base {}\n"

	writeTestModuleFile(files, "mod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    tools/gen/bin
        --out
        ${BINDIR}/first.bin
    IN
        a.remorph
        base.proto
        gz.gzt
    OUT_NOAUTO ${BINDIR}/first.bin
)
RUN_PROGRAM(
    tools/gen/bin
        --out
        ${BINDIR}/second.bin
    IN
        ${BINDIR}/first.bin
        c.gztproto
    OUT_NOAUTO ${BINDIR}/second.bin
)
RESOURCE(
    ${BINDIR}/second.bin KEY
)
END()
`)

	g := testGen(newMemFS(files), "mod")

	objcopy := findNodeByOutputPrefix(g, "$(B)/mod/objcopy_")

	if objcopy == nil {
		t.Fatal("graph is missing mod objcopy output")
	}

	if !nodeHasInput(objcopy, "$(B)/mod/second.bin") {
		t.Fatalf("objcopy inputs missing build-root second.bin: %#v", objcopy.flatInputs())
	}

	for _, want := range []string{
		"$(S)/mod/c.gztproto",
		"$(S)/mod/a.remorph",
		"$(S)/mod/gz.gzt",
	} {
		if !nodeHasInput(objcopy, want) {
			t.Fatalf("objcopy inputs missing producer source leaf %q: %#v", want, objcopy.flatInputs())
		}
	}

	if nodeHasInput(objcopy, "$(S)/mod/base.proto") {
		t.Fatalf("objcopy must not carry the .proto compile-closure leaf: %#v", objcopy.flatInputs())
	}

	args := prCmdArgStrings(objcopy)

	for _, a := range args {
		if strings.Contains(a, ".remorph") || strings.Contains(a, ".gzt") || strings.Contains(a, ".proto") {
			t.Fatalf("objcopy command must not name a producer source leaf: %v", args)
		}
	}

	wantHashInputs := []string{
		"${BINDIR}/second.bin",
		encb64.StdEncoding.EncodeToString([]byte("KEY")),
		"$S/mod",
	}
	sort.Strings(wantHashInputs)
	wantHash := md5Hex(strings.Join(wantHashInputs, ","))[:hashLen]
	wantOutput := "$(B)/mod/objcopy_" + wantHash + ".o"

	if got := objcopy.Outputs[0].string(); got != wantOutput {
		t.Fatalf("objcopy output = %q, want %q (hash must ignore source attribution)", got, wantOutput)
	}
}

func TestGen_ResourceStaticSourceGainsNoGeneratedProducerInputs(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

	files["mod/data.txt"] = "static\n"
	files["mod/extra.remorph"] = "unrelated\n"

	writeTestModuleFile(files, "mod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RESOURCE(
    data.txt KEY
)
END()
`)

	g := testGen(newMemFS(files), "mod")

	objcopy := findNodeByOutputPrefix(g, "$(B)/mod/objcopy_")

	if objcopy == nil {
		t.Fatal("graph is missing mod objcopy output")
	}

	if !nodeHasInput(objcopy, "$(S)/mod/data.txt") {
		t.Fatalf("objcopy inputs missing static source data.txt: %#v", objcopy.flatInputs())
	}

	if nodeHasInput(objcopy, "$(S)/mod/extra.remorph") {
		t.Fatalf("static-resource objcopy gained a synthetic producer source input: %#v", objcopy.flatInputs())
	}
}

func TestEmitProgramResource_CppProgramLinksObjcopy(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

	writeTestModuleFile(files, "dep/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(d.cpp)\nEND()\n")
	writeTestModuleFile(files, "dep/d.cpp", "int d(){return 0;}\n")

	writeTestModuleFile(files, "prog/ya.make", "PROGRAM()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(main.cpp)\nBUNDLE(dep NAME x.bundle)\nRESOURCE(x.bundle dep/key)\nEND()\n")
	writeTestModuleFile(files, "prog/main.cpp", "int main(){return 0;}\n")

	g := testGen(newMemFS(files), "prog")

	depAR := mustNodeByOutput(t, g, "$(B)/dep/libdep.a")
	bn := mustNodeByOutput(t, g, "$(B)/prog/x.bundle")

	if bn.KV.P != pkBN {
		t.Errorf("bundle node kv.p = %q, want BN", bn.KV.P.string())
	}

	if !nodeHasInput(bn, "$(B)/dep/libdep.a") {
		t.Errorf("BN node inputs missing $(B)/dep/libdep.a: %v", vfsStringsT3(bn.flatInputs()))
	}

	if !depsContain(graphDeps(g, bn), depAR.Ref) {
		t.Errorf("graphDeps(BN) %v does not include the bundled AR ref %d", graphDeps(g, bn), depAR.Ref)
	}

	oc := findNodeByOutputPrefix(g, "$(B)/prog/objcopy_")

	if oc == nil {
		t.Fatal("graph is missing the prog resource objcopy node (C++ PROGRAM resource not linked)")
	}

	if !nodeHasInput(oc, "$(B)/prog/x.bundle") {
		t.Errorf("objcopy inputs missing the BN build output $(B)/prog/x.bundle: %v", vfsStringsT3(oc.flatInputs()))
	}

	if nodeHasInput(oc, "$(S)/prog/x.bundle") {
		t.Errorf("objcopy lists the nonexistent source $(S)/prog/x.bundle: %v", vfsStringsT3(oc.flatInputs()))
	}

	if !depsContain(graphDeps(g, oc), bn.Ref) {
		t.Errorf("graphDeps(objcopy) %v does not include the BN ref %d", graphDeps(g, oc), bn.Ref)
	}

	ld := mustNodeByOutput(t, g, "$(B)/prog/prog")

	if !nodeHasInput(ld, oc.Outputs[0].string()) {
		t.Errorf("LD inputs missing the resource objcopy member %q: %v", oc.Outputs[0].string(), vfsStringsT3(ld.flatInputs()))
	}

	if !depsContain(graphDeps(g, ld), oc.Ref) {
		t.Errorf("graphDeps(LD) %v does not include the objcopy ref %d", graphDeps(g, ld), oc.Ref)
	}
}

func objcopyOutputs(g *Graph) []string {
	var out []string

	for _, n := range g.Graph {
		if n == nil {
			continue
		}

		if len(n.Outputs) > 0 && strings.Contains(n.Outputs[0].string(), "/objcopy_") {
			out = append(out, n.Outputs[0].string())
		}
	}

	return out
}

func depsContain(deps []NodeRef, want NodeRef) bool {
	for _, d := range deps {
		if d == want {
			return true
		}
	}

	return false
}
