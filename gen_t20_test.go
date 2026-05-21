package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

var t20ResourceMacroRE = regexp.MustCompile(`\$\((CLANG|LLD_ROOT|YMAKE_PYTHON3)-[0-9]+\)`)

type t20RefCmd struct {
	CmdArgs []string `json:"cmd_args"`
	Env     map[string]string `json:"env"`
}

type t20RefNode struct {
	Cmds    []t20RefCmd `json:"cmds"`
	Deps    []string    `json:"deps"`
	Inputs  []string    `json:"inputs"`
	Outputs []string    `json:"outputs"`
	UID     string      `json:"uid"`
}

func TestCollectModule_SETAPPENDRPathGlobal(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "mod")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir mod: %v", err)
	}

	content := "RESOURCES_LIBRARY()\nSET_APPEND(RPATH_GLOBAL '-Wl,-rpath,${\"$\"}ORIGIN')\nEND()\n"
	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), []byte(content), 0o644); err != nil {
		t.Fatalf("write ya.make: %v", err)
	}

	fs := NewFS(root)
	mf := Throw2(ParseFile(fs, filepath.Join(modDir, "ya.make")))
	instance := ModuleInstance{Path: "mod", Kind: KindLib, Platform: testTargetP}
	d := collectModule(fs, "mod", KindLib, mf.Stmts, buildIfEnv(instance))

	want := []string{"-Wl,-rpath,$ORIGIN"}
	if !reflect.DeepEqual(d.rpathFlagsGlobal, want) {
		t.Fatalf("rpathFlagsGlobal mismatch:\n  got:  %#v\n  want: %#v", d.rpathFlagsGlobal, want)
	}
}

func TestGen_LibiconvDynamic_InputsMatchReference(t *testing.T) {
	const targetDir = "contrib/libs/libiconv/dynamic"

	if _, err := os.Stat(filepath.Join(sourceRoot, targetDir, "ya.make")); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference ya.make not present at %s/%s/ya.make", sourceRoot, targetDir)
		}

		t.Fatalf("stat ya.make: %v", err)
	}

	our := testGenT20Tool(sourceRoot, targetDir)
	ourNode := findGraphNodeByOutputs(t, our, "$(B)/contrib/libs/libiconv/dynamic/libiconv.so")
	refNode := loadT20RefNode(t, "$(BUILD_ROOT)/contrib/libs/libiconv/dynamic/libiconv.so")

	gotInputs := sortedStrings(vfsStrings(ourNode.Inputs))
	wantInputs := sortedStrings(normalizeT20Strings(refNode.Inputs))

	if !reflect.DeepEqual(gotInputs, wantInputs) {
		t.Fatalf("libiconv inputs mismatch:\n  got:  %#v\n  want: %#v", gotInputs, wantInputs)
	}

	for _, want := range []string{
		"$(S)/build/scripts/c_templates/svn_interface.c",
		"$(S)/build/scripts/c_templates/svnversion.h",
		"$(S)/build/scripts/fs_tools.py",
		"$(S)/build/scripts/link_exe.py",
		"$(S)/build/scripts/vcs_info.py",
	} {
		if !slices.Contains(gotInputs, want) {
			t.Fatalf("libiconv inputs missing %q", want)
		}
	}
}

