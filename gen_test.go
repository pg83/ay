package main

import (
	"crypto/md5"
	enchex "encoding/hex"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestGen_AcceptsProgramModule_Synthetic(t *testing.T) {
	fs := newMemFS(map[string]string{
		"mainprog/ya.make": "PROGRAM()\nPEERDIR(thelib)\nSRCS(main.cpp)\nEND()\n",
		"thelib/ya.make":   "LIBRARY()\nSRCS(lib.cpp)\nEND()\n",
	})

	g := testGen(fs, "mainprog")

	if len(g.Graph) != 5 {
		t.Fatalf("Gen produced %d nodes, want 5 (2 CC + 1 AR + 1 LD + 1 vcs.json)", len(g.Graph))
	}

	if len(g.Result) != 1 {
		t.Fatalf("Gen produced %d results, want 1", len(g.Result))
	}

	nodesByOutput := make(map[string]*Node, len(g.Graph))

	for _, n := range g.Graph {
		if len(n.Outputs) == 0 {
			t.Fatalf("node uid=%s has no outputs", n.UID)
		}

		nodesByOutput[n.Outputs[0].string()] = n
	}

	const (
		libCCOut    = "$(B)/thelib/lib.cpp.o"
		libARout    = "$(B)/thelib/libthelib.a"
		mainCCOut   = "$(B)/mainprog/main.cpp.o"
		mainBinPath = "$(B)/mainprog/mainprog"
	)

	for _, key := range []string{libCCOut, libARout, mainCCOut, mainBinPath} {
		if _, ok := nodesByOutput[key]; !ok {
			t.Fatalf("graph is missing expected output %q", key)
		}
	}

	rootLD := nodesByOutput[mainBinPath]

	if rootLD.KV.P != pkLD {
		t.Errorf("root node kv.p = %q, want LD", rootLD.KV.P)
	}

	if len(rootLD.Cmds) != 4 {
		t.Errorf("root LD Cmds = %d, want 4", len(rootLD.Cmds))
	}

	if g.Result[0] != rootLD.UID {
		t.Errorf("result UID = %q, want mainprog LD uid %q", g.Result[0], rootLD.UID)
	}

	if rootLD.TargetProperties.ModuleDir != "mainprog" {
		t.Errorf("root LD module_dir = %q, want %q", rootLD.TargetProperties.ModuleDir, "mainprog")
	}

	if rootLD.TargetProperties.ModuleType != mtBin {
		t.Errorf("root LD module_type = %q, want bin", rootLD.TargetProperties.ModuleType.string())
	}

	mainCC := nodesByOutput[mainCCOut]
	libAR := nodesByOutput[libARout]

	depSet := make(map[UID]struct{}, len(graphDeps(g, rootLD)))
	for _, d := range graphDeps(g, rootLD) {
		depSet[d] = struct{}{}
	}

	if _, ok := depSet[mainCC.UID]; !ok {
		t.Errorf("root LD deps %v missing main.cpp.o uid %q", graphDeps(g, rootLD), mainCC.UID)
	}

	if _, ok := depSet[libAR.UID]; !ok {
		t.Errorf("root LD deps %v missing thelib AR uid %q", graphDeps(g, rootLD), libAR.UID)
	}
}

func TestGen_UnittestFor_Synthetic(t *testing.T) {
	fs := newMemFS(map[string]string{
		"mod/ya.make":                                    "UNITTEST_FOR(thelib)\nSRCS(a_ut.cpp)\nEND()\n",
		"thelib/ya.make":                                 "LIBRARY()\nSRCS(lib.cpp)\nEND()\n",
		"build/cow/on/ya.make":                           "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(cow.cpp)\nEND()\n",
		"library/cpp/malloc/api/ya.make":                 "LIBRARY()\nSRCS(api.cpp)\nEND()\n",
		"library/cpp/malloc/tcmalloc/ya.make":            "LIBRARY()\nPEERDIR(library/cpp/malloc/api contrib/restricted/abseil-cpp contrib/libs/tcmalloc/malloc_extension contrib/libs/tcmalloc/no_percpu_cache)\nSRCS(tcmalloc.cpp)\nEND()\n",
		"contrib/restricted/abseil-cpp/ya.make":          "LIBRARY()\nSRCS(absl.cpp)\nEND()\n",
		"contrib/libs/tcmalloc/malloc_extension/ya.make": "LIBRARY()\nSRCS(ext.cpp)\nEND()\n",
		"contrib/libs/tcmalloc/no_percpu_cache/ya.make":  "LIBRARY()\nSRCS(npc.cpp)\nEND()\n",
		"library/cpp/testing/unittest_main/ya.make":      "LIBRARY()\nSRCS(main.cpp)\nEND()\n",
		"thelib/a_ut.cpp":                                "int a_ut() { return 0; }\n",
		"thelib/lib.cpp":                                 "int thelib() { return 0; }\n",
		"build/cow/on/cow.cpp":                           "int cow() { return 0; }\n",
		"library/cpp/malloc/api/api.cpp":                 "int malloc_api() { return 0; }\n",
		"library/cpp/malloc/tcmalloc/tcmalloc.cpp":       "int tcmalloc_lib() { return 0; }\n",
		"contrib/restricted/abseil-cpp/absl.cpp":         "int absl() { return 0; }\n",
		"contrib/libs/tcmalloc/malloc_extension/ext.cpp": "int malloc_ext() { return 0; }\n",
		"contrib/libs/tcmalloc/no_percpu_cache/npc.cpp":  "int no_percpu_cache() { return 0; }\n",
		"library/cpp/testing/unittest_main/main.cpp":     "int unittest_main() { return 0; }\n",
	})

	g := testGen(fs, "mod")

	byOut := make(map[string]*Node, len(g.Graph))
	for _, n := range g.Graph {
		if len(n.Outputs) > 0 {
			byOut[n.Outputs[0].string()] = n
		}
	}

	ld := byOut["$(B)/mod/mod"]
	if ld == nil {
		t.Fatalf("missing UNITTEST_FOR LD output $(B)/mod/mod")
	}

	if ld.KV.P != pkLD {
		t.Errorf("root node kv.p = %q, want LD", ld.KV.P)
	}

	if ld.TargetProperties.ModuleType != mtBin {
		t.Errorf("module_type = %q, want bin", ld.TargetProperties.ModuleType.string())
	}

	deps := make(map[UID]struct{}, len(graphDeps(g, ld)))
	for _, d := range graphDeps(g, ld) {
		deps[d] = struct{}{}
	}

	libAR := byOut["$(B)/thelib/libthelib.a"]
	if libAR == nil {
		t.Fatal("missing tested-lib AR $(B)/thelib/libthelib.a")
	}

	if _, ok := deps[libAR.UID]; !ok {
		t.Errorf("LD deps missing tested-lib AR uid %q (implicit PEERDIR($arg) not walked)", libAR.UID)
	}

	umAR := byOut["$(B)/library/cpp/testing/unittest_main/libcpp-testing-unittest_main.a"]
	if umAR == nil {
		t.Fatal("missing unittest_main AR")
	}

	if _, ok := deps[umAR.UID]; !ok {
		t.Errorf("LD deps missing unittest_main AR uid %q (implicit PEERDIR not walked)", umAR.UID)
	}

	cc := byOut["$(B)/mod/__/thelib/a_ut.cpp.o"]
	if cc == nil {
		t.Fatal("missing own CC $(B)/mod/__/thelib/a_ut.cpp.o")
	}

	if cc.TargetProperties.ModuleDir != "mod" {
		t.Fatalf("cc module_dir = %q, want mod", cc.TargetProperties.ModuleDir)
	}

	inputs := make([]string, 0, len(cc.flatInputs()))
	for _, in := range cc.flatInputs() {
		inputs = append(inputs, in.string())
	}
	if !slicesContains(inputs, "$(S)/thelib/a_ut.cpp") {
		t.Fatalf("cc inputs missing $(S)/thelib/a_ut.cpp: %v", inputs)
	}

	for _, c := range cc.Cmds {
		for _, a := range strStrs(c.CmdArgs.flat()) {
			if a == "-I$(S)/thelib" {
				t.Fatalf("own CC unexpectedly carries direct -I$(S)/thelib: cmds=%+v", cc.Cmds)
			}
		}
	}

	linkArgs := ld.Cmds[2].CmdArgs.flat()
	thelibIdx := indexOfArg(linkArgs, "thelib/libthelib.a")
	cowIdx := indexOfArg(linkArgs, "build/cow/on/libbuild-cow-on.a")
	if thelibIdx < 0 || cowIdx < 0 {
		t.Fatalf("LD link args missing expected tested-dir/program-default archives: %v", linkArgs)
	}
	if thelibIdx > cowIdx {
		t.Fatalf("tested-dir archive lands after build/cow/on in LD cmd: thelib=%d cow=%d args=%v", thelibIdx, cowIdx, linkArgs)
	}
}

func TestGen_RejectsUnsupportedMacro(t *testing.T) {
	fs := newMemFS(map[string]string{
		"mod/ya.make": "LIBRARY()\nTOTALLY_UNKNOWN(foo bar)\nSRCS(main.cpp)\nEND()\n",
	})

	exc := try(func() {
		testGen(fs, "mod")
	})

	if exc == nil {
		t.Fatal("expected exception for unsupported macro, got nil")
	}

	// A name outside the closed TOK set fails fast at parse (internTok), before
	// any gen-time modelling check.
	if !strings.Contains(exc.Error(), "unknown macro name") {
		t.Errorf("error %q does not contain 'unknown macro name'", exc.Error())
	}
}

func TestGen_RejectsMultipleModules(t *testing.T) {
	fs := newMemFS(map[string]string{
		"bad/ya.make": `LIBRARY()
SRCS(a.c)
PROGRAM()
SRCS(b.c)
END()
`,
	})

	exc := try(func() {
		testGen(fs, "bad")
	})

	if exc == nil {
		t.Fatal("expected exception")
	}

	if !strings.Contains(exc.Error(), "multiple modules") {
		t.Errorf("unexpected error: %v", exc)
	}
}

func TestGen_RejectsZeroModule(t *testing.T) {
	fs := newMemFS(map[string]string{
		"noop/ya.make": `SET(X y)
END()
`,
	})

	exc := try(func() {
		testGen(fs, "noop")
	})

	if exc == nil {
		t.Fatal("expected exception")
	}

	if !strings.Contains(exc.Error(), "no module declaration") {
		t.Errorf("unexpected error: %v", exc)
	}
}

func TestGen_RejectsProgramAsPeer(t *testing.T) {
	fs := newMemFS(map[string]string{
		"peerprog/ya.make": `PROGRAM()
SRCS(peer_main.cpp)
END()
`,
		"caller/ya.make": `PROGRAM()
PEERDIR(peerprog)
SRCS(caller_main.cpp)
END()
`,
	})

	exc := try(func() {
		testGen(fs, "caller")
	})

	if exc == nil {
		t.Fatal("expected exception")
	}

	if !strings.Contains(exc.Error(), "peers PROGRAM module") {
		t.Errorf("unexpected error: %v", exc)
	}
}

func TestGen_PeerdirDeclarationOrder_Preserved(t *testing.T) {
	fs := newMemFS(map[string]string{
		"mainprog/ya.make": `PROGRAM()
PEERDIR(zlib alib)
SRCS(main.cpp)
END()
`,
		"zlib/ya.make": `LIBRARY()
SRCS(zlib.c)
END()
`,
		"alib/ya.make": `LIBRARY()
SRCS(alib.c)
END()
`,
	})

	g := testGen(fs, "mainprog")

	var zlibIdx, alibIdx int = -1, -1

	for i, n := range g.Graph {
		if len(n.Outputs) > 0 {
			if strings.Contains(n.Outputs[0].string(), "/zlib/") && n.KV.P == pkAR {
				zlibIdx = i
			}
			if strings.Contains(n.Outputs[0].string(), "/alib/") && n.KV.P == pkAR {
				alibIdx = i
			}
		}
	}

	if zlibIdx == -1 || alibIdx == -1 {
		t.Fatalf("expected both zlib and alib AR nodes; zlibIdx=%d alibIdx=%d", zlibIdx, alibIdx)
	}

	if len(g.Graph) != 7 {
		t.Errorf("expected 7 nodes (3 CC + 2 AR + 1 LD + 1 vcs.json), got %d", len(g.Graph))
	}
}

func TestGen_MacroEvaluation_IfStmt_TakeThen(t *testing.T) {
	fs := newMemFS(map[string]string{
		"ifmod/ya.make": `LIBRARY()
IF (OS_LINUX)
    SRCS(linux.c)
ELSE()
    SRCS(other.c)
ENDIF()
END()
`,
	})

	g := testGen(fs, "ifmod")

	if len(g.Graph) != 3 {
		t.Fatalf("expected 3 nodes (1 CC + 1 AR + 1 vcs.json), got %d", len(g.Graph))
	}

	var ccInputs []string

	for _, n := range g.Graph {
		if n.KV.P == pkCC {
			ccInputs = append(ccInputs, vfsStrings(n.flatInputs())...)
		}
	}

	if len(ccInputs) != 1 {
		t.Fatalf("expected 1 CC input, got %d (%v)", len(ccInputs), ccInputs)
	}

	if !strings.Contains(ccInputs[0], "linux.c") {
		t.Errorf("CC input %q does not reference linux.c (THEN branch)", ccInputs[0])
	}

	if strings.Contains(ccInputs[0], "other.c") {
		t.Errorf("CC input %q unexpectedly references other.c (ELSE branch should be unreached)", ccInputs[0])
	}
}

func TestGen_MacroEvaluation_NoLibcFlag(t *testing.T) {
	fs := newMemFS(map[string]string{
		"nolibcmod/ya.make": `LIBRARY()
NO_LIBC()
NO_UTIL()
NO_RUNTIME()
SRCS(lib.c)
END()
`,
	})

	mf := throw2(parseFile(fs, "nolibcmod/ya.make"))

	d := collectModule(newIncludeParserManagerFS(fs, newSharedParseCache()), &DeDuper{}, "nolibcmod", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Kind: KindLib, Platform: testTargetP}))

	if !d.flags.NoLibc {
		t.Errorf("flags.NoLibc = false, want true (macro overlay should have flipped it)")
	}

	if !d.flags.NoUtil {
		t.Errorf("flags.NoUtil = false, want true")
	}

	if !d.flags.NoRuntime {
		t.Errorf("flags.NoRuntime = false, want true")
	}

	g := testGen(fs, "nolibcmod")

	if len(g.Graph) != 3 {
		t.Errorf("Gen produced %d nodes, want 3 (1 CC + 1 AR + 1 vcs.json)", len(g.Graph))
	}
}

