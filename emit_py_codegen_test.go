package main

import "testing"

func TestEmitPyRegister_ProducerEmittedAtTargetPlatform(t *testing.T) {
	emit := newBufferedEmitter()
	ctx := &GenCtx{
		emit:   emit,
		na:     emit.nodeArenas(),
		host:   testHostP,
		target: testTargetP,
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

	// No cross-platform cache: each instance emits its own .reg3.cpp producer.
	// gen_py3_reg.py is platform-independent codegen, attributed to the target
	// platform, so both producers carry testTargetP and no tool tag — byte-
	// identical, hence they collapse by uid in the finalized graph.
	wantOutput := "$(B)/contrib/tools/python3/Modules/_sqlite/_sqlite3.reg3.cpp"
	var pyNodes []*Node

	for _, n := range emit.nodes {
		if len(n.Outputs) == 1 && n.Outputs[0].string() == wantOutput {
			pyNodes = append(pyNodes, n)
		}
	}

	if len(pyNodes) != 2 {
		t.Fatalf("emitted %d PY producers, want 2 (one per instance)", len(pyNodes))
	}

	for _, n := range pyNodes {
		if string(n.Platform.Target) != string(testTargetP.Target) {
			t.Errorf("PY node platform = %q, want %q (target)", n.Platform.Target, testTargetP.Target)
		}

		if len(nodeTags(n)) != 0 {
			t.Errorf("PY node tags = %v, want none", nodeTags(n))
		}
	}
}
