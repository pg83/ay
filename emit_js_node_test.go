package main

import (
	"strings"
	"testing"
)

func TestEmitJS_UsesRequestedPlatformTags(t *testing.T) {
	emit := newBufferedEmitter()
	target := newTestPlatform(OSLinux, ISAX8664, "no")

	ref, _ := emitJS(hostInstance("joinmod"), "all.cpp", []string{"a.cpp"}, nil, target, testToolchain(), nil, emit)
	got := emit.nodes[ref]

	if string(got.Platform.Target) != string(target.Target) {
		t.Fatalf("JS platform = %q, want %q", string(got.Platform.Target), target.Target)
	}
	if got.TargetProperties.ModuleDir != "joinmod" {
		t.Fatalf("JS module_dir = %q, want joinmod", got.TargetProperties.ModuleDir)
	}
}

func TestGen_JoinSrcs_EmitsJSAndCC(t *testing.T) {
	fs := newMemFS(map[string]string{
		"joinmod/ya.make": `LIBRARY()
JOIN_SRCS(all_my.cpp src1.cpp src2.cpp)
SRCS(other.cpp)
END()
`,
	})

	g := testGen(fs, "joinmod")

	counts := make(map[string]int)
	for _, n := range g.Graph {
		p := n.KV.P.string()
		counts[p]++
	}

	if counts["JS"] != 1 {
		t.Errorf("JS count = %d, want 1", counts["JS"])
	}

	if counts["CC"] != 2 {
		t.Errorf("CC count = %d, want 2 (1 for joined + 1 for other.cpp)", counts["CC"])
	}

	if counts["AR"] != 1 {
		t.Errorf("AR count = %d, want 1", counts["AR"])
	}

	if len(g.Graph) != 4 {
		t.Fatalf("total graph nodes = %d, want 4", len(g.Graph))
	}

	var (
		joinedInput string
		otherInput  string
	)

	for _, n := range g.Graph {
		if n.KV.P != pkCC {
			continue
		}

		if len(n.flatInputs()) == 0 {
			continue
		}

		switch {
		case strings.Contains(n.flatInputs()[0].string(), "all_my.cpp"):
			joinedInput = n.flatInputs()[0].string()
		case strings.Contains(n.flatInputs()[0].string(), "other.cpp"):
			otherInput = n.flatInputs()[0].string()
		}
	}

	if joinedInput == "" {
		t.Errorf("no CC node found whose input references all_my.cpp")
	}

	if otherInput == "" {
		t.Errorf("no CC node found whose input references other.cpp")
	}

	var jsOut string

	for _, n := range g.Graph {
		if n.KV.P == pkJS && len(n.Outputs) > 0 {
			jsOut = n.Outputs[0].string()
		}
	}

	wantJSOut := "$(B)/joinmod/all_my.cpp"
	if jsOut != wantJSOut {
		t.Errorf("JS output = %q, want %q", jsOut, wantJSOut)
	}
}

func TestGen_GeneratorWiredIntoDepRefs_JS(t *testing.T) {
	fs := newMemFS(map[string]string{
		"jsmod/ya.make": "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nJOIN_SRCS(all.cpp s1.cpp s2.cpp)\nEND()\n",
	})

	g := testGen(fs, "jsmod")

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

	found := false

	for _, dep := range graphDeps(g, ccNode) {
		if dep == jsNode.UID {
			found = true

			break
		}
	}

	if !found {
		t.Errorf("graphDeps(g, CC) = %v, want to contain JS UID %q (PR-30 D04 Generator wiring)", graphDeps(g, ccNode), jsNode.UID)
	}
}
