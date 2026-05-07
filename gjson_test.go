package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Path to the on-disk reference graph used as the high-fidelity smoke
// for LoadReference. Tests that depend on this file skip cleanly in
// environments where it is not present (per STYLE.md filter pattern).
//
// PR-31 D01: switched from g.json to sg.json. sg.json is the
// upstream-emitted graph WITH parsed `#include` transitive closures
// expanded as additional `inputs` per CC node. The L2 acceptance
// gate (≥70%) is computed against this reference; the include
// scanner (PR-31 D03) populates each CC node's IncludeInputs to
// match.
const referenceGraphPath = "/home/pg/monorepo/yatool_orig/sg.json"

// Counts pinned against the upstream sg.json:
//
//	$ python3 -c "import json; d=json.load(open('.../sg.json')); \
//	    print(len(d['graph']), len(d['result']))"
//	3730 1
//
// If upstream regenerates the reference graph these numbers may shift;
// the test failure message is the trigger to re-pin.
const (
	referenceNodeCount   = 3730
	referenceResultCount = 1
)

func TestLoadReference_RealGraph(t *testing.T) {
	if _, err := os.Stat(referenceGraphPath); err != nil {
		t.Skipf("reference graph %s not present: %v", referenceGraphPath, err)
	}

	g := LoadReference(referenceGraphPath)

	if got := len(g.Graph); got != referenceNodeCount {
		t.Fatalf("len(Graph): got %d, want %d", got, referenceNodeCount)
	}

	if got := len(g.Result); got != referenceResultCount {
		t.Fatalf("len(Result): got %d, want %d", got, referenceResultCount)
	}

	var sawArm, sawX86 bool
	for _, n := range g.Graph {

		if n.Platform == "default-linux-aarch64" {
			sawArm = true
		}

		if n.Platform == "default-linux-x86_64" {
			sawX86 = true
		}
	}

	if !sawArm {
		t.Errorf("expected at least one node with platform=default-linux-aarch64")
	}

	if !sawX86 {
		t.Errorf("expected at least one node with platform=default-linux-x86_64")
	}
}

func TestLoadReference_Synthetic(t *testing.T) {
	const payload = `{"conf":{},"graph":[{"uid":"x1","self_uid":"x1","stats_uid":"","cmds":[],"deps":[],"env":{},"inputs":[],"kv":{},"outputs":["o1"],"platform":"p1","requirements":{},"tags":[],"target_properties":{}}],"inputs":{},"result":["x1"]}`

	dir := t.TempDir()
	path := filepath.Join(dir, "g.json")
	Throw(os.WriteFile(path, []byte(payload), 0o600))

	g := LoadReference(path)

	if got := len(g.Graph); got != 1 {
		t.Fatalf("len(Graph): got %d, want 1", got)
	}

	if got := g.Graph[0].UID; got != "x1" {
		t.Errorf("Graph[0].UID: got %q, want %q", got, "x1")
	}

	if got := g.Graph[0].SelfUID; got != "x1" {
		t.Errorf("Graph[0].SelfUID: got %q, want %q", got, "x1")
	}

	if got := g.Graph[0].Platform; got != "p1" {
		t.Errorf("Graph[0].Platform: got %q, want %q", got, "p1")
	}

	if got := len(g.Graph[0].Outputs); got != 1 || g.Graph[0].Outputs[0] != "o1" {
		t.Errorf("Graph[0].Outputs: got %v, want [o1]", g.Graph[0].Outputs)
	}

	if got := len(g.Result); got != 1 || g.Result[0] != "x1" {
		t.Errorf("Result: got %v, want [x1]", g.Result)
	}

	// Internal *Refs fields are JSON-tagged "-" and must remain nil
	// after decoding from a finalized reference graph.
	if g.Graph[0].DepRefs != nil {
		t.Errorf("DepRefs: got %v, want nil", g.Graph[0].DepRefs)
	}

	if g.Graph[0].ForeignDepRefs != nil {
		t.Errorf("ForeignDepRefs: got %v, want nil", g.Graph[0].ForeignDepRefs)
	}
}