func TestGen_BisonLinkTailMatchesReference(t *testing.T) {
	const targetDir = "contrib/tools/bison"

	if _, err := os.Stat(filepath.Join(sourceRoot, targetDir, "ya.make")); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference ya.make not present at %s/%s/ya.make", sourceRoot, targetDir)
		}

		t.Fatalf("stat ya.make: %v", err)
	}

	our := testGenT20Tool(sourceRoot, targetDir)
	ourNode := findGraphNodeByOutputs(t, our, "$(B)/contrib/tools/bison/bison", "$(B)/contrib/tools/bison/libiconv.so")
	refNode := loadT20RefNode(t, "$(BUILD_ROOT)/contrib/tools/bison/bison", "$(BUILD_ROOT)/contrib/tools/bison/libiconv.so")

	if len(ourNode.Cmds) < 3 || len(refNode.Cmds) < 3 {
		t.Fatalf("expected both nodes to have at least 3 cmds")
	}

	gotTail := cmdArgsFrom(t, ourNode.Cmds[2].CmdArgs, "-Wl,--start-group")
	wantTail := normalizeT20Strings(cmdArgsFrom(t, refNode.Cmds[2].CmdArgs, "-Wl,--start-group"))

	if !reflect.DeepEqual(gotTail, wantTail) {
		t.Fatalf("bison link tail mismatch:\n  got:  %#v\n  want: %#v", gotTail, wantTail)
	}

	libiconvIdx := slices.Index(gotTail, "contrib/libs/libiconv/dynamic/libiconv.so")
	bisonLibIdx := slices.Index(gotTail, "contrib/tools/bison/lib/libtools-bison-lib.a")
	if libiconvIdx < 0 || bisonLibIdx < 0 {
		t.Fatalf("expected both libiconv.so and libtools-bison-lib.a in bison link tail: %v", gotTail)
	}
	if libiconvIdx >= bisonLibIdx {
		t.Fatalf("expected libiconv.so before libtools-bison-lib.a in bison link tail: %v", gotTail)
	}

	rpathCount := 0
	for _, arg := range gotTail {
		if arg == "-Wl,-rpath,$ORIGIN" {
			rpathCount++
		}
	}
	if rpathCount != 2 {
		t.Fatalf("expected 2 rpath entries in bison link tail, got %d: %v", rpathCount, gotTail)
	}

	if !slices.Contains(gotTail, "-Wl,--allow-multiple-definition") {
		t.Fatalf("bison link tail missing -Wl,--allow-multiple-definition: %v", gotTail)
	}
}

func TestGen_BisonCompileCommands_DoNotLeakLocalSoGlobals(t *testing.T) {
	const targetDir = "contrib/tools/bison"

	if _, err := os.Stat(filepath.Join(sourceRoot, targetDir, "ya.make")); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference ya.make not present at %s/%s/ya.make", sourceRoot, targetDir)
		}

		t.Fatalf("stat ya.make: %v", err)
	}

	our := testGenT20Tool(sourceRoot, targetDir)
	ref := loadT20RefGraph(t)

	tests := []struct {
		name      string
		ourOutput string
		refOutput string
		banned    []string
	}{
		{
			name:      "program-side",
			ourOutput: "$(B)/contrib/tools/bison/_/src/print-xml.c.pic.o",
			refOutput: "$(BUILD_ROOT)/contrib/tools/bison/_/src/print-xml.c.pic.o",
			banned: []string{
				"-I$(S)/contrib/libs/zlib/include",
				"-I$(S)/contrib/libs/double-conversion",
				"-I$(S)/contrib/libs/libc_compat/include/readpassphrase",
			},
		},
		{
			name:      "library-side",
			ourOutput: "$(B)/contrib/tools/bison/lib/mbchar.c.pic.o",
			refOutput: "$(BUILD_ROOT)/contrib/tools/bison/lib/mbchar.c.pic.o",
			banned: []string{
				"-I$(S)/contrib/libs/zlib/include",
				"-I$(S)/contrib/libs/double-conversion",
				"-I$(S)/contrib/libs/libc_compat/include/readpassphrase",
				"-I$(S)/contrib/libs/cxxsupp/libcxx/include",
				"-I$(S)/contrib/libs/cxxsupp/libcxxrt/include",
				"-I$(S)/contrib/libs/clang20-rt/include",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ourNode := findGraphNodeByOutputs(t, our, tc.ourOutput)
			refNode := findT20RefNodeByOutputs(t, ref, tc.refOutput)
			if len(ourNode.Cmds) == 0 || len(refNode.Cmds) == 0 {
				t.Fatalf("expected both nodes to have at least 1 cmd")
			}

			gotArgs := append([]string(nil), ourNode.Cmds[0].CmdArgs...)
			wantArgs := normalizeT20Strings(refNode.Cmds[0].CmdArgs)
			if !reflect.DeepEqual(gotArgs, wantArgs) {
				t.Fatalf("%s compile cmd_args mismatch:\n  got:  %#v\n  want: %#v", tc.name, gotArgs, wantArgs)
			}

			assertCmdArgsAbsent(t, gotArgs, tc.banned...)
		})
	}
}

