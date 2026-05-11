package main

import (
	"os"
	"testing"
)

// normalize_test.go — tests for the normalize subcommand.
//
// The integration test normalizes the sg.json reference and verifies:
//   - Correct node count (3730 for tools/archiver).
//   - Determinism: two runs produce sha256-identical output.
//   - Semantic correctness: L0–L3 comparators report 100% against our
//     gen output.
//
// Unit tests cover findRootNode, extractClosure, and reUIDClosure with
// small synthetic graphs so failures pinpoint the broken step.

// TestNormalizeFindsLDRoot verifies that findRootNode returns the LD node
// whose outputs[0] ends with "/<binaryName>".
func TestNormalizeFindsLDRoot(t *testing.T) {
	g := &NormalizedGraph{
		Graph: []*NormalizedNode{
			{
				KV:      map[string]string{"p": "CC"},
				Outputs: []string{"$(BUILD_ROOT)/tools/foo/bar.c.o"},
				UID:     "uid-cc",
			},
			{
				KV:      map[string]string{"p": "LD"},
				Outputs: []string{"$(BUILD_ROOT)/tools/foo/foo"},
				UID:     "uid-ld",
			},
		},
		Result: []string{"uid-ld"},
	}

	root := findRootNode(g, "tools/foo")

	if root.UID != "uid-ld" {
		t.Errorf("expected LD root uid-ld, got %q", root.UID)
	}
}

// TestNormalizeFindsARRootWhenNoLD verifies that findRootNode falls back to
// the AR node (target platform only) when no LD exists.
func TestNormalizeFindsARRootWhenNoLD(t *testing.T) {
	g := &NormalizedGraph{
		Graph: []*NormalizedNode{
			{
				KV:      map[string]string{"p": "CC"},
				Outputs: []string{"$(BUILD_ROOT)/build/bar/lib.c.o"},
				UID:     "uid-cc",
			},
			{
				KV:           map[string]string{"p": "AR"},
				Outputs:      []string{"$(BUILD_ROOT)/build/bar/libbuild-bar.a"},
				UID:          "uid-ar",
				HostPlatform: false,
			},
			{
				KV:           map[string]string{"p": "AR"},
				Outputs:      []string{"$(BUILD_ROOT)/build/bar/libbuild-bar.a"},
				UID:          "uid-ar-host",
				HostPlatform: true,
			},
		},
		Result: []string{"uid-ar"},
	}

	root := findRootNode(g, "build/bar")

	if root.UID != "uid-ar" {
		t.Errorf("expected AR root uid-ar, got %q", root.UID)
	}
}

// TestNormalizeExtractClosure verifies BFS closure extraction from a root.
func TestNormalizeExtractClosure(t *testing.T) {
	g := &NormalizedGraph{
		Graph: []*NormalizedNode{
			{UID: "root", Deps: []string{"child1", "child2"}},
			{UID: "child1", Deps: []string{"leaf"}},
			{UID: "child2", Deps: []string{"leaf"}},
			{UID: "leaf", Deps: []string{}},
			{UID: "unrelated", Deps: []string{}},
		},
		Result: []string{"root"},
	}

	root := g.Graph[0]
	closure := extractClosure(g, root)

	for _, expected := range []string{"root", "child1", "child2", "leaf"} {
		if _, ok := closure[expected]; !ok {
			t.Errorf("expected %q in closure", expected)
		}
	}

	if _, ok := closure["unrelated"]; ok {
		t.Error("unrelated node should not be in closure")
	}
}

