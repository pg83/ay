package main

import (
	"testing"
)

func TestResolveSourceVFS_RootRelativeSrc(t *testing.T) {
	fs := newMemFS(map[string]string{
		"geobase/library/asset.cpp":     "",
		"geobase/library/abi/local.cpp": "",
	})
	ctx := &GenCtx{fs: fs}
	moduleDir := "geobase/library/abi"
	instance := ModuleInstance{Path: source(moduleDir)}
	srcDirs := []VFS{dirKey(moduleDir).source()}

	got := resolveSourceVFS(ctx, instance, "geobase/library/asset.cpp", srcDirs)

	if want := source("geobase/library/asset.cpp"); got != want {
		t.Fatalf("root-relative src: got %s, want %s", got.relString(), want.relString())
	}

	got = resolveSourceVFS(ctx, instance, "local.cpp", srcDirs)

	if want := source("geobase/library/abi/local.cpp"); got != want {
		t.Fatalf("module-relative src: got %s, want %s", got.relString(), want.relString())
	}

	fs2 := newMemFS(map[string]string{
		"shared.cpp":                     "",
		"geobase/library/abi/shared.cpp": "",
	})
	ctx2 := &GenCtx{fs: fs2}
	got = resolveSourceVFS(ctx2, instance, "shared.cpp", srcDirs)

	if want := source("geobase/library/abi/shared.cpp"); got != want {
		t.Fatalf("ambiguous src: got %s, want %s", got.relString(), want.relString())
	}
}

func TestGen_RootRelativeSrc_CCInputsNotDoubled(t *testing.T) {
	fs := newMemFS(map[string]string{
		"geobase/library/abi/ya.make":   "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(local.cpp geobase/library/asset.cpp)\nEND()\n",
		"geobase/library/abi/local.cpp": "int local(){return 0;}\n",
		"geobase/library/asset.cpp":     "int asset(){return 1;}\n",
	})

	g := testGen(fs, "geobase/library/abi")

	const localInput = "$(S)/geobase/library/abi/local.cpp"
	const rootInput = "$(S)/geobase/library/asset.cpp"
	const doubledInput = "$(S)/geobase/library/abi/geobase/library/asset.cpp"

	var sawLocal, sawRoot bool

	for _, n := range g.Graph {
		if n == nil {
			continue
		}

		if n.KV.P != pkCC {
			continue
		}

		for _, in := range n.flatInputs() {
			switch in.string() {
			case localInput:
				sawLocal = true
			case rootInput:
				sawRoot = true
			case doubledInput:
				t.Fatalf("CC node lists the doubled root-relative input %q (output %v)", doubledInput, n.Outputs)
			}
		}
	}

	if !sawLocal {
		t.Errorf("no CC node lists the local source input %q", localInput)
	}

	if !sawRoot {
		t.Errorf("no CC node lists the root-relative source input %q (want arcadia-root binding, not doubled)", rootInput)
	}
}