func TestGen_AllocatorMacro_ResolvesToPeer(t *testing.T) {
	fs := newMemFS(map[string]string{
		"prog/ya.make":                        "PROGRAM()\nNO_PLATFORM()\nALLOCATOR(MIM)\nSRCS(main.cpp)\nEND()\n",
		"library/cpp/malloc/mimalloc/ya.make": "LIBRARY()\nNO_PLATFORM()\nSRCS(mim.cpp)\nEND()\n",
	})

	g := testGen(fs, "prog")

	var sawMimDir bool

	for _, n := range g.Graph {
		if n.TargetProperties.ModuleDir == "library/cpp/malloc/mimalloc" {
			sawMimDir = true

			break
		}
	}

	if !sawMimDir {
		t.Errorf("expected ALLOCATOR(MIM) to add library/cpp/malloc/mimalloc as peer; got Graph with no such module_dir")
	}
}

func TestGen_DefaultPeerdirs_SimpleLibrary(t *testing.T) {
	stubLib := "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(stub.cpp)\nEND()\n"

	files := map[string]string{
		"consumer/ya.make": "LIBRARY()\nSRCS(main.cpp)\nEND()\n",
	}
	for _, path := range []string{
		"contrib/libs/cxxsupp/builtins",
		"library/cpp/malloc/api",
		"contrib/libs/cxxsupp/libcxx",
		"contrib/libs/cxxsupp/libcxxrt",
		"contrib/libs/libunwind",
		"util",
	} {
		files[path+"/ya.make"] = stubLib
	}
	fs := newMemFS(files)

	plain := ModuleInstance{
		Path:     source("consumer"),
		Kind:     KindLib,
		Language: LangCPP,
		Platform: testTargetP,
	}

	wantDefaults := []string{
		"contrib/libs/linux-headers",
		"contrib/libs/cxxsupp/libcxx",
		"contrib/libs/cxxsupp/libcxxrt",
		"contrib/libs/libunwind",
		"util",
		"build/platform/clang",
		"build/platform/clang/clang-format",
		"build/platform/lld",
		"build/platform/python/ymake_python3",
	}

	gotDefaults := defaultPeerdirsForWithState(nil, plain, &ModuleData{})

	if !stringSlicesEqual(gotDefaults, wantDefaults) {
		t.Errorf("defaultPeerdirsForWithState(plain CPP) = %v, want %v", gotDefaults, wantDefaults)
	}

	g := testGen(fs, "consumer")

	emittedDirs := make(map[string]bool)

	for _, n := range g.Graph {
		if md := n.TargetProperties.ModuleDir; md != "" {
			emittedDirs[md] = true
		}
	}

	for _, want := range []string{
		"consumer",
		"contrib/libs/cxxsupp/libcxx",
		"contrib/libs/cxxsupp/libcxxrt",
		"contrib/libs/libunwind",
		"util",
	} {
		if !emittedDirs[want] {
			t.Errorf("graph missing module_dir %q; got %v", want, emittedDirs)
		}
	}
}

