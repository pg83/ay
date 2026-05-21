package main

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

const t17SwigTargetDir = "contrib/tools/swig"

func TestReorderLDMembers_LegacyDoubleUnderscorePathsTrailRegularSources(t *testing.T) {
	refs := []NodeRef{{id: 1}, {id: 2}, {id: 3}}
	paths := []VFS{
		Build("contrib/tools/swig/_/Source/CParse/cscanner.c.pic.o"),
		Build("contrib/tools/swig/_/_/Source/CParse/parser.y.c.pic.o"),
		Build("contrib/tools/swig/_/Source/CParse/templ.c.pic.o"),
	}

	gotRefs, gotPaths := reorderLDMembers(refs, paths)

	wantRefs := []NodeRef{{id: 1}, {id: 3}, {id: 2}}
	if !reflect.DeepEqual(gotRefs, wantRefs) {
		t.Fatalf("ld refs mismatch:\n  got:  %#v\n  want: %#v", gotRefs, wantRefs)
	}

	wantPaths := []string{
		"$(B)/contrib/tools/swig/_/Source/CParse/cscanner.c.pic.o",
		"$(B)/contrib/tools/swig/_/Source/CParse/templ.c.pic.o",
		"$(B)/contrib/tools/swig/_/_/Source/CParse/parser.y.c.pic.o",
	}
	if got := vfsStrings(gotPaths); !reflect.DeepEqual(got, wantPaths) {
		t.Fatalf("ld paths mismatch:\n  got:  %#v\n  want: %#v", got, wantPaths)
	}
}

func TestGen_SwigToolBisonCompileNodesMatchReference(t *testing.T) {
	requireT17SwigFixture(t)

	our := testGenT20Tool(sourceRoot, t17SwigTargetDir)
	ref := loadT20RefGraph(t)

	tests := []struct {
		name      string
		ourOutput string
		refOutput string
	}{
		{
			name:      "cscanner",
			ourOutput: "$(B)/contrib/tools/swig/_/Source/CParse/cscanner.c.pic.o",
			refOutput: "$(BUILD_ROOT)/contrib/tools/swig/_/Source/CParse/cscanner.c.pic.o",
		},
		{
			name:      "parser-y-generated-cc",
			ourOutput: "$(B)/contrib/tools/swig/_/_/Source/CParse/parser.y.c.pic.o",
			refOutput: "$(BUILD_ROOT)/contrib/tools/swig/_/_/Source/CParse/parser.y.c.pic.o",
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
				t.Fatalf("%s cmd_args mismatch:\n  got:  %#v\n  want: %#v", tc.name, gotArgs, wantArgs)
			}

			gotInputs := sortedStrings(vfsStrings(ourNode.Inputs))
			wantInputs := sortedStrings(normalizeT20Strings(refNode.Inputs))
			if !reflect.DeepEqual(gotInputs, wantInputs) {
				t.Fatalf("%s inputs mismatch:\n  got:  %#v\n  want: %#v", tc.name, gotInputs, wantInputs)
			}

			gotDepOutputs := projectGraphDepOutputs(t, our, ourNode.Deps)
			wantDepOutputs := projectT20RefDepOutputs(t, ref, refNode.Deps)
			if !reflect.DeepEqual(gotDepOutputs, wantDepOutputs) {
				t.Fatalf("%s dep outputs mismatch:\n  got:  %#v\n  want: %#v", tc.name, gotDepOutputs, wantDepOutputs)
			}
		})
	}
}

func TestGen_SwigToolLDMatchesReference(t *testing.T) {
	requireT17SwigFixture(t)

	our := testGenT20Tool(sourceRoot, t17SwigTargetDir)
	ref := loadT20RefGraph(t)

	ourNode := findGraphNodeByOutputs(t, our, "$(B)/contrib/tools/swig/swig")
	refNode := findT20RefNodeByOutputs(t, ref, "$(BUILD_ROOT)/contrib/tools/swig/swig")
	if len(ourNode.Cmds) < 3 || len(refNode.Cmds) < 3 {
		t.Fatalf("expected both nodes to have at least 3 cmds")
	}

	gotArgs := append([]string(nil), ourNode.Cmds[2].CmdArgs...)
	wantArgs := normalizeT20Strings(refNode.Cmds[2].CmdArgs)
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("swig ld cmd_args mismatch:\n  got:  %#v\n  want: %#v", gotArgs, wantArgs)
	}

	gotInputs := sortedStrings(vfsStrings(ourNode.Inputs))
	wantInputs := sortedStrings(normalizeT20Strings(refNode.Inputs))
	if !reflect.DeepEqual(gotInputs, wantInputs) {
		t.Fatalf("swig ld inputs mismatch:\n  got:  %#v\n  want: %#v", gotInputs, wantInputs)
	}

	gotDepOutputs := projectGraphDepOutputs(t, our, ourNode.Deps)
	wantDepOutputs := projectT20RefDepOutputs(t, ref, refNode.Deps)
	if !reflect.DeepEqual(gotDepOutputs, wantDepOutputs) {
		t.Fatalf("swig ld dep outputs mismatch:\n  got:  %#v\n  want: %#v", gotDepOutputs, wantDepOutputs)
	}

	parserIdx := slices.Index(gotArgs, "$(B)/contrib/tools/swig/_/_/Source/CParse/parser.y.c.pic.o")
	swigLibIdx := slices.Index(gotArgs, "$(B)/contrib/tools/swig/swig_lib.cpp.pic.o")
	if parserIdx < 0 || swigLibIdx < 0 {
		t.Fatalf("expected parser.y.c.pic.o and swig_lib.cpp.pic.o in swig link cmd_args: %v", gotArgs)
	}
	if parserIdx <= swigLibIdx {
		t.Fatalf("expected parser.y.c.pic.o after swig_lib.cpp.pic.o in swig link cmd_args: %v", gotArgs)
	}
}

func requireT17SwigFixture(t *testing.T) {
	t.Helper()

	if _, err := os.Stat(filepath.Join(sourceRoot, t17SwigTargetDir, "ya.make")); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference ya.make not present at %s/%s/ya.make", sourceRoot, t17SwigTargetDir)
		}

		t.Fatalf("stat ya.make: %v", err)
	}
}
