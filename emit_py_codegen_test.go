package main

import "testing"

func TestEmitPyRegister_TargetPlatformCacheWinsOverHostFirstVisit(t *testing.T) {
	emit := NewBufferedEmitter()
	ctx := &genCtx{
		emit:              emit,
		host:              testHostP,
		target:            testTargetP,
		pyRegisterOutputs: make(map[VFS]NodeRef),
	}
	d := &moduleData{pyRegister: []string{"_sqlite3"}}
	hostInst := ModuleInstance{
		Path:     "contrib/tools/python3/Modules/_sqlite",
		Kind:     KindLib,
		Language: LangCPP,
		Platform: testHostP,
	}
	targetInst := hostInst
	targetInst.Platform = testTargetP

	emitPyRegister(ctx, hostInst, d, ModuleCCInputs{}, false)
	emitPyRegister(ctx, targetInst, d, ModuleCCInputs{}, false)

	if len(emit.nodes) != 3 {
		t.Fatalf("emitter buffered %d nodes, want 3", len(emit.nodes))
	}

	wantOutput := "$(B)/contrib/tools/python3/Modules/_sqlite/_sqlite3.reg3.cpp"
	var pyNode *Node

	for _, n := range emit.nodes {
		if len(n.Outputs) == 1 && n.Outputs[0].String() == wantOutput {
			pyNode = n
			break
		}
	}

	if pyNode == nil {
		t.Fatalf("emitter buffered no PY node with output %q", wantOutput)
	}

	if platformTarget(pyNode.Platform) != string(testTargetP.Target) {
		t.Errorf("PY node platform = %q, want %q", platformTarget(pyNode.Platform), testTargetP.Target)
	}

	if len(pyNode.Tags) != 0 {
		t.Errorf("PY node tags = %v, want none", pyNode.Tags)
	}
}