func TestGen_DefaultPeerdirs_HelperSuppression(t *testing.T) {

	fullSet := []string{
		"contrib/libs/cxxsupp/libcxx",
		"contrib/libs/cxxsupp/libcxxrt",
		"contrib/libs/libunwind",
		"util",
		"build/platform/clang",
		"build/platform/clang/clang-format",
		"build/platform/lld",
		"build/platform/python/ymake_python3",
	}

	cases := []struct {
		name  string
		mi    ModuleInstance
		flags FlagSet
		want  []string
	}{
		{
			name: "effective_no_platform",
			mi: ModuleInstance{
				Path:     source("x"),
				Kind:     KindLib,
				Language: LangCPP,
			},
			flags: FlagSet{NoLibc: true, NoRuntime: true, NoUtil: true},
			want:  []string{"contrib/libs/linux-headers", "build/platform/clang", "build/platform/clang/clang-format", "build/platform/lld", "build/platform/python/ymake_python3"},
		},
		{
			name: "explicit_no_platform",
			mi: ModuleInstance{
				Path:     source("x"),
				Kind:     KindLib,
				Language: LangCPP,
			},
			flags: FlagSet{NoPlatform: true},
			want:  []string{"contrib/libs/linux-headers", "build/platform/clang", "build/platform/clang/clang-format", "build/platform/lld", "build/platform/python/ymake_python3"},
		},
		{
			name: "no_libc_only",
			mi: ModuleInstance{
				Path:     source("x"),
				Kind:     KindLib,
				Language: LangCPP,
				Platform: testTargetP,
			},
			flags: FlagSet{NoLibc: true},

			want: append([]string{"contrib/libs/linux-headers"}, fullSet...),
		},
		{
			name: "no_runtime_only",
			mi: ModuleInstance{
				Path:     source("x"),
				Kind:     KindLib,
				Language: LangCPP,
				Platform: testTargetP,
			},
			flags: FlagSet{NoRuntime: true},

			want: []string{"contrib/libs/linux-headers", "util", "build/platform/clang", "build/platform/clang/clang-format", "build/platform/lld", "build/platform/python/ymake_python3"},
		},
		{
			name: "non_cpp",
			mi: ModuleInstance{
				Path:     source("x"),
				Kind:     KindLib,
				Language: LangProto,
			},
			want: []string{"contrib/libs/linux-headers"},
		},
		{
			name:  "no_util_only",
			mi:    ModuleInstance{Path: source("x"), Kind: KindLib, Language: LangCPP},
			flags: FlagSet{NoUtil: true},

			want: []string{
				"contrib/libs/linux-headers",
				"contrib/libs/cxxsupp/libcxx",
				"contrib/libs/cxxsupp/libcxxrt",
				"contrib/libs/libunwind",
				"build/platform/clang",
				"build/platform/clang/clang-format",
				"build/platform/lld",
				"build/platform/python/ymake_python3",
			},
		},

		// Runtime-ancestor paths no longer take a restricted peer set; suppression
		// is driven purely by NO_RUNTIME/NO_UTIL/NO_PLATFORM flags (none set here),
		// so these exercise self-exclusion: a module never peers itself.
		{
			name: "self_builtins_not_in_stack",
			mi: ModuleInstance{
				Path:     source("contrib/libs/cxxsupp/builtins"),
				Kind:     KindLib,
				Language: LangCPP,
			},
			want: []string{
				"contrib/libs/linux-headers",
				"contrib/libs/cxxsupp/libcxx",
				"contrib/libs/cxxsupp/libcxxrt",
				"contrib/libs/libunwind",
				"util",
				"build/platform/clang",
				"build/platform/clang/clang-format",
				"build/platform/lld",
				"build/platform/python/ymake_python3",
			},
		},
		{
			name: "self_malloc_api_not_in_stack",
			mi: ModuleInstance{
				Path:     source("library/cpp/malloc/api"),
				Kind:     KindLib,
				Language: LangCPP,
			},
			want: []string{
				"contrib/libs/linux-headers",
				"contrib/libs/cxxsupp/libcxx",
				"contrib/libs/cxxsupp/libcxxrt",
				"contrib/libs/libunwind",
				"util",
				"build/platform/clang",
				"build/platform/clang/clang-format",
				"build/platform/lld",
				"build/platform/python/ymake_python3",
			},
		},
		{
			name: "self_libcxx_excluded",
			mi: ModuleInstance{
				Path:     source("contrib/libs/cxxsupp/libcxx"),
				Kind:     KindLib,
				Language: LangCPP,
			},
			want: []string{
				"contrib/libs/linux-headers",
				"contrib/libs/cxxsupp/libcxxrt",
				"contrib/libs/libunwind",
				"util",
				"build/platform/clang",
				"build/platform/clang/clang-format",
				"build/platform/lld",
				"build/platform/python/ymake_python3",
			},
		},
		{
			name: "self_util_excluded",
			mi: ModuleInstance{
				Path:     source("util"),
				Kind:     KindLib,
				Language: LangCPP,
			},
			want: []string{
				"contrib/libs/linux-headers",
				"contrib/libs/cxxsupp/libcxx",
				"contrib/libs/cxxsupp/libcxxrt",
				"contrib/libs/libunwind",
				"build/platform/clang",
				"build/platform/clang/clang-format",
				"build/platform/lld",
				"build/platform/python/ymake_python3",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mi := c.mi
			if mi.Platform == nil {
				mi.Platform = testTargetP
			}

			got := defaultPeerdirsForWithState(nil, mi, &ModuleData{flags: c.flags})

			if !stringSlicesEqual(got, c.want) {
				t.Errorf("defaultPeerdirsForWithState(%+v, %+v) = %v, want %v", mi, c.flags, got, c.want)
			}
		})
	}
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

func TestGen_DefaultPeerdirs_ExplicitDuplicateDeduped(t *testing.T) {
	fs := newMemFS(map[string]string{
		"lib1/ya.make": `LIBRARY()
PEERDIR(contrib/libs/foolib)
SRCS(a.cpp)
END()
`,
		"contrib/libs/foolib/ya.make": `LIBRARY()
NO_LIBC()
NO_UTIL()
NO_RUNTIME()
NO_PLATFORM()
SRCS(stub.c)
END()
`,
	})

	g := testGen(fs, "lib1")

	var lib1AR *Node
	for _, n := range g.Graph {
		if n.KV.P == pkAR && n.TargetProperties.ModuleDir == "lib1" {
			lib1AR = n
			break
		}
	}

	if lib1AR == nil {
		t.Fatal("lib1 AR not found")
	}

	for _, ref := range graphDeps(g, lib1AR) {
		for _, n := range g.Graph {
			if n.UID == ref && n.KV.P == pkAR {
				t.Errorf("lib1 AR has AR-typed dep %q (module_dir=%q); reference invariant: zero AR-on-AR deps", ref, n.TargetProperties.ModuleDir)
			}
		}
	}
}

func TestGen_SrcDirRebasesSourceResolution(t *testing.T) {
	t.Run("with_srcdir", func(t *testing.T) {
		fs := newMemFS(map[string]string{
			"mymod/ya.make":     "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCDIR(other/dir)\nSRCS(foo.cpp)\nEND()\n",
			"other/dir/foo.cpp": "int foo() { return 0; }\n",
		})

		g := testGen(fs, "mymod")

		if len(g.Graph) != 3 {
			t.Fatalf("expected 3 nodes (1 CC + 1 AR + 1 vcs.json), got %d", len(g.Graph))
		}

		var ccNode *Node

		for _, n := range g.Graph {
			if n.KV.P == pkCC {
				ccNode = n
			}
		}

		if ccNode == nil {
			t.Fatal("no CC node emitted")
		}

		if ccNode.TargetProperties.ModuleDir != "mymod" {
			t.Errorf("CC module_dir = %q, want %q", ccNode.TargetProperties.ModuleDir, "mymod")
		}

		wantInput := "$(S)/other/dir/foo.cpp"

		if len(ccNode.flatInputs()) == 0 || ccNode.flatInputs()[0].string() != wantInput {
			t.Errorf("CC inputs = %v, want first = %q", ccNode.flatInputs(), wantInput)
		}

		wantOutput := "$(B)/mymod/__/other/dir/foo.cpp.o"

		if len(ccNode.Outputs) == 0 || ccNode.Outputs[0].string() != wantOutput {
			t.Errorf("CC outputs = %v, want first = %q", ccNode.Outputs, wantOutput)
		}
	})

	t.Run("without_srcdir_baseline", func(t *testing.T) {
		fs := newMemFS(map[string]string{
			"basemod/ya.make": "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(bar.cpp)\nEND()\n",
		})

		g := testGen(fs, "basemod")

		if len(g.Graph) != 3 {
			t.Fatalf("expected 3 nodes (1 CC + 1 AR + 1 vcs.json), got %d", len(g.Graph))
		}

		var ccNode *Node

		for _, n := range g.Graph {
			if n.KV.P == pkCC {
				ccNode = n
			}
		}

		if ccNode == nil {
			t.Fatal("no CC node emitted")
		}

		if ccNode.TargetProperties.ModuleDir != "basemod" {
			t.Errorf("CC module_dir = %q, want %q", ccNode.TargetProperties.ModuleDir, "basemod")
		}

		wantInput := "$(S)/basemod/bar.cpp"

		if len(ccNode.flatInputs()) == 0 || ccNode.flatInputs()[0].string() != wantInput {
			t.Errorf("CC inputs = %v, want first = %q", ccNode.flatInputs(), wantInput)
		}
	})

	t.Run("join_srcs_with_srcdir_library_non_ancestor", func(t *testing.T) {
		fs := newMemFS(map[string]string{
			"jsmod/ya.make": "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCDIR(other/dir)\nJOIN_SRCS(all.cpp s1.cpp s2.cpp)\nEND()\n",
		})

		g := testGen(fs, "jsmod")

		if len(g.Graph) != 4 {
			t.Fatalf("expected 4 nodes (1 JS + 1 CC + 1 AR + 1 vcs.json), got %d", len(g.Graph))
		}

		var jsNode, ccNode *Node

		for _, n := range g.Graph {
			switch n.KV.P.string() {
			case "JS":
				jsNode = n
			case "CC":
				ccNode = n
			}
		}

		if jsNode == nil {
			t.Fatal("no JS node emitted")
		}

		if ccNode == nil {
			t.Fatal("no CC node emitted")
		}

		if jsNode.TargetProperties.ModuleDir != "jsmod" {
			t.Errorf("JS module_dir = %q, want %q", jsNode.TargetProperties.ModuleDir, "jsmod")
		}

		if ccNode.TargetProperties.ModuleDir != "jsmod" {
			t.Errorf("CC module_dir = %q, want %q", ccNode.TargetProperties.ModuleDir, "jsmod")
		}
	})

	t.Run("ancestor_program_keeps_module_dir", func(t *testing.T) {
		fs := newMemFS(map[string]string{
			"tools/r6/bin/ya.make": "PROGRAM(myprog)\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nALLOCATOR(FAKE)\nSRCDIR(tools/r6)\nSRCS(main.cpp)\nEND()\n",
			"tools/r6/main.cpp":    "int main() { return 0; }\n",
		})

		g := testGen(fs, "tools/r6/bin")

		var ccNode *Node

		for _, n := range g.Graph {
			if n.KV.P == pkCC {
				ccNode = n
			}
		}

		if ccNode == nil {
			t.Fatal("no CC node emitted")
		}

		// module_dir is the module's own path; SRCDIR is a source-search dir, not
		// a module relocation.
		if ccNode.TargetProperties.ModuleDir != "tools/r6/bin" {
			t.Errorf("CC module_dir = %q, want %q", ccNode.TargetProperties.ModuleDir, "tools/r6/bin")
		}

		wantInput := "$(S)/tools/r6/main.cpp"
		if len(ccNode.flatInputs()) == 0 || ccNode.flatInputs()[0].string() != wantInput {
			t.Errorf("CC inputs = %v, want first = %q", ccNode.flatInputs(), wantInput)
		}

		wantOutput := "$(B)/tools/r6/bin/__/main.cpp.o"
		if len(ccNode.Outputs) == 0 || ccNode.Outputs[0].string() != wantOutput {
			t.Errorf("CC outputs = %v, want first = %q", ccNode.Outputs, wantOutput)
		}
	})

	t.Run("ancestor_program_nested_source_keeps_module_dir", func(t *testing.T) {
		fs := newMemFS(map[string]string{
			"tools/r6/bin/ya.make":  "PROGRAM(myprog)\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nALLOCATOR(FAKE)\nSRCDIR(tools/r6)\nSRCS(sub/main.cpp)\nEND()\n",
			"tools/r6/sub/main.cpp": "int main() { return 0; }\n",
		})

		g := testGen(fs, "tools/r6/bin")

		var ccNode *Node
		for _, n := range g.Graph {
			if n.KV.P == pkCC {
				ccNode = n
				break
			}
		}
		if ccNode == nil {
			t.Fatal("no CC node emitted")
		}

		if ccNode.TargetProperties.ModuleDir != "tools/r6/bin" {
			t.Errorf("CC module_dir = %q, want %q", ccNode.TargetProperties.ModuleDir, "tools/r6/bin")
		}

		if got := ccNode.flatInputs()[0].string(); got != "$(S)/tools/r6/sub/main.cpp" {
			t.Errorf("CC input = %q, want %q", got, "$(S)/tools/r6/sub/main.cpp")
		}

		if got := ccNode.Outputs[0].string(); got != "$(B)/tools/r6/bin/__/sub/main.cpp.o" {
			t.Errorf("CC output = %q, want %q", got, "$(B)/tools/r6/bin/__/sub/main.cpp.o")
		}
	})
}

func TestGen_PROGRAM_DefaultAllocator_TcmallocTc(t *testing.T) {
	fs := newMemFS(map[string]string{
		"myprog/ya.make": `PROGRAM(myprog)
NO_RUNTIME()
NO_UTIL()
SRCS(main.cpp)
END()
`,
		"library/cpp/malloc/tcmalloc/ya.make": `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PLATFORM()
SRCS(stub.cpp)
END()
`,
		"contrib/libs/tcmalloc/no_percpu_cache/ya.make": `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PLATFORM()
SRCS(stub.cpp)
END()
`,
	})

	host := newTestPlatform(OSLinux, ISAX8664, "yes")
	target := newTestPlatform(OSLinux, ISAX8664, "no")
	g := Gen(fs, "myprog", host, target, func(Warn) {})

	hasTcmalloc := false
	hasNoPercpu := false

	for _, n := range g.Graph {
		md := n.TargetProperties.ModuleDir

		if n.KV.P != pkAR {
			continue
		}

		switch md {
		case "library/cpp/malloc/tcmalloc":
			hasTcmalloc = true
		case "contrib/libs/tcmalloc/no_percpu_cache":
			hasNoPercpu = true
		}
	}

	if !hasTcmalloc {
		t.Errorf("expected AR with module_dir=library/cpp/malloc/tcmalloc; not found")
	}

	if !hasNoPercpu {
		t.Errorf("expected AR with module_dir=contrib/libs/tcmalloc/no_percpu_cache; not found")
	}
}

func TestGen_PROGRAM_ExplicitAllocator_NoTcmallocDefault(t *testing.T) {
	fs := newMemFS(map[string]string{
		"myprog/ya.make": `PROGRAM(myprog)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
ALLOCATOR(FAKE)
SRCS(main.cpp)
END()
`,
	})

	g := testGen(fs, "myprog")

	for _, n := range g.Graph {
		md := n.TargetProperties.ModuleDir

		if md == "library/cpp/malloc/tcmalloc" || md == "contrib/libs/tcmalloc/no_percpu_cache" {
			t.Errorf("PROGRAM with ALLOCATOR(FAKE) emitted unexpected node module_dir=%q (TCMALLOC_TC default must be suppressed)", md)
		}
	}
}

func TestGen_SrcdirSibling_KeepsModuleDir(t *testing.T) {
	fs := newMemFS(map[string]string{
		"mylib/ya.make": `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCDIR(other)
SRCS(src/foo.cpp)
END()
`,
		"other/src/foo.cpp": "int foo() { return 0; }\n",
	})

	g := testGen(fs, "mylib")

	var ccNode *Node

	for _, n := range g.Graph {
		if n.KV.P == pkCC {
			ccNode = n

			break
		}
	}

	if ccNode == nil {
		t.Fatal("no CC node emitted")
	}

	if got := ccNode.TargetProperties.ModuleDir; got != "mylib" {
		t.Errorf("CC module_dir = %q, want %q (sibling SRCDIR — module_dir stays at instance.Path)", got, "mylib")
	}

	wantInput := "$(S)/other/src/foo.cpp"

	if len(ccNode.flatInputs()) == 0 || ccNode.flatInputs()[0].string() != wantInput {
		t.Errorf("CC input = %v, want first = %q", ccNode.flatInputs(), wantInput)
	}

	wantOutput := "$(B)/mylib/__/other/src/foo.cpp.o"

	if len(ccNode.Outputs) == 0 || ccNode.Outputs[0].string() != wantOutput {
		t.Errorf("CC output = %v, want first = %q", ccNode.Outputs, wantOutput)
	}
}

func TestGen_SrcdirLocal_IgnoresSrcdir(t *testing.T) {
	fs := newMemFS(map[string]string{
		"mylib/ya.make": `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCDIR(other)
SRCS(local.c)
END()
`,
		"mylib/local.c": "int x;\n",
	})

	g := testGen(fs, "mylib")

	var ccNode *Node

	for _, n := range g.Graph {
		if n.KV.P == pkCC {
			ccNode = n

			break
		}
	}

	if ccNode == nil {
		t.Fatal("no CC node emitted")
	}

	wantInput := "$(S)/mylib/local.c"

	if len(ccNode.flatInputs()) == 0 || ccNode.flatInputs()[0].string() != wantInput {
		t.Errorf("CC input = %v, want first = %q (local-existing source must ignore SRCDIR)", ccNode.flatInputs(), wantInput)
	}

	wantOutput := "$(B)/mylib/local.c.o"

	if len(ccNode.Outputs) == 0 || ccNode.Outputs[0].string() != wantOutput {
		t.Errorf("CC output = %v, want first = %q", ccNode.Outputs, wantOutput)
	}
}

func TestGen_AddInclMixed_OwnPathStaysOwn(t *testing.T) {
	fs := newMemFS(map[string]string{
		"lib/ya.make":       "LIBRARY()\nADDINCL(\n    GLOBAL lib/include\n    lib/src\n)\nSRCS(lib.cpp)\nEND()\n",
		"lib/include/.keep": "",
		"consumer/ya.make":  "LIBRARY()\nPEERDIR(lib)\nSRCS(main.cpp)\nEND()\n",
	})

	g := testGen(fs, "consumer")

	var consumerCC *Node

	for _, n := range g.Graph {
		if n.KV.P == pkCC {
			for _, out := range n.Outputs {
				if strings.Contains(out.string(), "main.cpp.o") {
					consumerCC = n
					break
				}
			}
		}
	}

	if consumerCC == nil {
		t.Fatal("consumer CC node for main.cpp not found")
	}

	var iFlags []string

	if len(consumerCC.Cmds) > 0 {
		for _, arg := range strStrs(consumerCC.Cmds[0].CmdArgs.flat()) {
			if strings.HasPrefix(arg, "-I") {
				iFlags = append(iFlags, arg)
			}
		}
	}

	wantGlobal := "-I$(S)/lib/include"
	foundGlobal := false

	for _, f := range iFlags {
		if f == wantGlobal {
			foundGlobal = true
			break
		}
	}

	if !foundGlobal {
		t.Errorf("consumer CC -I flags = %v; want %q (GLOBAL ADDINCL must propagate to peers)", iFlags, wantGlobal)
	}

	wantAbsent := "-I$(S)/lib/src"

	for _, f := range iFlags {
		if f == wantAbsent {
			t.Errorf("consumer CC -I flags = %v; must NOT contain %q (module-own ADDINCL must stay module-own, PR-31 D13)", iFlags, wantAbsent)
			break
		}
	}
}

// TestGen_OneLevelAddIncl_DeclOrderPreserved verifies that when a peer has mixed
// ADDINCL(GLOBAL g ONE_LEVEL ol), the consumer sees g before ol — upstream ymake
// preserves declaration order within UserGlobal (GLOBAL and ONE_LEVEL dirs are added
// to UserGlobal in the order they appear in the ADDINCL call, not GLOBAL-first or
// ONE_LEVEL-first). A prior implementation always emitted all ONE_LEVEL before all
// GLOBAL for user peers, breaking declaration order for mixed peers.
func TestGen_OneLevelAddIncl_DeclOrderPreserved(t *testing.T) {
	fs := newMemFS(map[string]string{
		// Peer declares GLOBAL first, ONE_LEVEL second. Upstream preserves that order.
		"peerlib/ya.make":              "LIBRARY()\nADDINCL(\n    GLOBAL\n    peerlib/global_include\n    ONE_LEVEL\n    peerlib/onelevel_include\n)\nSRCS(peerlib.cpp)\nEND()\n",
		"peerlib/peerlib.cpp":          "",
		"peerlib/global_include/.keep": "", // directory must exist for filterExistingSourceDirs
		"consumer/ya.make":             "LIBRARY()\nPEERDIR(peerlib)\nSRCS(consumer.cpp)\nEND()\n",
		"consumer/consumer.cpp":        "",
	})

	g := testGen(fs, "consumer")

	var consumerCC *Node
	for _, n := range g.Graph {
		if n.KV.P == pkCC {
			for _, out := range n.Outputs {
				if strings.Contains(out.string(), "consumer.cpp.o") {
					consumerCC = n
					break
				}
			}
		}
	}

	if consumerCC == nil {
		t.Fatal("consumer CC node for consumer.cpp.o not found")
	}
	if len(consumerCC.Cmds) == 0 {
		t.Fatal("consumer CC node has no commands")
	}

	args := consumerCC.Cmds[0].CmdArgs.flat()
	globalIdx := indexOfArg(args, "-I$(S)/peerlib/global_include")
	oneLevelIdx := indexOfArg(args, "-I$(S)/peerlib/onelevel_include")

	if globalIdx == -1 {
		t.Fatal("consumer CC missing -I$(S)/peerlib/global_include (GLOBAL addincl from peer must propagate)")
	}
	if oneLevelIdx == -1 {
		t.Fatal("consumer CC missing -I$(S)/peerlib/onelevel_include (ONE_LEVEL addincl from peer must propagate)")
	}
	// GLOBAL was declared before ONE_LEVEL; upstream UserGlobal preserves that order.
	if globalIdx > oneLevelIdx {
		t.Errorf("GLOBAL addincl at idx=%d comes AFTER ONE_LEVEL at idx=%d; want declaration order (GLOBAL before ONE_LEVEL)", globalIdx, oneLevelIdx)
	}
}

// TestGen_ImplicitOwnGlobal_BeforeOneLevelAddIncl verifies that a peer's implicit
// own-global include dir (from SRCS(file.h.in)) appears before a later ADDINCL(ONE_LEVEL)
// in the consumer's -I list. Upstream UserGlobal accumulates own-global dirs in declaration
// order — generated-header build dirs are added when the SRCS statement is processed, so
// they precede ADDINCL(ONE_LEVEL) declared later in the same ya.make.
func TestGen_ImplicitOwnGlobal_BeforeOneLevelAddIncl(t *testing.T) {
	fs := newMemFS(map[string]string{
		// peerlib: generated header declared before ONE_LEVEL ADDINCL.
		// Upstream UserGlobal: generated-header build dir comes first, ONE_LEVEL second.
		"peerlib/ya.make":       "LIBRARY()\nSRCS(gen.h.in)\nADDINCL(\n    ONE_LEVEL\n    peerlib/onelevel_dir\n)\nSRCS(peerlib.cpp)\nEND()\n",
		"peerlib/gen.h.in":      "",
		"peerlib/peerlib.cpp":   "",
		"consumer/ya.make":      "LIBRARY()\nPEERDIR(peerlib)\nSRCS(consumer.cpp)\nEND()\n",
		"consumer/consumer.cpp": "",
	})

	g := testGen(fs, "consumer")

	var consumerCC *Node
	for _, n := range g.Graph {
		if n.KV.P == pkCC {
			for _, out := range n.Outputs {
				if strings.Contains(out.string(), "consumer.cpp.o") {
					consumerCC = n
					break
				}
			}
		}
	}

	if consumerCC == nil {
		t.Fatal("consumer CC node for consumer.cpp.o not found")
	}
	if len(consumerCC.Cmds) == 0 {
		t.Fatal("consumer CC node has no commands")
	}

	args := consumerCC.Cmds[0].CmdArgs.flat()
	// addGeneratedHeaderInclude("peerlib", "gen.h", d) produces Build("peerlib") = $(B)/peerlib.
	genHdrIdx := indexOfArg(args, "-I$(B)/peerlib")
	oneLevelIdx := indexOfArg(args, "-I$(S)/peerlib/onelevel_dir")

	if genHdrIdx == -1 {
		t.Fatal("consumer CC missing -I$(B)/peerlib (generated-header build dir from peer)")
	}
	if oneLevelIdx == -1 {
		t.Fatal("consumer CC missing -I$(S)/peerlib/onelevel_dir (ONE_LEVEL addincl from peer)")
	}
	// Generated-header build dir was added before ONE_LEVEL; upstream UserGlobal preserves that order.
	if genHdrIdx > oneLevelIdx {
		t.Errorf("generated-header build include at idx=%d comes AFTER ONE_LEVEL at idx=%d; want own-global (UserGlobal) order before ONE_LEVEL", genHdrIdx, oneLevelIdx)
	}
}

// TestGen_ConfigureFileOwnGlobal_AfterExplicitAddIncl verifies that a peer's CF-generated
// include dir (from CONFIGURE_FILE(non-header dst)) appears AFTER an explicit ADDINCL(GLOBAL)
// in the consumer's -I list, even when CONFIGURE_FILE is declared first in the ya.make.
//
// Upstream ymake resolves addincl;output (from CONFIGURE_FILE) AFTER all explicit ADDINCL
// statements, regardless of source-level declaration order. Our cfAddIncl deferred buffer
// correctly models this: CF dirs are merged into addInclGlobal and addInclUserGlobal AFTER
// collectStmts processes all ADDINCL statements.
//
// This test also verifies the CF dir IS present in AddInclUserGlobal (i.e. propagated to
// direct consumers in the UserGlobal phase), not silently dropped.
func TestGen_ConfigureFileOwnGlobal_AfterExplicitAddIncl(t *testing.T) {
	fs := newMemFS(map[string]string{
		// peerlib: CONFIGURE_FILE (non-header dst) declared BEFORE ADDINCL(GLOBAL).
		// Upstream: ADDINCL dirs appear first in UserGlobal, CF dirs deferred after.
		"peerlib/ya.make":          "LIBRARY()\nCONFIGURE_FILE(config.cfg.in config.cfg)\nADDINCL(\n    GLOBAL\n    peerlib/global_dir\n)\nSRCS(peerlib.cpp)\nEND()\n",
		"peerlib/config.cfg.in":    "",
		"peerlib/peerlib.cpp":      "",
		"peerlib/global_dir/.keep": "", // must exist for filterExistingSourceDirs
		"consumer/ya.make":         "LIBRARY()\nPEERDIR(peerlib)\nSRCS(consumer.cpp)\nEND()\n",
		"consumer/consumer.cpp":    "",
	})

	g := testGen(fs, "consumer")

	var consumerCC *Node
	for _, n := range g.Graph {
		if n.KV.P == pkCC {
			for _, out := range n.Outputs {
				if strings.Contains(out.string(), "consumer.cpp.o") {
					consumerCC = n
					break
				}
			}
		}
	}

	if consumerCC == nil {
		t.Fatal("consumer CC node for consumer.cpp.o not found")
	}
	if len(consumerCC.Cmds) == 0 {
		t.Fatal("consumer CC node has no commands")
	}

	args := consumerCC.Cmds[0].CmdArgs.flat()
	// addGeneratedHeaderIncludeCF("peerlib", "config.cfg", d) produces Build("peerlib") = $(B)/peerlib.
	cfBuildIdx := indexOfArg(args, "-I$(B)/peerlib")
	globalIdx := indexOfArg(args, "-I$(S)/peerlib/global_dir")

	if cfBuildIdx == -1 {
		t.Fatal("consumer CC missing -I$(B)/peerlib (CF-generated build dir from peer must be in UserGlobal)")
	}
	if globalIdx == -1 {
		t.Fatal("consumer CC missing -I$(S)/peerlib/global_dir (explicit ADDINCL GLOBAL from peer)")
	}
	// Upstream defers CF-generated include dirs after explicit ADDINCL; GLOBAL must appear first.
	if globalIdx > cfBuildIdx {
		t.Errorf("ADDINCL GLOBAL at idx=%d comes AFTER CF build dir at idx=%d; want explicit ADDINCL before deferred CF dir", globalIdx, cfBuildIdx)
	}
}

// TestGen_OneLevelAddIncl_AppearsInPeerIncludeSlot verifies that ADDINCL(ONE_LEVEL)
// from a direct PEERDIR peer lands in the peer-include slot, AFTER the platform
// linux-headers (which leads peerAddInclGlobal as a language default), NOT in the
// own-include slot before it.
// Upstream: ONE_LEVEL dirs go into UserGlobal → PropagateTo → UserGlobalPropagated of the
// consumer, which renders after the consumer's own LocalUserGlobal and after the
// platform linux-headers global addincl.
func TestGen_OneLevelAddIncl_AppearsInPeerIncludeSlot(t *testing.T) {
	fs := newMemFS(map[string]string{
		"peerlib/ya.make":                    "LIBRARY()\nADDINCL(\n    ONE_LEVEL\n    peerlib/include\n)\nSRCS(peerlib.cpp)\nEND()\n",
		"peerlib/peerlib.cpp":                "",
		"consumer/ya.make":                   "LIBRARY()\nPEERDIR(peerlib)\nSRCS(consumer.cpp)\nEND()\n",
		"consumer/consumer.cpp":              "",
		"contrib/libs/linux-headers/ya.make": "LIBRARY()\nADDINCL(\n    GLOBAL contrib/libs/linux-headers\n    GLOBAL contrib/libs/linux-headers/_nf\n)\nEND()\n",
	})

	g := testGen(fs, "consumer")

	var consumerCC *Node
	for _, n := range g.Graph {
		if n.KV.P == pkCC {
			for _, out := range n.Outputs {
				if strings.Contains(out.string(), "consumer.cpp.o") {
					consumerCC = n
					break
				}
			}
		}
	}

	if consumerCC == nil {
		t.Fatal("consumer CC node for consumer.cpp.o not found")
	}

	if len(consumerCC.Cmds) == 0 {
		t.Fatal("consumer CC node has no commands")
	}

	args := consumerCC.Cmds[0].CmdArgs.flat()

	linuxHeadersIdx := indexOfArg(args, "-I$(S)/contrib/libs/linux-headers")
	oneLevelIdx := indexOfArg(args, "-I$(S)/peerlib/include")

	if oneLevelIdx == -1 {
		t.Fatal("consumer CC missing -I$(S)/peerlib/include (ONE_LEVEL addincl from peer should propagate to direct consumer)")
	}
	if linuxHeadersIdx == -1 {
		t.Fatal("consumer CC missing -I$(S)/contrib/libs/linux-headers (expected from the platform linux-headers language default)")
	}
	// ONE_LEVEL from peer must appear AFTER linux-headers, not before. Before this fix,
	// ONE_LEVEL was appended to d.addIncl (own bag) which lands before the peer slot;
	// it lands in peerAddInclGlobal after the leading linux-headers default.
	if oneLevelIdx < linuxHeadersIdx {
		t.Errorf("ONE_LEVEL addincl from peer at idx=%d, before linux-headers at idx=%d; want AFTER (peer-include slot, not own-include slot)", oneLevelIdx, linuxHeadersIdx)
	}
}

func TestIsRuntimeAncestor_LiteralOnly(t *testing.T) {
	literals := []string{
		"contrib/libs/libc_compat",
		"contrib/libs/linuxvdso",
		"contrib/libs/cxxsupp/builtins",
		"contrib/libs/cxxsupp/libcxx",
		"contrib/libs/cxxsupp/libcxxrt",
		"contrib/libs/cxxsupp/libcxxabi",
		"contrib/libs/cxxsupp/libcxxabi-parts",
		"contrib/libs/libunwind",
		"library/cpp/malloc/api",
		"library/cpp/sanitizer/include",
		"util",
	}

	for _, p := range literals {
		if !isRuntimeAncestor(p) {
			t.Errorf("isRuntimeAncestor(%q) = false, want true (literal entry)", p)
		}
	}

	subtree := []string{
		"util/charset",
		"util/datetime/parser.rl6.cpp.o",
		"contrib/libs/cxxsupp/libcxxabi-parts/src",
		"contrib/libs/libunwind/private",
	}

	for _, p := range subtree {
		if isRuntimeAncestor(p) {
			t.Errorf("isRuntimeAncestor(%q) = true, want false (subtree extension dropped in PR-33 D01)", p)
		}
	}
}

func TestGen_SRC_RejectsZeroArgs(t *testing.T) {
	fs := newMemFS(map[string]string{
		"mod/ya.make": "LIBRARY()\nSRC()\nEND()\n",
	})

	exc := try(func() {
		testGen(fs, "mod")
	})

	if exc == nil {
		t.Fatal("expected exception for SRC() with no args, got nil")
	}

	if !strings.Contains(exc.Error(), "SRC()") {
		t.Errorf("error %q does not mention SRC()", exc.Error())
	}
}

func TestEvalCond_ARCH_ARM64_Aliased(t *testing.T) {
	inst := ModuleInstance{Kind: KindLib, Platform: testTargetP}
	env := buildIfEnv(inst)

	if !evalCond(&ExprIdent{Name: "ARCH_ARM64"}, env) {
		t.Errorf("EvalCond(ARCH_ARM64) on aarch64 instance = false, want true (alias for ARCH_AARCH64)")
	}

	if !evalCond(&ExprIdent{Name: "ARCH_AARCH64"}, env) {
		t.Errorf("EvalCond(ARCH_AARCH64) on aarch64 instance = false, want true")
	}

	hostInst := ModuleInstance{Kind: KindLib, Platform: testHostP}
	hostEnv := buildIfEnv(hostInst)

	if evalCond(&ExprIdent{Name: "ARCH_ARM64"}, hostEnv) {
		t.Errorf("EvalCond(ARCH_ARM64) on x86_64 instance = true, want false")
	}

	if !evalCond(&ExprIdent{Name: "ARCH_X86_64"}, hostEnv) {
		t.Errorf("EvalCond(ARCH_X86_64) on x86_64 instance = false, want true")
	}
}

func TestGen_ProtoAstStylePipelineExpandsLowercaseVarsAndRootedPaths(t *testing.T) {
	files := map[string]string{}
	writeFile := func(rel, body string) {
		files[rel] = body
	}

	writeToolProgram(files, "contrib/tools/protoc/bin", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")

	writeFile("proto/ya.make", `LIBRARY()
SET(antlr_output ${ARCADIA_BUILD_ROOT}/${MODDIR})
SET(antlr_templates ${antlr_output}/templates)
SET(sql_grammar ${antlr_output}/Grammar.g)
SET(PROTOC_PATH contrib/tools/protoc/bin)

CONFIGURE_FILE(${ARCADIA_ROOT}/templates/Java.stg.in ${antlr_templates}/Java/Java.stg)
CONFIGURE_FILE(${ARCADIA_ROOT}/templates/Grammar.g.in ${sql_grammar})

RUN_ANTLR4(
    ${sql_grammar}
    -lib .
    -o ${antlr_output}
    IN ${sql_grammar} ${antlr_templates}/Java/Java.stg
	    OUT_NOAUTO Generated.proto
	    CWD ${antlr_output}
)

RUN_PROGRAM(
	    $PROTOC_PATH
	    -I=${CURDIR} -I=${ARCADIA_ROOT} -I=${ARCADIA_BUILD_ROOT} -I=${ARCADIA_ROOT}/contrib/libs/protobuf/src
	    --cpp_out=${ARCADIA_BUILD_ROOT} --cpp_styleguide_out=${ARCADIA_BUILD_ROOT}
	    --plugin=protoc-gen-cpp_styleguide=contrib/tools/protoc/plugins/cpp_styleguide
	    Generated.proto
	    IN Generated.proto
	    TOOL contrib/tools/protoc/plugins/cpp_styleguide
	    OUT_NOAUTO Generated.pb.h Generated.pb.cc
	    CWD ${antlr_output}
)

RUN_PYTHON3(
	    ${ARCADIA_ROOT}/tools/multiproto.py Generated
	    IN Generated.pb.h Generated.pb.cc
	    OUT_NOAUTO Generated.code0.cc Generated.main.h
	    CWD ${antlr_output}
)

SRCS(Generated.code0.cc)
END()
`)
	writeFile("templates/Java.stg.in", "java template\n")
	writeFile("templates/Grammar.g.in", "grammar Generated;\n")
	writeFile("tools/multiproto.py", "print('ok')\n")
	writeFile("build/scripts/configure_file.py", "print('cfg')\n")
	writeFile("build/scripts/stdout2stderr.py", "print('stderr')\n")
	writeFile("contrib/java/antlr/antlr4/antlr.jar", "")

	g := testGen(newMemFS(files), "proto")

	cfTemplate := findGraphNodeByOutputs(t, g, "$(B)/proto/templates/Java/Java.stg")
	cfGrammar := findGraphNodeByOutputs(t, g, "$(B)/proto/Grammar.g")
	antlr := findGraphNodeByOutputs(t, g, "$(B)/proto/Generated.proto")
	protoc := findGraphNodeByOutputs(t, g, "$(B)/proto/Generated.pb.h", "$(B)/proto/Generated.pb.cc")
	py := findGraphNodeByOutputs(t, g, "$(B)/proto/Generated.code0.cc", "$(B)/proto/Generated.main.h")
	cc := findGraphNodeByOutputs(t, g, "$(B)/proto/Generated.code0.cc.o")
	ar := findGraphNodeByOutputs(t, g, "$(B)/proto/libproto.a")

	if got := cfTemplate.flatInputs()[1].string(); got != "$(S)/templates/Java.stg.in" {
		t.Fatalf("template CF input = %q, want $(S)/templates/Java.stg.in", got)
	}
	if got := cfTemplate.Cmds[0].CmdArgs.flat()[3].string(); got != "$(B)/proto/templates/Java/Java.stg" {
		t.Fatalf("template CF output arg = %q, want $(B)/proto/templates/Java/Java.stg", got)
	}
	if got := cfGrammar.Cmds[0].CmdArgs.flat()[3].string(); got != "$(B)/proto/Grammar.g" {
		t.Fatalf("grammar CF output arg = %q, want $(B)/proto/Grammar.g", got)
	}

	if got := antlr.Cmds[0].CmdArgs.flat()[5].string(); got != "$(B)/proto/Grammar.g" {
		t.Fatalf("antlr grammar arg = %q, want $(B)/proto/Grammar.g", got)
	}
	if got := antlr.Cmds[0].CmdArgs.flat()[9].string(); got != "$(B)/proto" {
		t.Fatalf("antlr output dir arg = %q, want $(B)/proto", got)
	}
	if got := antlr.Cmds[0].Cwd.string(); got != "$(B)/proto" {
		t.Fatalf("antlr cwd = %q, want $(B)/proto", got)
	}

	if got := protoc.Cmds[0].CmdArgs.flat()[0].string(); got != "$(B)/contrib/tools/protoc/bin/protoc" {
		t.Fatalf("protoc tool arg = %q, want $(B)/contrib/tools/protoc/bin/protoc", got)
	}
	wantPluginArg := "--plugin=protoc-gen-cpp_styleguide=$(B)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide"
	if !containsString(strStrs(protoc.Cmds[0].CmdArgs.flat()), wantPluginArg) {
		t.Fatalf("protoc cmd args missing %q: %v", wantPluginArg, protoc.Cmds[0].CmdArgs.flat())
	}
	if !nodeHasInput(protoc, "$(B)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide") {
		t.Fatalf("protoc inputs missing built plugin binary: %v", protoc.flatInputs())
	}
	if nodeHasInput(protoc, "$(S)/contrib/tools/protoc/plugins/cpp_styleguide") {
		t.Fatalf("protoc inputs still contain source-root plugin path: %v", protoc.flatInputs())
	}

	if got := py.flatInputs()[0].string(); got != "$(S)/tools/multiproto.py" {
		t.Fatalf("python script input = %q, want $(S)/tools/multiproto.py", got)
	}
	if got := py.flatInputs()[1].string(); got != "$(B)/proto/Generated.pb.h" {
		t.Fatalf("python generated header input = %q, want $(B)/proto/Generated.pb.h", got)
	}
	if got := py.flatInputs()[2].string(); got != "$(B)/proto/Generated.pb.cc" {
		t.Fatalf("python generated source input = %q, want $(B)/proto/Generated.pb.cc", got)
	}
	if got := py.Cmds[0].CmdArgs.flat()[1].string(); got != "$(S)/tools/multiproto.py" {
		t.Fatalf("python script arg = %q, want $(S)/tools/multiproto.py", got)
	}
	if got := py.Cmds[0].Cwd.string(); got != "$(B)/proto" {
		t.Fatalf("python cwd = %q, want $(B)/proto", got)
	}

	if !containsString(strStrs(cc.Cmds[0].CmdArgs.flat()), "$(B)/proto/Generated.code0.cc") {
		t.Fatalf("cc cmd args missing built generated source: %v", cc.Cmds[0].CmdArgs.flat())
	}
	if containsString(strStrs(cc.Cmds[0].CmdArgs.flat()), "$(S)/proto/Generated.code0.cc") {
		t.Fatalf("cc cmd args still contain source-root generated source: %v", cc.Cmds[0].CmdArgs.flat())
	}
	if !nodeHasInput(cc, "$(B)/proto/Generated.code0.cc") {
		t.Fatalf("cc inputs missing built generated source: %v", cc.flatInputs())
	}
	for _, want := range []string{
		"$(S)/tools/multiproto.py",
		"$(S)/build/scripts/stdout2stderr.py",
		"$(S)/contrib/java/antlr/antlr4/antlr.jar",
		"$(S)/build/scripts/configure_file.py",
		"$(S)/templates/Java.stg.in",
		"$(S)/templates/Grammar.g.in",
	} {
		if !nodeHasInput(cc, want) {
			t.Fatalf("cc inputs missing generator closure %q: %v", want, cc.flatInputs())
		}
	}

	if nodeHasInput(ar, "$(S)/proto/Generated.code0.cc") {
		t.Fatalf("ar inputs still contain source-root generated source: %v", ar.flatInputs())
	}
	if nodeHasInput(ar, "$(B)/proto/Generated.code0.cc") {
		t.Fatalf("ar inputs still contain build-root generated source: %v", ar.flatInputs())
	}

	for _, absent := range []string{
		"$(S)/tools/multiproto.py",
		"$(S)/contrib/java/antlr/antlr4/antlr.jar",
		"$(S)/templates/Java.stg.in",
		"$(S)/templates/Grammar.g.in",
	} {
		if nodeHasInput(ar, absent) {
			t.Fatalf("ar inputs must not contain generator-closure source %q: %v", absent, ar.flatInputs())
		}
	}

	assertNodeHasNoRawProtoAstPlaceholders := func(node *Node) {
		t.Helper()

		var values []string
		for _, input := range node.flatInputs() {
			values = append(values, input.string())
		}
		for _, output := range node.Outputs {
			values = append(values, output.string())
		}
		for _, cmd := range node.Cmds {
			values = append(values, strStrs(cmd.CmdArgs.flat())...)
			if cmd.Cwd != 0 {
				values = append(values, cmd.Cwd.string())
			}
			if cmd.Stdout != 0 {
				values = append(values, cmd.Stdout.string())
			}
		}

		for _, value := range values {
			if strings.Contains(value, "${") {
				t.Fatalf("%s contains unresolved placeholder %q", node.KV.P, value)
			}
			if strings.Contains(value, "/$(S)/") || strings.Contains(value, "/$(B)/") {
				t.Fatalf("%s contains duplicated rooted path %q", node.KV.P, value)
			}
		}
	}

	assertNodeHasNoRawProtoAstPlaceholders(cfTemplate)
	assertNodeHasNoRawProtoAstPlaceholders(cfGrammar)
	assertNodeHasNoRawProtoAstPlaceholders(antlr)
	assertNodeHasNoRawProtoAstPlaceholders(protoc)
	assertNodeHasNoRawProtoAstPlaceholders(py)
	assertNodeHasNoRawProtoAstPlaceholders(cc)
	assertNodeHasNoRawProtoAstPlaceholders(ar)
}

func TestGen_ProtoLibrary_TransitiveHeadersNoAddsDepsHeaderAndEnumUsesGeneratedPBHeader(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "protos/ya.make", `PROTO_LIBRARY()
SET(PROTOC_TRANSITIVE_HEADERS "no")
GRPC()
GENERATE_ENUM_SERIALIZATION(main.pb.h)
SRCS(
    dep.proto
    main.proto
)
END()
`)
	writeTestModuleFile(files, "protos/dep.proto", `syntax = "proto3";
package test;
message Dep {
  string value = 1;
}
`)
	writeTestModuleFile(files, "protos/main.proto", `syntax = "proto3";
package test;
import "dep.proto";
enum Mode {
  MODE_UNSPECIFIED = 0;
  MODE_READY = 1;
}
message Main {
  Dep dep = 1;
  Mode mode = 2;
}
service TestService {
  rpc Ping(Main) returns (Main);
}
`)
	writeTestModuleFile(files, "app/ya.make", `LIBRARY()
PEERDIR(protos)
SRCS(use.cpp)
END()
`)
	writeTestModuleFile(files, "app/use.cpp", `#include <protos/main.deps.pb.h>
int use() { return 0; }
`)

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "contrib/tools/protoc/plugins/grpc_cpp", "grpc_cpp")
	writeToolProgram(files, "tools/enum_parser/enum_parser", "enum_parser")
	writeTestModuleFile(files, "tools/enum_parser/enum_serialization_runtime/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(runtime.cpp)\nEND()\n")
	writeTestModuleFile(files, "tools/enum_parser/enum_serialization_runtime/runtime.cpp", "int runtime(){return 0;}\n")

	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/grpc/ya.make", "LIBRARY()\nSRCS(grpc.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/grpc/grpc.cpp", "int grpc(){return 0;}\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(newMemFS(files), "app")

	pb := findGraphNodeByOutputs(t, g,
		"$(B)/protos/main.pb.h",
		"$(B)/protos/main.pb.cc",
		"$(B)/protos/main.deps.pb.h",
		"$(B)/protos/main.grpc.pb.cc",
		"$(B)/protos/main.grpc.pb.h",
	)
	if !containsString(strStrs(pb.Cmds[0].CmdArgs.flat()), "--cpp_out=proto_h=true:$(B)/") {
		t.Fatalf("pb cmd args missing lite-header cpp_out flag: %v", pb.Cmds[0].CmdArgs.flat())
	}

	useCC := mustNodeByOutput(t, g, "$(B)/app/use.cpp.o")
	mainPB := mustNodeByOutput(t, g, "$(B)/protos/main.pb.h")
	depPB := mustNodeByOutput(t, g, "$(B)/protos/dep.pb.h")
	ar := mustNodeByOutput(t, g, "$(B)/protos/libprotos.a")
	for _, want := range []string{
		"$(B)/protos/main.deps.pb.h",
		"$(B)/protos/main.pb.h",
		"$(B)/protos/dep.pb.h",
	} {
		if !nodeHasInput(useCC, want) {
			t.Fatalf("use.cpp.o inputs missing %q: %#v", want, useCC.flatInputs())
		}
	}
	for _, want := range []UID{mainPB.UID, depPB.UID} {
		if !slices.Contains(graphDeps(g, useCC), want) {
			t.Fatalf("use.cpp.o deps missing %q: %v", want, graphDeps(g, useCC))
		}
	}

	en := mustNodeByOutput(t, g, "$(B)/protos/main.pb.h_serialized.cpp")
	if !nodeHasInput(en, "$(B)/protos/main.pb.h") {
		t.Fatalf("enum node inputs missing generated pb.h: %#v", en.flatInputs())
	}
	if nodeHasInput(en, "$(S)/protos/main.pb.h") {
		t.Fatalf("enum node still consumes source-root pb.h: %#v", en.flatInputs())
	}
	if !slices.Contains(graphDeps(g, en), mainPB.UID) {
		t.Fatalf("enum node deps missing pb producer uid %q: %v", mainPB.UID, graphDeps(g, en))
	}
	if !slices.Contains(graphDeps(g, en), depPB.UID) {
		t.Fatalf("enum node deps missing imported pb producer uid %q: %v", depPB.UID, graphDeps(g, en))
	}
	if got := en.TargetProperties.ModuleTag; got != tagCppProto {
		t.Fatalf("enum node module_tag = %q, want cpp_proto", got.string())
	}
	if !nodeHasInput(en, "$(B)/protos/dep.pb.h") {
		t.Fatalf("enum node inputs missing imported pb.h dep.pb.h: %#v", en.flatInputs())
	}
	// Protobuf runtime headers (<google/protobuf/message.h> →
	// contrib/libs/protobuf/src/...) resolve only through the real protobuf
	// ADDINCL tree, exercised byte-exact by the sg gates. This mock has no such
	// tree, so the input is not asserted here.

	if !nodeHasInput(ar, "$(B)/protos/main.pb.h_serialized.cpp.o") {
		t.Fatalf("archive missing enum serialization object: %#v", ar.flatInputs())
	}
}

func testGen(fs FS, targetDir string) *Graph {
	return testGenContour(fs, targetDir, true)
}

// testGenInternal builds under the internal (non-opensource) contour, where
// LINK_OR_COPY_SO_CMD is not emitted for plain programs — so fs_tools.py is not
// a link input via emitCopy and only the BUNDLE MOVE_FILE mechanism can put it
// on the link node.
func testGenInternal(fs FS, targetDir string) *Graph {
	return testGenContour(fs, targetDir, false)
}

func testGenContour(fs FS, targetDir string, opensource bool) *Graph {
	host := newTestPlatform(OSLinux, ISAX8664, "yes")
	targetFlags := make(map[string]string, len(testToolchainFlags)+1)
	for k, v := range testToolchainFlags {
		targetFlags[k] = v
	}
	if !opensource {
		delete(targetFlags, "OPENSOURCE")
	}
	targetFlags["PIC"] = "no"
	target := newPlatform(fs, OSLinux, ISAAArch64, targetFlags, "", "")
	return Gen(fs, targetDir, host, target, func(Warn) {})
}

func TestCollectModule_YqlAbiMacrosAppendCXXFlags(t *testing.T) {
	fs := newMemFS(map[string]string{
		"mod/ya.make": `LIBRARY()
YQL_LAST_ABI_VERSION()
YQL_ABI_VERSION(2 44 0)
SRCS(lib.cpp)
END()
`,
	})
	mf := throw2(parseFile(fs, "mod/ya.make"))
	d := collectModule(newIncludeParserManagerFS(fs, newSharedParseCache()), &DeDuper{}, "mod", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: source("mod"), Kind: KindLib, Platform: testTargetP}))

	want := []string{
		"-DUSE_CURRENT_UDF_ABI_VERSION",
		"-DUDF_ABI_VERSION_MAJOR=2",
		"-DUDF_ABI_VERSION_MINOR=44",
		"-DUDF_ABI_VERSION_PATCH=0",
	}
	if !reflect.DeepEqual(argStrs(d.cxxFlags), want) {
		t.Fatalf("cxxFlags = %#v, want %#v", argStrs(d.cxxFlags), want)
	}
}