func TestGen_BisonArchiveMatchesReferenceAfterLocalSoIsolation(t *testing.T) {
	const targetDir = "contrib/tools/bison"

	if _, err := os.Stat(filepath.Join(sourceRoot, targetDir, "ya.make")); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference ya.make not present at %s/%s/ya.make", sourceRoot, targetDir)
		}

		t.Fatalf("stat ya.make: %v", err)
	}

	our := testGenT20Tool(sourceRoot, targetDir)
	ref := loadT20RefGraph(t)

	ourNode := findGraphNodeByOutputs(t, our, "$(B)/contrib/tools/bison/lib/libtools-bison-lib.a")
	refNode := findT20RefNodeByOutputs(t, ref, "$(BUILD_ROOT)/contrib/tools/bison/lib/libtools-bison-lib.a")
	if len(ourNode.Cmds) == 0 || len(refNode.Cmds) == 0 {
		t.Fatalf("expected both archive nodes to have at least 1 cmd")
	}

	gotArgs := append([]string(nil), ourNode.Cmds[0].CmdArgs...)
	wantArgs := normalizeT20Strings(refNode.Cmds[0].CmdArgs)
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("bison archive cmd_args mismatch:\n  got:  %#v\n  want: %#v", gotArgs, wantArgs)
	}

	gotDepOutputs := projectGraphDepOutputs(t, our, ourNode.Deps)
	wantDepOutputs := projectT20RefDepOutputs(t, ref, refNode.Deps)
	if !reflect.DeepEqual(gotDepOutputs, wantDepOutputs) {
		t.Fatalf("bison archive dep outputs mismatch:\n  got:  %#v\n  want: %#v", gotDepOutputs, wantDepOutputs)
	}
}

func testGenT20(sourceRoot, targetDir string) *Graph {
	host := newT20ResourcePlatform(OSLinux, ISAX8664, "yes", []string{"tool"}, false)
	target := newT20ResourcePlatform(OSLinux, ISAAArch64, "yes", nil, true)

	return GenWithMode(sourceRoot, targetDir, host, target, defaultScanCtxMode, func(Warn) {})
}

func testGenT20Tool(sourceRoot, targetDir string) *Graph {
	host := newT20ResourcePlatform(OSLinux, ISAX8664, "yes", []string{"tool"}, false)

	return GenWithMode(sourceRoot, targetDir, host, host, defaultScanCtxMode, func(Warn) {})
}

func newT20ResourcePlatform(os OS, isa ISA, pic string, tags []string, musl bool) *Platform {
	flags := map[string]string{
		"AR_TOOL":           "$(CLANG)/bin/llvm-ar",
		"BUILD_PYTHON_BIN":  "$(YMAKE_PYTHON3)/bin/python3",
		"BUILD_PYTHON3_BIN": "$(YMAKE_PYTHON3)/bin/python3",
		"CLANG_TOOL":        "$(CLANG)/bin/clang",
		"CLANG_pl_pl_TOOL":  "$(CLANG)/bin/clang++",
		"LLD_TOOL":          "$(LLD_ROOT)/bin/ld.lld",
		"OBJCOPY_TOOL":      "$(CLANG)/bin/llvm-objcopy",
		"PIC":               pic,
		"STRIP_TOOL":        "$(CLANG)/bin/llvm-strip",
	}
	if musl {
		flags["MUSL"] = "yes"
	}

	return NewPlatform(os, isa, flags, tags, "", "")
}

type t20RefGraph struct {
	nodes []*t20RefNode
	byUID map[string]*t20RefNode
}

func loadT20RefGraph(t *testing.T) *t20RefGraph {
	t.Helper()

	path := filepath.Join(sourceRoot, "sg3.json")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference graph not present at %s", path)
		}

		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	dec := json.NewDecoder(f)

	tok, err := dec.Token()
	if err != nil {
		t.Fatalf("read opening token from %s: %v", path, err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		t.Fatalf("unexpected opening token in %s: %v", path, tok)
	}

	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			t.Fatalf("read object key from %s: %v", path, err)
		}

		key, ok := keyTok.(string)
		if !ok {
			t.Fatalf("unexpected key token in %s: %v", path, keyTok)
		}

		if key != "graph" {
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				t.Fatalf("skip %q in %s: %v", key, path, err)
			}

			continue
		}

		tok, err = dec.Token()
		if err != nil {
			t.Fatalf("read graph opener from %s: %v", path, err)
		}
		if d, ok := tok.(json.Delim); !ok || d != '[' {
			t.Fatalf("unexpected graph opener in %s: %v", path, tok)
		}

		ref := &t20RefGraph{byUID: make(map[string]*t20RefNode)}
		for dec.More() {
			var node t20RefNode
			if err := dec.Decode(&node); err != nil {
				t.Fatalf("decode graph node from %s: %v", path, err)
			}

			nodeCopy := node
			ref.nodes = append(ref.nodes, &nodeCopy)
			if nodeCopy.UID != "" {
				ref.byUID[nodeCopy.UID] = &nodeCopy
			}
		}

		return ref
	}

	t.Fatalf("graph array not found in %s", path)

	return nil
}

