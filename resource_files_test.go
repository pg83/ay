package main

import (
	encb64 "encoding/base64"
	"slices"
	"strings"
	"testing"
)

func TestExpandResourceFilesRapidjson(t *testing.T) {
	args := []string{
		"PREFIX", "devtools/ymake/contrib/python-rapidjson/",
		".dist-info/METADATA",
		".dist-info/top_level.txt",
		"rapidjson/license.txt",
		"rapidjson/readme.md",
	}

	got := expandResourceFiles(&ModuleData{}, nil, args)

	want := []ResourceEntry{
		{Path: "-", Key: "resfs/file/devtools/ymake/contrib/python-rapidjson/.dist-info/METADATA", SrcPath: ".dist-info/METADATA"},
		{Path: ".dist-info/METADATA", Key: "resfs/file/devtools/ymake/contrib/python-rapidjson/.dist-info/METADATA"},
		{Path: "-", Key: "resfs/file/devtools/ymake/contrib/python-rapidjson/.dist-info/top_level.txt", SrcPath: ".dist-info/top_level.txt"},
		{Path: ".dist-info/top_level.txt", Key: "resfs/file/devtools/ymake/contrib/python-rapidjson/.dist-info/top_level.txt"},
		{Path: "-", Key: "resfs/file/devtools/ymake/contrib/python-rapidjson/rapidjson/license.txt", SrcPath: "rapidjson/license.txt"},
		{Path: "rapidjson/license.txt", Key: "resfs/file/devtools/ymake/contrib/python-rapidjson/rapidjson/license.txt"},
		{Path: "-", Key: "resfs/file/devtools/ymake/contrib/python-rapidjson/rapidjson/readme.md", SrcPath: "rapidjson/readme.md"},
		{Path: "rapidjson/readme.md", Key: "resfs/file/devtools/ymake/contrib/python-rapidjson/rapidjson/readme.md"},
	}

	if len(got) != len(want) {
		t.Fatalf("expanded entries: got %d, want %d", len(got), len(want))
	}

	for i := range got {
		if got[i] != want[i] {
			t.Errorf("entry %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestGen_AllResourceFilesGlobMatchesResourceFiles(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

	files["mod/cfg/a.json"] = "{\"a\":1}\n"
	files["mod/cfg/b.json"] = "{\"b\":2}\n"
	files["mod/cfg/ignore.txt"] = "not a resource\n"

	writeTestModuleFile(files, "mod/libs/cpp/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
ALL_RESOURCE_FILES(
    json
    PREFIX cfg
    ${ARCADIA_ROOT}/mod/cfg
)
END()
`)

	g := testGen(newMemFS(files), "mod/libs/cpp")

	const moddir = "mod/libs/cpp"
	const prefix = "cfg"
	sorted := []string{"a.json", "b.json"}

	var hashPaths, keysB64, kvsHash []string

	for _, f := range sorted {
		path := "${ARCADIA_ROOT}/mod/cfg/" + f
		fileKey := "resfs/file/" + prefix + path
		hashPaths = append(hashPaths, path)
		keysB64 = append(keysB64, encb64.StdEncoding.EncodeToString([]byte(fileKey)))
		kvsHash = append(kvsHash, "resfs/src/"+fileKey+"=${rootrel;context=TEXT;input=TEXT:\""+path+"\"}")
	}

	wantHash := objcopyHash(hashPaths, keysB64, kvsHash, moddir, 0)
	wantOutput := "$(B)/mod/libs/cpp/objcopy_" + wantHash + ".o"

	objcopy := nodeByOutput(g, wantOutput)

	if objcopy == nil {
		t.Fatalf("graph is missing the ALL_RESOURCE_FILES objcopy output %q\nobjcopy nodes: %v", wantOutput, objcopyOutputs(g))
	}

	if !nodeHasInput(objcopy, "$(S)/mod/cfg/a.json") || !nodeHasInput(objcopy, "$(S)/mod/cfg/b.json") {
		t.Fatalf("objcopy inputs missing the globbed json sources: %#v", objcopy.flatInputs())
	}

	if nodeHasInput(objcopy, "$(S)/mod/cfg/ignore.txt") {
		t.Fatalf("objcopy picked up the non-json file ignore.txt: %#v", objcopy.flatInputs())
	}

	args := prCmdArgStrings(objcopy)

	for _, f := range sorted {
		wantKey := encb64.StdEncoding.EncodeToString([]byte("resfs/file/" + prefix + "${ARCADIA_ROOT}/mod/cfg/" + f))

		if !slices.Contains(args, wantKey) {
			t.Fatalf("objcopy --keys missing base64 marker key for %q: %v", f, args)
		}

		wantKv := "resfs/src/resfs/file/" + prefix + "$(S)/mod/cfg/" + f + "=mod/cfg/" + f

		if !slices.Contains(args, wantKv) {
			t.Fatalf("objcopy --kvs missing rendered resfs/src for %q (want %q): %v", f, wantKv, args)
		}

		if slices.Contains(args, "resfs/src/resfs/file/"+prefix+"${ARCADIA_ROOT}/mod/cfg/"+f+"=mod/cfg/"+f) {
			t.Fatalf("objcopy --kvs leaked the literal ${ARCADIA_ROOT} marker for %q: %v", f, args)
		}
	}

	globalAr := nodeByOutput(g, "$(B)/mod/libs/cpp/libmod-libs-cpp.global.a")

	if globalAr == nil {
		t.Fatal("graph is missing the global archive libmod-libs-cpp.global.a")
	}

	if !slices.Contains(prCmdArgStrings(globalAr), wantOutput) {
		t.Fatalf("global archive does not link the resource objcopy %q: %v", wantOutput, prCmdArgStrings(globalAr))
	}
}

func TestGen_AllResourceFilesGlobRelativeDir(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

	files["mod/app/templates/x.j2"] = "x\n"
	files["mod/app/templates/y.j2"] = "y\n"
	files["mod/app/templates/skip.txt"] = "not a resource\n"

	writeTestModuleFile(files, "mod/app/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
ALL_RESOURCE_FILES(j2 templates)
END()
`)

	g := testGen(newMemFS(files), "mod/app")

	const moddir = "mod/app"
	sorted := []string{"x.j2", "y.j2"}

	var hashPaths, keysB64, kvsHash []string

	for _, f := range sorted {
		path := "${ARCADIA_ROOT}/mod/app/templates/" + f
		fileKey := "resfs/file/templates/" + f
		hashPaths = append(hashPaths, path)
		keysB64 = append(keysB64, encb64.StdEncoding.EncodeToString([]byte(fileKey)))
		kvsHash = append(kvsHash, "resfs/src/"+fileKey+"=${rootrel;context=TEXT;input=TEXT:\""+path+"\"}")
	}

	wantHash := objcopyHash(hashPaths, keysB64, kvsHash, moddir, 0)
	wantOutput := "$(B)/mod/app/objcopy_" + wantHash + ".o"

	objcopy := nodeByOutput(g, wantOutput)

	if objcopy == nil {
		t.Fatalf("graph is missing the relative-DIR ALL_RESOURCE_FILES objcopy output %q\nobjcopy nodes: %v", wantOutput, objcopyOutputs(g))
	}

	if !nodeHasInput(objcopy, "$(S)/mod/app/templates/x.j2") || !nodeHasInput(objcopy, "$(S)/mod/app/templates/y.j2") {
		t.Fatalf("objcopy inputs missing the globbed j2 sources: %#v", objcopy.flatInputs())
	}

	if nodeHasInput(objcopy, "$(S)/mod/app/templates/skip.txt") {
		t.Fatalf("objcopy picked up the non-j2 file skip.txt: %#v", objcopy.flatInputs())
	}

	args := prCmdArgStrings(objcopy)

	for _, f := range sorted {
		wantKey := encb64.StdEncoding.EncodeToString([]byte("resfs/file/templates/" + f))

		if !slices.Contains(args, wantKey) {
			t.Fatalf("objcopy --keys missing base64 key for %q: %v", f, args)
		}

		wantKv := "resfs/src/resfs/file/templates/" + f + "=mod/app/templates/" + f

		if !slices.Contains(args, wantKv) {
			t.Fatalf("objcopy --kvs missing rendered resfs/src for %q (want %q): %v", f, wantKv, args)
		}
	}
}

func TestGen_AllResourceFilesFromDirsRelativeParentDir(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

	files["base/configs/p/a.cfg"] = "a\n"
	files["base/configs/p/b.cfg"] = "b\n"

	writeTestModuleFile(files, "base/tools/sync/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
ALL_RESOURCE_FILES_FROM_DIRS(PREFIX adminka ../../configs/p)
END()
`)

	g := testGen(newMemFS(files), "base/tools/sync")

	const moddir = "base/tools/sync"
	const prefix = "adminka"
	sorted := []string{"a.cfg", "b.cfg"}

	var hashPaths, keysB64, kvsHash []string

	for _, f := range sorted {
		path := "${ARCADIA_ROOT}/base/configs/p/" + f
		fileKey := "resfs/file/" + prefix + path
		hashPaths = append(hashPaths, path)
		keysB64 = append(keysB64, encb64.StdEncoding.EncodeToString([]byte(fileKey)))
		kvsHash = append(kvsHash, "resfs/src/"+fileKey+"=${rootrel;context=TEXT;input=TEXT:\""+path+"\"}")
	}

	wantHash := objcopyHash(hashPaths, keysB64, kvsHash, moddir, 0)
	wantOutput := "$(B)/base/tools/sync/objcopy_" + wantHash + ".o"

	objcopy := nodeByOutput(g, wantOutput)

	if objcopy == nil {
		t.Fatalf("graph is missing the FROM_DIRS `..` objcopy output %q\nobjcopy nodes: %v", wantOutput, objcopyOutputs(g))
	}

	if !nodeHasInput(objcopy, "$(S)/base/configs/p/a.cfg") || !nodeHasInput(objcopy, "$(S)/base/configs/p/b.cfg") {
		t.Fatalf("objcopy inputs missing the `..`-resolved config sources: %#v", objcopy.flatInputs())
	}

	args := prCmdArgStrings(objcopy)

	for _, f := range sorted {
		wantKv := "resfs/src/resfs/file/" + prefix + "$(S)/base/configs/p/" + f + "=base/configs/p/" + f

		if !slices.Contains(args, wantKv) {
			t.Fatalf("objcopy --kvs missing rendered resfs/src for %q (want %q): %v", f, wantKv, args)
		}
	}
}

func TestGen_AllResourceFilesGlobSourceRootedTrailingSlash(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

	files["mod/cfg/a.json"] = "{\"a\":1}\n"
	files["mod/cfg/b.json"] = "{\"b\":2}\n"

	writeTestModuleFile(files, "mod/libs/cpp/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
ALL_RESOURCE_FILES(
    json
    PREFIX cfg
    ${ARCADIA_ROOT}/mod/cfg/
)
END()
`)

	g := testGen(newMemFS(files), "mod/libs/cpp")

	const moddir = "mod/libs/cpp"
	const prefix = "cfg"
	sorted := []string{"a.json", "b.json"}

	var hashPaths, keysB64, kvsHash []string

	for _, f := range sorted {
		path := "${ARCADIA_ROOT}/mod/cfg/" + f
		fileKey := "resfs/file/" + prefix + path
		hashPaths = append(hashPaths, path)
		keysB64 = append(keysB64, encb64.StdEncoding.EncodeToString([]byte(fileKey)))
		kvsHash = append(kvsHash, "resfs/src/"+fileKey+"=${rootrel;context=TEXT;input=TEXT:\""+path+"\"}")
	}

	wantHash := objcopyHash(hashPaths, keysB64, kvsHash, moddir, 0)
	wantOutput := "$(B)/mod/libs/cpp/objcopy_" + wantHash + ".o"

	objcopy := nodeByOutput(g, wantOutput)

	if objcopy == nil {
		t.Fatalf("graph is missing the trailing-slash ALL_RESOURCE_FILES objcopy output %q\nobjcopy nodes: %v", wantOutput, objcopyOutputs(g))
	}

	args := prCmdArgStrings(objcopy)

	for _, f := range sorted {
		wantKey := encb64.StdEncoding.EncodeToString([]byte("resfs/file/" + prefix + "${ARCADIA_ROOT}/mod/cfg/" + f))

		if !slices.Contains(args, wantKey) {
			t.Fatalf("objcopy --keys missing normalized base64 marker key for %q: %v", f, args)
		}

		for _, a := range args {
			if strings.Contains(a, "mod/cfg//") {
				t.Fatalf("objcopy arg carries a double slash from the trailing-slash DIR: %q", a)
			}
		}
	}
}

func TestGen_AllResourceFilesGlobDirWildcard(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

	files["mod/cfg/sub1/a.json"] = "{\"a\":1}\n"
	files["mod/cfg/sub2/b.json"] = "{\"b\":2}\n"
	files["mod/cfg/sub1/skip.txt"] = "not a resource\n"
	files["mod/cfg/top.json"] = "{\"top\":0}\n"

	writeTestModuleFile(files, "mod/libs/cpp/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
ALL_RESOURCE_FILES(
    json
    PREFIX cfg
    ${ARCADIA_ROOT}/mod/cfg/*
)
END()
`)

	g := testGen(newMemFS(files), "mod/libs/cpp")

	const moddir = "mod/libs/cpp"
	const prefix = "cfg"

	sorted := []string{"sub1/a.json", "sub2/b.json"}

	var hashPaths, keysB64, kvsHash []string

	for _, f := range sorted {
		path := "${ARCADIA_ROOT}/mod/cfg/" + f
		fileKey := "resfs/file/" + prefix + path
		hashPaths = append(hashPaths, path)
		keysB64 = append(keysB64, encb64.StdEncoding.EncodeToString([]byte(fileKey)))
		kvsHash = append(kvsHash, "resfs/src/"+fileKey+"=${rootrel;context=TEXT;input=TEXT:\""+path+"\"}")
	}

	wantHash := objcopyHash(hashPaths, keysB64, kvsHash, moddir, 0)
	wantOutput := "$(B)/mod/libs/cpp/objcopy_" + wantHash + ".o"

	objcopy := nodeByOutput(g, wantOutput)

	if objcopy == nil {
		t.Fatalf("graph is missing the dir/* ALL_RESOURCE_FILES objcopy output %q\nobjcopy nodes: %v", wantOutput, objcopyOutputs(g))
	}

	if !nodeHasInput(objcopy, "$(S)/mod/cfg/sub1/a.json") || !nodeHasInput(objcopy, "$(S)/mod/cfg/sub2/b.json") {
		t.Fatalf("objcopy inputs missing the depth-2 globbed json sources: %#v", objcopy.flatInputs())
	}

	if nodeHasInput(objcopy, "$(S)/mod/cfg/top.json") {
		t.Fatalf("dir/*/*.json matched a depth-1 file top.json: %#v", objcopy.flatInputs())
	}

	if nodeHasInput(objcopy, "$(S)/mod/cfg/sub1/skip.txt") {
		t.Fatalf("objcopy picked up the non-json file skip.txt: %#v", objcopy.flatInputs())
	}

	args := prCmdArgStrings(objcopy)

	for _, f := range sorted {
		wantKey := encb64.StdEncoding.EncodeToString([]byte("resfs/file/" + prefix + "${ARCADIA_ROOT}/mod/cfg/" + f))

		if !slices.Contains(args, wantKey) {
			t.Fatalf("objcopy --keys missing base64 marker key for %q: %v", f, args)
		}

		wantKv := "resfs/src/resfs/file/" + prefix + "$(S)/mod/cfg/" + f + "=mod/cfg/" + f

		if !slices.Contains(args, wantKv) {
			t.Fatalf("objcopy --kvs missing rendered resfs/src for %q (want %q): %v", f, wantKv, args)
		}
	}

	globalAr := nodeByOutput(g, "$(B)/mod/libs/cpp/libmod-libs-cpp.global.a")

	if globalAr == nil {
		t.Fatal("graph is missing the global archive libmod-libs-cpp.global.a")
	}

	if !slices.Contains(prCmdArgStrings(globalAr), wantOutput) {
		t.Fatalf("global archive does not link the dir/* resource objcopy %q: %v", wantOutput, prCmdArgStrings(globalAr))
	}
}