func TestCollectModule_YqlUdfStaticRoutesSrcsToGlobal(t *testing.T) {
	fs := newMemFS(map[string]string{
		"mod/ya.make": `YQL_UDF_CONTRIB(my_udf)
SRCS(lib.cpp nested/extra.cpp)
PEERDIR(custom/peer)
END()
`,
	})
	mf := throw2(parseFile(fs, "mod/ya.make"))
	d := collectModule(newIncludeParserManagerFS(fs, newSharedParseCache()), &DeDuper{}, "mod", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: source("mod"), Kind: KindLib, Platform: testTargetP}))

	if d.moduleStmt == nil || d.moduleStmt.Name != tokYqlUdfContrib {
		t.Fatalf("moduleStmt = %#v, want YQL_UDF_CONTRIB", d.moduleStmt)
	}
	if !equalStrings(strStrings(d.moduleStmt.Args), []string{"my_udf"}) {
		t.Fatalf("module args = %v, want [my_udf]", d.moduleStmt.Args)
	}
	if len(d.srcs) != 0 {
		t.Fatalf("srcs = %v, want empty (SRCS must alias to GLOBAL_SRCS)", d.srcs)
	}
	if !equalStrings(strStrings(d.globalSrcs), []string{"lib.cpp", "nested/extra.cpp"}) {
		t.Fatalf("globalSrcs = %v, want [lib.cpp nested/extra.cpp]", d.globalSrcs)
	}
	if !equalStrings(strStrings(d.peerdirs), []string{
		"yql/essentials/public/udf",
		"yql/essentials/public/udf/support",
		"custom/peer",
	}) {
		t.Fatalf("peerdirs = %v", d.peerdirs)
	}
}

