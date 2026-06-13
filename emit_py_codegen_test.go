package main

import "testing"

func TestEmitPyRegister_TargetPlatformCacheWinsOverHostFirstVisit(t *testing.T) {
	emit := newBufferedEmitter()
	ctx := &GenCtx{
		emit:              emit,
		na:                emit.nodeArenas(),
		host:              testHostP,
		target:            testTargetP,
		pyRegisterOutputs: make(map[VFS]NodeRef),
	}
	d := &ModuleData{pyRegister: STRS("_sqlite3")}
	hostInst := ModuleInstance{
		Path:     source("contrib/tools/python3/Modules/_sqlite"),
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
		if len(n.Outputs) == 1 && n.Outputs[0].string() == wantOutput {
			pyNode = n
			break
		}
	}

	if pyNode == nil {
		t.Fatalf("emitter buffered no PY node with output %q", wantOutput)
	}

	if string(pyNode.Platform.Target) != string(testTargetP.Target) {
		t.Errorf("PY node platform = %q, want %q", string(pyNode.Platform.Target), testTargetP.Target)
	}

	if len(nodeTags(pyNode)) != 0 {
		t.Errorf("PY node tags = %v, want none", nodeTags(pyNode))
	}
}
