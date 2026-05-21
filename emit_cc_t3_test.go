package main

import "testing"

func TestEmitCC_OutputPath_YqlUdfSuffix(t *testing.T) {
	e := NewBufferedEmitter()
	in := ModuleCCInputs{ObjectSuffixStem: stringPtr("udfs")}

	_, outPath := EmitCC(targetInstance("udfmod"), "lib.cpp", Source("udfmod/lib.cpp"), in, testHostP, e)

	want := "$(B)/udfmod/lib.cpp.udfs.o"
	if outPath.String() != want {
		t.Fatalf("outPath = %q, want %q", outPath, want)
	}
}

func TestEmitCC_OutputPath_YqlUdfSuffixPIC(t *testing.T) {
	e := NewBufferedEmitter()
	in := ModuleCCInputs{ObjectSuffixStem: stringPtr("udfs")}
	instance := ModuleInstance{
		Path:     "udfmod",
		Kind:     KindLib,
		Language: LangCPP,
		Platform: testHostP,
	}

	_, outPath := EmitCC(instance, "lib.cpp", Source("udfmod/lib.cpp"), in, testHostP, e)

	want := "$(B)/udfmod/lib.cpp.udfs.pic.o"
	if outPath.String() != want {
		t.Fatalf("outPath = %q, want %q", outPath, want)
	}
}

func TestEmitCC_NoWShadowAddsWarningFlag(t *testing.T) {
	e := NewBufferedEmitter()
	in := ModuleCCInputs{Flags: FlagSet{NoWShadow: true}}

	EmitCC(targetInstance("build/cow/on"), "lib.cpp", Source("build/cow/on/lib.cpp"), in, testHostP, e)

	if !contains(e.nodes[0].Cmds[0].CmdArgs, "-Wno-shadow") {
		t.Fatalf("cmd_args missing -Wno-shadow: %v", e.nodes[0].Cmds[0].CmdArgs)
	}
}