func TestCollectModule_ProtocFatalWarningsAddsProtoFlag(t *testing.T) {
	fs := newMemFS(map[string]string{
		"proto/ya.make": `PROTO_LIBRARY()
PROTOC_FATAL_WARNINGS()
SRCS(test.proto)
END()
`,
	})
	mf := throw2(parseFile(fs, "proto/ya.make"))
	d := collectModule(newIncludeParserManagerFS(fs, newSharedParseCache()), &DeDuper{}, "proto", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: source("proto"), Kind: KindLib, Platform: testTargetP}))

	if !equalStrings(argStrs(d.protocFlags), []string{"--fatal_warnings"}) {
		t.Fatalf("protocFlags = %v, want [--fatal_warnings]", d.protocFlags)
	}
}

func TestCollectModule_CPPProtoPluginRecorded(t *testing.T) {
	fs := newMemFS(map[string]string{
		"proto/ya.make": `PROTO_LIBRARY()
CPP_PROTO_PLUGIN(validation ydb/public/lib/validation .validation.pb.h DEPS ydb/public/api/protos/annotations EXTRA_OUT_FLAG lite=true)
SRCS(test.proto)
END()
`,
	})
	mf := throw2(parseFile(fs, "proto/ya.make"))
	d := collectModule(newIncludeParserManagerFS(fs, newSharedParseCache()), &DeDuper{}, "proto", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: source("proto"), Kind: KindLib, Platform: testTargetP}))

	if len(d.cppProtoPlugins) != 1 {
		t.Fatalf("cppProtoPlugins = %d, want 1", len(d.cppProtoPlugins))
	}

	plugin := d.cppProtoPlugins[0]
	if plugin.Name != "validation" {
		t.Fatalf("plugin.Name = %q, want validation", plugin.Name)
	}
	if plugin.ToolPath != "ydb/public/lib/validation" {
		t.Fatalf("plugin.ToolPath = %q, want ydb/public/lib/validation", plugin.ToolPath)
	}
	if !equalStrings(plugin.OutputSuffixes, []string{".validation.pb.h"}) {
		t.Fatalf("plugin.OutputSuffixes = %v, want [.validation.pb.h]", plugin.OutputSuffixes)
	}
	if !equalStrings(plugin.Deps, []string{"ydb/public/api/protos/annotations"}) {
		t.Fatalf("plugin.Deps = %v, want [ydb/public/api/protos/annotations]", plugin.Deps)
	}
	if plugin.ExtraOutFlag != "lite=true" {
		t.Fatalf("plugin.ExtraOutFlag = %q, want lite=true", plugin.ExtraOutFlag)
	}
	if !containsString(strStrings(d.peerdirs), "ydb/public/api/protos/annotations") {
		t.Fatalf("peerdirs = %v, want ydb/public/api/protos/annotations", d.peerdirs)
	}
}