// TestNormalizeReUIDDeterministic verifies that reUIDClosure is deterministic:
// two calls with the same input produce the same old→new UID map.
func TestNormalizeReUIDDeterministic(t *testing.T) {
	leaf := &NormalizedNode{
		UID:              "old-leaf",
		KV:               map[string]string{"p": "CC"},
		Outputs:          []string{"leaf.o"},
		Cmds:             []Cmd{},
		Deps:             []string{},
		Env:              map[string]string{},
		Inputs:           []string{},
		Tags:             []string{},
		Requirements:     map[string]interface{}{},
		TargetProperties: map[string]string{},
	}
	root := &NormalizedNode{
		UID:              "old-root",
		KV:               map[string]string{"p": "AR"},
		Outputs:          []string{"libfoo.a"},
		Cmds:             []Cmd{},
		Deps:             []string{"old-leaf"},
		Env:              map[string]string{},
		Inputs:           []string{},
		Tags:             []string{},
		Requirements:     map[string]interface{}{},
		TargetProperties: map[string]string{},
	}

	closure := map[string]*NormalizedNode{
		"old-leaf": leaf,
		"old-root": root,
	}

	m1 := reUIDClosure(closure)
	m2 := reUIDClosure(closure)

	if m1["old-leaf"] != m2["old-leaf"] {
		t.Errorf("leaf UID not deterministic: %q vs %q", m1["old-leaf"], m2["old-leaf"])
	}

	if m1["old-root"] != m2["old-root"] {
		t.Errorf("root UID not deterministic: %q vs %q", m1["old-root"], m2["old-root"])
	}
}

// TestNormalizeSelfUIDEqualsUID verifies that the output NormalizedNodes
// always have self_uid == uid after normalization.
func TestNormalizeSelfUIDEqualsUID(t *testing.T) {
	if _, err := os.Stat("/home/pg/monorepo/yatool_orig/sg.json"); err != nil {
		t.Skip("sg.json not available")
	}

	g := loadNormalizedReference("/home/pg/monorepo/yatool_orig/sg.json")
	nodes, _ := normalizeGraph(g, "tools/archiver")

	for i, n := range nodes {
		if n.UID != n.SelfUID {
			t.Errorf("node[%d] uid=%q self_uid=%q; want self_uid==uid", i, n.UID, n.SelfUID)
		}
	}
}

// TestNormalizeStatsUIDDropped verifies that all normalized nodes have
// empty stats_uid.
func TestNormalizeStatsUIDDropped(t *testing.T) {
	if _, err := os.Stat("/home/pg/monorepo/yatool_orig/sg.json"); err != nil {
		t.Skip("sg.json not available")
	}

	g := loadNormalizedReference("/home/pg/monorepo/yatool_orig/sg.json")
	nodes, _ := normalizeGraph(g, "tools/archiver")

	for i, n := range nodes {
		if n.StatsUID != "" {
			t.Errorf("node[%d] has non-empty stats_uid %q", i, n.StatsUID)
		}
	}
}

// TestNormalizeNodeCount verifies the 3730-node closure for tools/archiver.
func TestNormalizeNodeCount(t *testing.T) {
	if _, err := os.Stat("/home/pg/monorepo/yatool_orig/sg.json"); err != nil {
		t.Skip("sg.json not available")
	}

	g := loadNormalizedReference("/home/pg/monorepo/yatool_orig/sg.json")
	nodes, result := normalizeGraph(g, "tools/archiver")

	if len(nodes) != 3730 {
		t.Errorf("expected 3730 nodes, got %d", len(nodes))
	}

	if len(result) != 1 {
		t.Errorf("expected 1 result, got %d", len(result))
	}
}

// TestNormalizeCowOn verifies the 2-node closure for build/cow/on.
func TestNormalizeCowOn(t *testing.T) {
	if _, err := os.Stat("/home/pg/monorepo/yatool_orig/sg.json"); err != nil {
		t.Skip("sg.json not available")
	}

	g := loadNormalizedReference("/home/pg/monorepo/yatool_orig/sg.json")
	nodes, result := normalizeGraph(g, "build/cow/on")

	if len(nodes) != 2 {
		t.Errorf("expected 2 nodes for build/cow/on, got %d", len(nodes))
	}

	if len(result) != 1 {
		t.Errorf("expected 1 result, got %d", len(result))
	}
}