func loadT20RefNode(t *testing.T, wantOutputs ...string) *t20RefNode {
	t.Helper()

	return findT20RefNodeByOutputs(t, loadT20RefGraph(t), wantOutputs...)
}

func findT20RefNodeByOutputs(t *testing.T, ref *t20RefGraph, wantOutputs ...string) *t20RefNode {
	t.Helper()

	for _, node := range ref.nodes {
		if slices.Equal(node.Outputs, wantOutputs) {
			return node
		}
	}

	t.Fatalf("reference node with outputs %v not found", wantOutputs)
	return nil
}

func findGraphNodeByOutputs(t *testing.T, g *Graph, wantOutputs ...string) *Node {
	t.Helper()

	for _, node := range g.Graph {
		if len(node.Outputs) != len(wantOutputs) {
			continue
		}

		match := true
		for i, out := range node.Outputs {
			if out.String() != wantOutputs[i] {
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

func cmdArgsFrom[T interface{ ~[]string }](t *testing.T, args T, marker string) []string {
	t.Helper()

	idx := slices.Index(args, marker)
	if idx < 0 {
		t.Fatalf("marker %q not found in cmd args: %v", marker, args)
	}

	return append([]string(nil), args[idx:]...)
}

func normalizeT20Token(s string) string {
	s = strings.NewReplacer(
		"$(BUILD_ROOT)", "$(B)",
		"$(SOURCE_ROOT)", "$(S)",
	).Replace(s)

	return t20ResourceMacroRE.ReplaceAllStringFunc(s, func(match string) string {
		dash := strings.IndexByte(match, '-')
		if dash < 0 {
			return match
		}

		return "$(" + match[2:dash] + ")"
	})
}

func normalizeT20Strings(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = normalizeT20Token(s)
	}

	return out
}

func normalizeT20Env(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}

	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = normalizeT20Token(v)
	}

	return out
}

func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)

	return out
}

func assertCmdArgsAbsent(t *testing.T, args []string, banned ...string) {
	t.Helper()

	for _, wantAbsent := range banned {
		if slices.Contains(args, wantAbsent) {
			t.Fatalf("cmd_args unexpectedly contain %q: %v", wantAbsent, args)
		}
	}
}

func projectGraphDepOutputs(t *testing.T, g *Graph, deps []string) [][]string {
	t.Helper()

	byUID := make(map[string]*Node, len(g.Graph))
	for _, node := range g.Graph {
		byUID[node.UID] = node
	}

	out := make([][]string, 0, len(deps))
	for _, uid := range deps {
		node := byUID[uid]
		if node == nil {
			t.Fatalf("dep uid %q not found in generated graph", uid)
		}

		out = append(out, append([]string(nil), vfsStrings(node.Outputs)...))
	}

	sort.Slice(out, func(i, j int) bool {
		return strings.Join(out[i], "\x00") < strings.Join(out[j], "\x00")
	})

	return out
}

func projectT20RefDepOutputs(t *testing.T, ref *t20RefGraph, deps []string) [][]string {
	t.Helper()

	out := make([][]string, 0, len(deps))
	for _, uid := range deps {
		node := ref.byUID[uid]
		if node == nil {
			t.Fatalf("dep uid %q not found in reference graph", uid)
		}

		out = append(out, normalizeT20Strings(node.Outputs))
	}

	sort.Slice(out, func(i, j int) bool {
		return strings.Join(out[i], "\x00") < strings.Join(out[j], "\x00")
	})

	return out
}