// TestCollectModule_ProtoCmdPeersRecorded pins the proto plugin-runtime peers a
// PROTO_LIBRARY records (the C++ proto plugins' DEPS, in plugin order) AND that
// the declared PEERDIR keeps its slot in d.peerdirs (link order untouched).
// CPP_EVLOG registers event2cpp with DEPS library/cpp/eventlog; that eventlog
// runtime leads the ADDINCL closure (see walkPeersForGlobalAddIncl). The base
// protobuf runtime is intentionally absent — it keeps its declared placement.
func TestCollectModule_ProtoCmdPeersRecorded(t *testing.T) {
	fs := newMemFS(map[string]string{
		"proto/ya.make": `PROTO_LIBRARY()
PEERDIR(some/declared/peer)
SRCS(test.proto)
CPP_EVLOG()
END()
`,
	})
	mf := throw2(parseFile(fs, "proto/ya.make"))
	d := collectModule(newIncludeParserManagerFS(fs, newSharedParseCache()), &DeDuper{}, "proto", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: source("proto"), Kind: KindLib, Platform: testTargetP}))

	if !equalStrings(strStrings(d.protoCmdPeers), []string{"library/cpp/eventlog"}) {
		t.Fatalf("protoCmdPeers = %v, want [library/cpp/eventlog]", strStrings(d.protoCmdPeers))
	}

	// d.peerdirs (link/archive order) keeps the declared peer first; the front
	// peers are a subset, not a reordering of, the declared closure.
	if len(d.peerdirs) == 0 || d.peerdirs[0].string() != "some/declared/peer" {
		t.Fatalf("d.peerdirs[0] = %v, want some/declared/peer first (link order intact): %v", d.peerdirs, strStrings(d.peerdirs))
	}
}