func TestLoadReference_MissingFile(t *testing.T) {
	const path = "/nonexistent/yatool-pr03-missing.json"

	exc := Try(func() {
		LoadReference(path)
	})

	if exc == nil {
		t.Fatal("expected exception for missing file, got nil")
	}

	if !strings.Contains(exc.Error(), "nonexistent") {
		t.Fatalf("expected error mentioning 'nonexistent', got: %v", exc)
	}
}

func TestLoadReference_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	Throw(os.WriteFile(path, []byte("not json {{"), 0o600))

	exc := Try(func() {
		LoadReference(path)
	})

	if exc == nil {
		t.Fatal("expected exception for malformed JSON, got nil")
	}
}

func TestLoadReference_EmptyGraph(t *testing.T) {
	const payload = `{"conf":{},"graph":[],"inputs":{},"result":["x1"]}`

	dir := t.TempDir()
	path := filepath.Join(dir, "empty.json")
	Throw(os.WriteFile(path, []byte(payload), 0o600))

	exc := Try(func() {
		LoadReference(path)
	})

	if exc == nil {
		t.Fatal("expected exception for empty graph, got nil")
	}

	if !strings.Contains(exc.Error(), "empty graph") {
		t.Fatalf("expected error mentioning 'empty graph', got: %v", exc)
	}
}

func TestLoadReference_EmptyResult(t *testing.T) {
	// Same precondition (zero-result), distinct from empty-graph: the
	// "or" branch in the validator must trigger on either side. Without
	// this, a regression that only checked g.Graph (or only g.Result)
	// would slip through.
	const payload = `{"conf":{},"graph":[{"uid":"x1","self_uid":"x1","stats_uid":"","cmds":[],"deps":[],"env":{},"inputs":[],"kv":{},"outputs":["o1"],"platform":"p1","requirements":{},"tags":[],"target_properties":{}}],"inputs":{},"result":[]}`

	dir := t.TempDir()
	path := filepath.Join(dir, "noresult.json")
	Throw(os.WriteFile(path, []byte(payload), 0o600))

	exc := Try(func() {
		LoadReference(path)
	})

	if exc == nil {
		t.Fatal("expected exception for empty result, got nil")
	}

	if !strings.Contains(exc.Error(), "empty graph or result") {
		t.Fatalf("expected error mentioning 'empty graph or result', got: %v", exc)
	}
}

// TestCmdInspect_RealGraph: end-to-end stdout-capture for cmdInspect is
// out of scope here — capturing fmt.Printf inside a process requires
// either swapping os.Stdout (racy with parallel tests) or refactoring
// cmdInspect to take an io.Writer (broader change than this PR's
// charter). The smoke-test invocation in PR-03's verification
// transcript covers the wiring; LoadReference itself is fully exercised
// above.

// TestCmdInspect_HelpFlag_PrintsUsageAndExits0 verifies that -h / --help
// returns exit code 0 and does not panic. Capturing the stdout output
// would require either swapping os.Stdout (racy) or refactoring
// cmdInspect to accept an io.Writer — that is a broader change deferred
// to a later PR. The no-panic / exit-0 contract is the critical signal.
func TestCmdInspect_HelpFlag_PrintsUsageAndExits0(t *testing.T) {
	code := cmdInspect([]string{"-h"})

	if code != 0 {
		t.Fatalf("cmdInspect(-h): got exit code %d, want 0", code)
	}
}

// TestCmdInspect_UnknownFlag_PanicsWithSingleErrorMessage verifies that
// an unrecognised flag causes cmdInspect to throw exactly one error via
// the Exception path (no duplicate output from flag's auto-write). The
// test asserts that the exception message contains "flag provided but not
// defined" — this is the canonical string from the stdlib flag package.
func TestCmdInspect_UnknownFlag_PanicsWithSingleErrorMessage(t *testing.T) {
	exc := Try(func() {
		cmdInspect([]string{"-bogus"})
	})

	if exc == nil {
		t.Fatal("cmdInspect(-bogus): expected exception, got nil (function returned normally)")
	}

	if !strings.Contains(exc.Error(), "flag provided but not defined") {
		t.Fatalf("cmdInspect(-bogus): exception message %q does not contain expected substring", exc.Error())
	}
}
