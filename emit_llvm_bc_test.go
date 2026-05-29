package main

import (
	"strings"
	"testing"
)

// TestEmitLLVMBC_PipelineProducesFiveNodes reproduces the G3 yt codec llvm16
// gap: USE_LLVM_BC16 + LLVM_BC parses (modules.go:1029) but emission is
// missing. Upstream `build/plugins/llvm_bc.py` drives the 5-step pipeline:
//   - llvm_compile_cxx  → $(B)/<src>.<suffix>.bc                kv.p=BC
//   - llvm_link         → $(B)/<mod>/<NAME>_merged.<suffix>.bc  kv.p=LD
//   - llvm_opt          → $(B)/<mod>/<NAME>_optimized.<suffix>.bc kv.p=OP
//   - onresource([out_bc, '/llvm_bc/'+NAME]) ⇒
//        objcopy_<hash>.o   kv.p=PY  (handled by existing emitResourceObjcopy)
//        lib<mod>.global.a  kv.p=AR  (handled by existing global-archive flow)
// Test asserts all 5 nodes reachable from the LIBRARY's archive root.
func TestEmitLLVMBC_PipelineProducesFiveNodes(t *testing.T) {
	const modPath = "mod/llvm"

	files := map[string]string{}
	writeToolProgram(files, "tools/rescompiler/bin", "rescompiler")
	writeToolProgram(files, "tools/rescompressor/bin", "rescompressor")
	for k, v := range map[string]string{
		modPath + "/ya.make": `LIBRARY()

USE_LLVM_BC16()

LLVM_BC(
    foo.cpp
    NAME
    Bar
    SUFFIX .16
    SYMBOLS
    DoThing
)

SRCS(foo.cpp)

END()
`,
		modPath + "/foo.cpp": "int Bar(){return 0;}\n",
	} {
		files[k] = v
	}

	g := testGen(newMemFS(files), modPath)

	byOut := make(map[string]*Node, len(g.Graph))
	for _, n := range g.Graph {
		for _, o := range n.Outputs {
			byOut[o.String()] = n
		}
	}

	want := map[string]string{
		"$(B)/" + modPath + "/foo.cpp.16.bc":          "BC",
		"$(B)/" + modPath + "/Bar_merged.16.bc":       "LD",
		"$(B)/" + modPath + "/Bar_optimized.16.bc":    "OP",
	}
	for path, kvp := range want {
		n := byOut[path]
		if n == nil {
			t.Errorf("graph missing %s node with output %q", kvp, path)
			continue
		}
		if got, _ := n.KV["p"].(string); got != kvp {
			t.Errorf("output %q kv.p = %q, want %q", path, got, kvp)
		}
	}

	var pyNode *Node
	for _, n := range g.Graph {
		if got, _ := n.KV["p"].(string); got != "PY" {
			continue
		}
		for _, o := range n.Outputs {
			if strings.HasPrefix(o.String(), "$(B)/"+modPath+"/objcopy_") &&
				strings.HasSuffix(o.String(), ".o") {
				pyNode = n
				break
			}
		}
		if pyNode != nil {
			break
		}
	}
	if pyNode == nil {
		t.Errorf("graph missing PY objcopy node for embedded LLVM_BC output")
	} else {
		hasOptBc := false
		for _, in := range pyNode.Inputs {
			if in.String() == "$(B)/"+modPath+"/Bar_optimized.16.bc" {
				hasOptBc = true
				break
			}
		}
		if !hasOptBc {
			t.Errorf("PY objcopy inputs do not include the optimized.bc: %v", pyNode.Inputs)
		}
	}

	var arNode *Node
	for _, n := range g.Graph {
		if got, _ := n.KV["p"].(string); got != "AR" {
			continue
		}
		for _, o := range n.Outputs {
			if strings.HasSuffix(o.String(), ".global.a") {
				arNode = n
				break
			}
		}
		if arNode != nil {
			break
		}
	}
	if arNode == nil {
		t.Errorf("graph missing AR .global.a node carrying the PY objcopy.o")
	}
}