func TestCollectModule_FlatcFlagsRecorded(t *testing.T) {
	fs := newMemFS(map[string]string{
		"flatcmod/ya.make": `LIBRARY()
FLATC_FLAGS(--scoped-enums --gen-all)
SRCS(Schema.fbs)
END()
`,
	})
	mf := throw2(parseFile(fs, "flatcmod/ya.make"))
	d := collectModule(newIncludeParserManagerFS(fs, newSharedParseCache()), &DeDuper{}, "flatcmod", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: source("flatcmod"), Kind: KindLib, Platform: testTargetP}))

	if !equalStrings(argStrs(d.flatcFlags), []string{"--scoped-enums", "--gen-all"}) {
		t.Fatalf("flatcFlags = %v, want [--scoped-enums --gen-all]", d.flatcFlags)
	}
}

func TestCollectModule_UseCommonGoogleApisAddsPeer(t *testing.T) {
	fs := newMemFS(map[string]string{
		"proto/ya.make": `PROTO_LIBRARY()
USE_COMMON_GOOGLE_APIS(api/annotations)
SRCS(test.proto)
END()
`,
	})
	mf := throw2(parseFile(fs, "proto/ya.make"))
	d := collectModule(newIncludeParserManagerFS(fs, newSharedParseCache()), &DeDuper{}, "proto", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: source("proto"), Kind: KindLib, Platform: testTargetP}))

	if !containsString(strStrings(d.peerdirs), "contrib/libs/googleapis-common-protos") {
		t.Fatalf("peerdirs = %v, want contrib/libs/googleapis-common-protos", d.peerdirs)
	}
}

func TestCollectModule_Py3ProgramSplitsPyMainFromPySrcs(t *testing.T) {
	fs := newMemFS(map[string]string{
		"pytool/ya.make": `PY3_PROGRAM()
PY_SRCS(
    MAIN
    __main__.py
)
END()
`,
	})
	mf := throw2(parseFile(fs, "pytool/ya.make"))

	bin := collectModule(newIncludeParserManagerFS(fs, newSharedParseCache()), &DeDuper{}, "pytool", KindBin, mf.Stmts, buildIfEnv(ModuleInstance{Path: source("pytool"), Kind: KindBin, Platform: testTargetP}))
	if got := bin.pyMain; got == nil || got.string() != "pytool.__main__:main" {
		t.Fatalf("bin pyMain = %#v, want pytool.__main__:main", got)
	}
	// PY_SRCS stays populated on KindBin since 50cd9e9: the PROGRAM-side
	// emitResourceObjcopy needs len(d.pySrcs)>0 to enter its hasKvOnly
	// branch and surface the PY_MAIN objcopy_<hash>.o into LD inputs.
	if !equalStrings(strStrings(bin.pySrcs), []string{"__main__.py"}) {
		t.Fatalf("bin pySrcs = %v, want [__main__.py]", bin.pySrcs)
	}

	lib := collectModule(newIncludeParserManagerFS(fs, newSharedParseCache()), &DeDuper{}, "pytool", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: source("pytool"), Kind: KindLib, Platform: testTargetP}))
	if lib.pyMain != nil {
		t.Fatalf("lib pyMain = %#v, want nil", lib.pyMain)
	}
	if !equalStrings(strStrings(lib.pySrcs), []string{"__main__.py"}) {
		t.Fatalf("lib pySrcs = %v, want [__main__.py]", lib.pySrcs)
	}
}

func TestCollectModule_CopyExpandsVarsIntoAutoSources(t *testing.T) {
	fs := newMemFS(map[string]string{
		"copymod/ya.make": `LIBRARY()
SET(ORIG_SRC_DIR src)
SET(ORIG_SOURCES a.cpp b.h)
COPY(
    WITH_CONTEXT
    AUTO
    FROM ${ORIG_SRC_DIR}
    ${ORIG_SOURCES}
    OUTPUT_INCLUDES dep.h
)
END()
`,
	})
	mf := throw2(parseFile(fs, "copymod/ya.make"))
	d := collectModule(newIncludeParserManagerFS(fs, newSharedParseCache()), &DeDuper{}, "copymod", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: source("copymod"), Kind: KindLib, Platform: testTargetP}))

	if !equalStrings(strStrings(d.srcs), []string{"a.cpp", "b.h"}) {
		t.Fatalf("srcs = %v, want [a.cpp b.h]", d.srcs)
	}
	if len(d.copyFiles) != 2 {
		t.Fatalf("len(copyFiles) = %d, want 2", len(d.copyFiles))
	}
	if d.copyFiles[0].Src != "src/a.cpp" || d.copyFiles[1].Src != "src/b.h" {
		t.Fatalf("copyFiles srcs = %#v", d.copyFiles)
	}
}

func vfsStringsT3(in []VFS) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = v.string()
	}
	return out
}

// vfsInputsContainAll reports whether got contains every entry of want,
// order- and duplicate-agnostic (mirrors the gate's normalized comparison).
func vfsInputsContainAll(got, want []string) bool {
	set := make(map[string]struct{}, len(got))
	for _, g := range got {
		set[g] = struct{}{}
	}
	for _, w := range want {
		if _, ok := set[w]; !ok {
			return false
		}
	}
	return true
}

