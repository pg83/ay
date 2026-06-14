package main

import "testing"

// TestEmitCF_GeneratedFromRidesAsClosureLeaf pins the CONFIGURE_FILE emitter's
// generated-from propagation: a cross-module consumer that #includes a configured
// header must carry, in its CC input closure, the generated header, the template
// source (.h.in) and configure_file.py — both riding as registry ClosureLeaves,
// not as fake #includes — plus the template's own #include (registered as the
// generated header's parsed includes).
func TestEmitCF_GeneratedFromRidesAsClosureLeaf(t *testing.T) {
	fs := newMemFS(map[string]string{
		"prod/ya.make":     "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(config.h.in)\nEND()\n",
		"prod/config.h.in": "#include \"marker.h\"\nint x = @V@;\n",
		"prod/marker.h":    "// marker\n",
		"app/ya.make":      "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nPEERDIR(prod)\nSRCS(use.cpp)\nEND()\n",
		"app/use.cpp":      "#include <prod/config.h>\nint use(){return 0;}\n",
	})

	g := testGen(fs, "app")
	cc := mustNodeByOutput(t, g, "$(B)/app/use.cpp.o")

	inputs := map[string]bool{}

	for _, in := range cc.flatInputs() {
		inputs[in.string()] = true
	}

	for _, want := range []string{
		"$(B)/prod/config.h",                   // the generated header itself
		"$(S)/prod/config.h.in",                // template source — generated-from ClosureLeaf
		"$(S)/build/scripts/configure_file.py", // generator script — generated-from ClosureLeaf
		"$(S)/prod/marker.h",                   // the template's own #include, registered on config.h
	} {
		if !inputs[want] {
			t.Errorf("use.cpp.o input closure missing %q", want)
		}
	}
}
