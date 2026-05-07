package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gen_test.go — end-to-end test for the M1 vertical slice. Parses
// build/cow/on/ya.make, emits the 2-node CC+AR subgraph, and asserts
// byte-exact L3 equality against the corresponding 2 nodes carved out
// of the upstream reference graph.
//
// Skip-if-missing pattern follows cc_test.go / ar_test.go: the reference
// snapshot under /home/pg/monorepo/yatool_orig is required; absence is a
// host condition, not a test failure.

var sourceRoot = filepath.Dir(referenceGraphPath)

func TestGen_BuildCowOn_TwoNodeSubgraph_L3MatchesReference(t *testing.T) {
	const targetDir = "build/cow/on"
	const arOutput = "$(BUILD_ROOT)/build/cow/on/libbuild-cow-on.a"

	if _, err := os.Stat(sourceRoot + "/" + targetDir + "/ya.make"); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference ya.make not present at %s/%s/ya.make", sourceRoot, targetDir)
		}

		t.Fatalf("stat ya.make: %v", err)
	}

	if _, err := os.Stat(referenceGraphPath); err != nil {
		t.Skipf("reference graph %s not present: %v", referenceGraphPath, err)
	}

	our := Gen(TargetCfg, sourceRoot, targetDir)

	if len(our.Graph) != 2 {
		t.Fatalf("Gen produced %d nodes for %s, want 2 (1 CC + 1 AR)", len(our.Graph), targetDir)
	}

	ref := LoadReference(referenceGraphPath)

	subgraphRef := &Graph{
		Conf:   ref.Conf,
		Inputs: ref.Inputs,
		Graph:  []*Node{},
	}

	var arUID string

	for _, n := range ref.Graph {
		if len(n.Outputs) == 0 {
			continue
		}

		if !strings.HasPrefix(n.Outputs[0], "$(BUILD_ROOT)/"+targetDir+"/") {
			continue
		}
		// PR-10 emits one platform (TargetCfg.Name). The reference graph
		// carries the same module on multiple platforms (4 nodes for
		// build/cow/on: 2 platforms × {CC, AR}); restrict the comparison
		// subgraph to TargetCfg.Name so the pairing is 2-vs-2 not 4-vs-2.
		if n.Platform != TargetCfg.Name {
			continue
		}

		subgraphRef.Graph = append(subgraphRef.Graph, n)

		if n.Outputs[0] == arOutput {
			arUID = n.UID
		}
	}

	if len(subgraphRef.Graph) != 2 {
		t.Fatalf("expected 2 nodes in reference subgraph for %s, got %d", targetDir, len(subgraphRef.Graph))
	}

	if arUID == "" {
		t.Fatalf("reference subgraph for %s has no AR node with output %q", targetDir, arOutput)
	}

	subgraphRef.Result = []string{arUID}

	r := Compare(subgraphRef, our, 3)

	if r.L0 != 1.0 {
		t.Errorf("L0 = %v, want 1.0 (note: %q)", r.L0, r.L0Note)
	}

	if r.L1 != 1.0 {
		t.Errorf("L1 = %v, want 1.0 (note: %q)", r.L1, r.L1Note)
	}

	if r.L2 != 1.0 {
		t.Errorf("L2 = %v, want 1.0 (note: %q)", r.L2, r.L2Note)
	}

	if r.L3 != 1.0 {
		t.Errorf("L3 = %v, want 1.0 (note: %q)", r.L3, r.L3Note)
	}

	t.Logf("L0=%.4f L1=%.4f L2=%.4f L3=%.4f", r.L0, r.L1, r.L2, r.L3)
	t.Logf("L3 note: %s", r.L3Note)
}

func TestCmdGen_HelpFlag_PrintsUsageAndExits0(t *testing.T) {
	rc := cmdGen([]string{"-h"})

	if rc != 0 {
		t.Errorf("rc = %d, want 0", rc)
	}
}

func TestCmdGen_UnknownFlag_PanicsWithSingleErrorMessage(t *testing.T) {
	exc := Try(func() {
		cmdGen([]string{"-bogus"})
	})

	if exc == nil {
		t.Fatal("expected exception")
	}

	if !strings.Contains(exc.Error(), "flag provided but not defined") {
		t.Errorf("unexpected error: %v", exc)
	}
}

func TestCmdGen_MissingTargetThrows(t *testing.T) {
	exc := Try(func() {
		cmdGen([]string{"--out", "/dev/null"})
	})

	if exc == nil {
		t.Fatal("expected exception")
	}

	if !strings.Contains(exc.Error(), "--target is required") {
		t.Errorf("unexpected error: %v", exc)
	}
}

func TestCmdGen_MissingOutThrows(t *testing.T) {
	exc := Try(func() {
		cmdGen([]string{"--target", "build/cow/on"})
	})

	if exc == nil {
		t.Fatal("expected exception")
	}

	if !strings.Contains(exc.Error(), "--out is required") {
		t.Errorf("unexpected error: %v", exc)
	}
}