func TestCollectModule_BisonGeneratedHeaderExportedGlobally(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "gen/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(pire/re_parser.y)
END()
`)
	writeTestModuleFile(files, "gen/pire/re_parser.y", `%{
#include "re_lexer.h"
%}
%%
`)
	writeTestModuleFile(files, "gen/pire/re_lexer.h", "#pragma once\n")

	fs := newMemFS(files)
	mf := throw2(parseFile(fs, "gen/ya.make"))
	instance := ModuleInstance{Path: source("gen"), Kind: KindLib, Platform: testTargetP}
	d := collectModule(newIncludeParserManagerFS(fs, newSharedParseCache()), &DeDuper{}, "gen", KindLib, mf.Stmts, buildIfEnv(instance))

	for _, got := range [][]VFS{d.addIncl, d.addInclGlobal} {
		if !slices.Contains(vfsStrings(got), "$(B)/gen/pire") {
			t.Fatalf("generated bison include dir missing from %v", vfsStrings(got))
		}
	}
}

const t17SwigTargetDir = "contrib/tools/swig"

var t20ResourceMacroRE = regexp.MustCompile(`\$\((CLANG|LLD_ROOT|YMAKE_PYTHON3)-[0-9]+\)`)

type T20RefCmd struct {
	CmdArgs []string          `json:"cmd_args"`
	Env     map[string]string `json:"env"`
}

type T20RefNode struct {
	Cmds    []T20RefCmd `json:"cmds"`
	Deps    []string    `json:"deps"`
	Inputs  []string    `json:"inputs"`
	Outputs []string    `json:"outputs"`
	UID     string      `json:"uid"`
}

func TestCollectModule_SETAPPENDRPathGlobal(t *testing.T) {
	content := "RESOURCES_LIBRARY()\nSET_APPEND(RPATH_GLOBAL '-Wl,-rpath,${\"$\"}ORIGIN')\nEND()\n"
	fs := newMemFS(map[string]string{
		"mod/ya.make": content,
	})
	mf := throw2(parseFile(fs, "mod/ya.make"))
	instance := ModuleInstance{Path: source("mod"), Kind: KindLib, Platform: testTargetP}
	d := collectModule(newIncludeParserManagerFS(fs, newSharedParseCache()), &DeDuper{}, "mod", KindLib, mf.Stmts, buildIfEnv(instance))

	want := []string{"-Wl,-rpath,$ORIGIN"}
	if !reflect.DeepEqual(argStrs(d.rpathFlagsGlobal), want) {
		t.Fatalf("rpathFlagsGlobal mismatch:\n  got:  %#v\n  want: %#v", argStrs(d.rpathFlagsGlobal), want)
	}
}

type T20RefGraph struct {
	nodes []*T20RefNode
	byUID map[string]*T20RefNode
}

func findGraphNodeByOutputs(t *testing.T, g *Graph, wantOutputs ...string) *Node {
	t.Helper()

	for _, node := range g.Graph {
		if len(node.Outputs) != len(wantOutputs) {
			continue
		}

		match := true
		for i, out := range node.Outputs {
			if out.string() != wantOutputs[i] {
				match = false

				break
			}
		}

		if match {
			return node
		}
	}

	t.Fatalf("graph node with outputs %v not found", wantOutputs)
	return nil
}

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return strings.ToLower(enchex.EncodeToString(sum[:]))
}

func TestGen_ResourceBindirRunProgramCarriesInputClosure(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeToolProgram(files, "tools/json_gen/bin", "json_gen")

	writeTestModuleFile(files, "dep/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(dep.cpp dep.h)
END()
`)
	writeTestModuleFile(files, "dep/dep.cpp", "int dep(){return 0;}\n")
	writeTestModuleFile(files, "dep/dep.h", "#pragma once\n")

	writeTestModuleFile(files, "db/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(dep)
RUN_PROGRAM(
    tools/json_gen/bin
        --output
        ${BINDIR}/unused.cpp
        --json-output
        ${BINDIR}/data.json
        gen.h
    IN gen.h
    OUT_NOAUTO ${BINDIR}/data.json
)
RESOURCE(
    ${BINDIR}/data.json /data.json
)
END()
`)
	writeTestModuleFile(files, "db/gen.h", `#pragma once
#include <dep/dep.h>
`)

	g := testGen(newMemFS(files), "db")

	pr := mustNodeByOutput(t, g, "$(B)/db/data.json")
	if !nodeHasInput(pr, "$(S)/db/gen.h") {
		t.Fatalf("pr inputs missing direct gen.h input: %#v", pr.flatInputs())
	}
	if !nodeHasInput(pr, "$(S)/dep/dep.h") {
		t.Fatalf("pr inputs missing transitive dep/dep.h closure: %#v", pr.flatInputs())
	}

	objcopy := findNodeByOutputPrefix(g, "$(B)/db/objcopy_")
	if objcopy == nil {
		t.Fatal("graph is missing db objcopy output")
	}
	if !nodeHasInput(objcopy, "$(B)/db/data.json") {
		t.Fatalf("objcopy inputs missing build-root data.json: %#v", objcopy.flatInputs())
	}
	// The objcopy embeds data.json and reads only it; the producer's transitive
	// $(S) compile closure (dep/dep.h) is NOT an objcopy input. Upstream over-emits
	// it as a cache-key-only input; we do not, and normalization strips it from the
	// reference side (see bugs/20260615-upstream-resource-objcopy-overemit.md).
	if nodeHasInput(objcopy, "$(S)/dep/dep.h") {
		t.Fatalf("objcopy should not carry producer-closure dep/dep.h: %#v", objcopy.flatInputs())
	}
}

func writeToolProgram(files map[string]string, modulePath, binaryName string) {
	files[modulePath+"/ya.make"] = "PROGRAM(" + binaryName + ")\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(main.cpp)\nEND()\n"
	files[modulePath+"/main.cpp"] = "int main(){return 0;}\n"
}

func writeTestModuleFile(files map[string]string, rel, content string) {
	files[rel] = content
}

func mustNodeByOutput(t *testing.T, g *Graph, output string) *Node {
	t.Helper()

	for _, n := range g.Graph {
		if len(n.Outputs) > 0 && n.Outputs[0].string() == output {
			return n
		}
	}

	t.Fatalf("graph is missing output %q", output)
	return nil
}

func nodeByOutput(g *Graph, output string) *Node {
	for _, n := range g.Graph {
		if len(n.Outputs) > 0 && n.Outputs[0].string() == output {
			return n
		}
	}

	return nil
}

func mustNodeByAnyOutput(t *testing.T, g *Graph, output string) *Node {
	t.Helper()

	for _, n := range g.Graph {
		for _, o := range n.Outputs {
			if o.string() == output {
				return n
			}
		}
	}

	t.Fatalf("graph is missing a node with output %q", output)
	return nil
}

func findNodeByOutputPrefix(g *Graph, prefix string) *Node {
	for _, n := range g.Graph {
		if len(n.Outputs) > 0 && strings.HasPrefix(n.Outputs[0].string(), prefix) {
			return n
		}
	}

	return nil
}

func nodeHasInput(n *Node, input string) bool {
	for _, got := range n.flatInputs() {
		if got.string() == input {
			return true
		}
	}

	return false
}

func indexOfArg(args []STR, want string) int {
	for i, arg := range args {
		if arg.string() == want {
			return i
		}
	}

	return -1
}

func TestIsHeaderSource_ExtendedHeaderExtensions(t *testing.T) {
	for _, src := range []string{
		"a.h",
		"a.hh",
		"a.hpp",
		"a.hxx",
		"a.ipp",
		"a.ixx",
		"a.inl",
	} {
		if !isHeaderSource(src) {
			t.Fatalf("isHeaderSource(%q) = false, want true", src)
		}
	}
}

func TestProgramBinaryName_Py3ProgramBinArgWins(t *testing.T) {
	inst := ModuleInstance{Path: source("tools/py3cc/slow"), Kind: KindBin, Platform: testTargetP}

	// PY3_PROGRAM_BIN(py3cc) links as its argument (the opensource reference:
	// tools/py3cc/slow/bin INCLUDEd into tools/py3cc/slow links as .../py3cc). In the
	// internal contour the dir is a PREBUILT_PROGRAM instead, named .../slow by the
	// module-dir basename via genPrebuiltProgram — a distinct module type.
	got := programBinaryName(inst, &ModuleStmt{Name: tokPy3ProgramBin, Args: STRS("py3cc")})
	if got != "py3cc" {
		t.Fatalf("PY3_PROGRAM_BIN binary name = %q, want py3cc", got)
	}

	// Without an argument it resolves to the module-dir basename — upstream's
	// REALPRJNAME default, the single source of truth for the binary output, the
	// .ldcref/.map, and the <REALPRJNAME>.<LANG>.component.sbom name.
	if got := programBinaryName(inst, &ModuleStmt{Name: tokPy3ProgramBin}); got != "slow" {
		t.Fatalf("PY3_PROGRAM_BIN no-arg binary name = %q, want slow (dir-basename fallback)", got)
	}

	// A plain PROGRAM(foo) still honours its explicit name argument.
	if got := programBinaryName(inst, &ModuleStmt{Name: tokProgram, Args: STRS("foo")}); got != "foo" {
		t.Fatalf("PROGRAM(foo) binary name = %q, want foo", got)
	}
}

// TestProgramBinaryName_UnnamedProgramRealPrjName reproduces T-17: a from-source
// PROGRAM() with no name argument (e.g. contrib/libs/flatbuffers64/flatc) must
// resolve REALPRJNAME to the module-dir basename so its _GEN_SBOM_COMPONENT is
// named <basename>.<LANG>.component.sbom. Before the fix programBinaryName
// returned "", yielding a degenerate ".CPP.component.sbom" output that diverged
// from the reference's "flatc.CPP.component.sbom".
func TestProgramBinaryName_UnnamedProgramRealPrjName(t *testing.T) {
	inst := ModuleInstance{Path: source("contrib/libs/flatbuffers64/flatc"), Kind: KindBin, Platform: testTargetP}

	if got := programBinaryName(inst, &ModuleStmt{Name: tokProgram}); got != "flatc" {
		t.Fatalf("unnamed PROGRAM() REALPRJNAME = %q, want flatc (dir basename)", got)
	}

	// The SBOM component name is REALPRJNAME + ".<LANG>.component.sbom"; an empty
	// REALPRJNAME would produce the degenerate ".CPP.component.sbom" seen in sg7.
	if got := programBinaryName(inst, &ModuleStmt{Name: tokProgram}); got == "" {
		t.Fatal("unnamed PROGRAM() REALPRJNAME is empty; SBOM component would be \".CPP.component.sbom\"")
	}
}

// TestGen_CppEvlog_PropagatesEventlogGlobalAddIncl reproduces T-8: a
// PROTO_LIBRARY with CPP_EVLOG() must gain library/cpp/eventlog as a C++ peer
// (upstream: CPP_EVLOG -> CPP_PROTO_PLUGIN0(... DEPS library/cpp/eventlog) ->
// PEERDIR). eventlog transitively PEERDIRs a leaf declaring
// ADDINCL(GLOBAL leaf/include); a consumer PEERDIRing the proto must therefore
// compile with -I$(S)/leaf/include. Before modelling CPP_EVLOG (it was a
// no-op stub) the eventlog peer edge was absent, so the GLOBAL include never
// reached the consumer's CC command. Mirrors the cookie_cleaner.cpp.o sg7 case.
func TestGen_CppEvlog_PropagatesEventlogGlobalAddIncl(t *testing.T) {
	files := map[string]string{}
	writeTestModuleFile(files, "leaf/ya.make", "LIBRARY()\nADDINCL(GLOBAL leaf/include)\nSRCS(leaf.cpp)\nEND()\n")
	writeTestModuleFile(files, "leaf/leaf.cpp", "int leaf(){return 0;}\n")
	writeTestModuleFile(files, "leaf/include/.keep", "")
	writeTestModuleFile(files, "library/cpp/eventlog/ya.make", "LIBRARY()\nPEERDIR(leaf)\nSRCS(eventlog.cpp)\nEND()\n")
	writeTestModuleFile(files, "library/cpp/eventlog/eventlog.cpp", "int eventlog(){return 0;}\n")

	writeTestModuleFile(files, "protos/ya.make", "PROTO_LIBRARY()\nSET(PROTOC_TRANSITIVE_HEADERS \"no\")\nCPP_EVLOG()\nSRCS(main.proto)\nEND()\n")
	writeTestModuleFile(files, "protos/main.proto", "syntax = \"proto3\";\npackage test;\nmessage Main { string value = 1; }\n")

	writeTestModuleFile(files, "consumer/ya.make", "LIBRARY()\nPEERDIR(protos)\nSRCS(consumer.cpp)\nEND()\n")
	writeTestModuleFile(files, "consumer/consumer.cpp", "int consumer(){return 0;}\n")

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "tools/event2cpp", "event2cpp")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(newMemFS(files), "consumer")

	consumerCC := mustNodeByOutput(t, g, "$(B)/consumer/consumer.cpp.o")
	if len(consumerCC.Cmds) == 0 {
		t.Fatal("consumer CC node has no commands")
	}
	args := consumerCC.Cmds[0].CmdArgs.flat()
	want := "-I$(S)/leaf/include"
	if indexOfArg(args, want) == -1 {
		var iFlags []string
		for _, a := range strStrs(args) {
			if strings.HasPrefix(a, "-I") {
				iFlags = append(iFlags, a)
			}
		}
		t.Fatalf("consumer CC -I flags = %v; want %q (CPP_EVLOG must peer library/cpp/eventlog so its transitive GLOBAL ADDINCL reaches the consumer)", iFlags, want)
	}
}

func TestGen_AutoincludeClangWarningsRequiresModuleLangCPP(t *testing.T) {
	fs := newMemFS(map[string]string{
		"build/conf/autoincludes.json": `["ads"]`,
		"ads/linters.make.inc": `IF (MODULE_LANG == CPP)
    IF (MODDIR STARTS_WITH ads/bsyeti)
        CLANG_WARNINGS(-Wimplicit-fallthrough)
    ENDIF()
ENDIF()
`,
		"ads/bsyeti/lib/ya.make": `LIBRARY()
CXXFLAGS(-Wimplicit-fallthrough -Wdeprecated-this-capture)
SRCS(x.cpp)
END()
`,
		"ads/bsyeti/lib/x.cpp": "int x() { return 0; }\n",
	})

	g := testGen(fs, "ads/bsyeti/lib")

	var cc *Node
	for _, n := range g.Graph {
		if n.KV.P != pkCC {
			continue
		}
		for _, out := range n.Outputs {
			if strings.Contains(out.string(), "x.cpp.o") {
				cc = n
			}
		}
	}
	if cc == nil {
		t.Fatal("CC node for ads/bsyeti/lib/x.cpp.o not found")
	}

	var args []string
	for _, c := range cc.Cmds {
		args = append(args, strStrs(c.CmdArgs.flat())...)
	}

	fallthroughCount := 0
	firstFallthrough := -1
	deprecatedIdx := -1
	deprecatedCount := 0
	for i, a := range args {
		switch a {
		case "-Wimplicit-fallthrough":
			fallthroughCount++
			if firstFallthrough < 0 {
				firstFallthrough = i
			}
		case "-Wdeprecated-this-capture":
			deprecatedCount++
			deprecatedIdx = i
		}
	}

	if fallthroughCount != 2 {
		t.Fatalf("-Wimplicit-fallthrough count = %d, want 2 (early CLANG_WARNINGS slot + CXXFLAGS tail); args=%v", fallthroughCount, args)
	}
	if deprecatedCount != 1 {
		t.Fatalf("-Wdeprecated-this-capture count = %d, want 1; args=%v", deprecatedCount, args)
	}
	if firstFallthrough >= deprecatedIdx {
		t.Fatalf("first -Wimplicit-fallthrough (idx %d) must precede -Wdeprecated-this-capture (idx %d); args=%v", firstFallthrough, deprecatedIdx, args)
	}
}
