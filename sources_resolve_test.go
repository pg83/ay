package main

import "testing"

// Arcadia ya.make files routinely list SRCS as arcadia-ROOT-relative paths that
// do not live under the declaring module dir — e.g. geobase/library/abi lists
// `geobase/library/asset.cpp` (the file is at $(S)/geobase/library/asset.cpp,
// not $(S)/geobase/library/abi/geobase/library/asset.cpp), and
// market/idx/datacamp/proto/external lists
// `market/idx/datacamp/proto/api/ExportMessage.proto`. Upstream ymake's source
// resolution plan ends at the arcadia root, so such a SRCS entry binds to
// $(S)/<path>, not to the doubled $(S)/<moduledir>/<path>. These tests pin that
// behavior; before the fix resolution doubled the path against the module dir.

func TestResolveSourceVFS_RootRelativeSrc(t *testing.T) {
	fs := newMemFS(map[string]string{
		"geobase/library/asset.cpp":     "",
		"geobase/library/abi/local.cpp": "",
	})
	ctx := &GenCtx{fs: fs}
	moduleDir := "geobase/library/abi"
	instance := ModuleInstance{Path: source(moduleDir)}
	srcDirs := []VFS{dirKey(moduleDir)}

	// Root-relative SRCS not under the module dir resolves at the arcadia root.
	got := resolveSourceVFS(ctx, instance, "geobase/library/asset.cpp", srcDirs)
	if want := source("geobase/library/asset.cpp"); got != want {
		t.Fatalf("root-relative src: got %s, want %s", got.rel(), want.rel())
	}

	// A genuinely module-relative src still resolves under the module dir.
	got = resolveSourceVFS(ctx, instance, "local.cpp", srcDirs)
	if want := source("geobase/library/abi/local.cpp"); got != want {
		t.Fatalf("module-relative src: got %s, want %s", got.rel(), want.rel())
	}

	// A name that exists under BOTH the module dir and the root prefers the
	// module dir (curdir wins, matching upstream resolution order).
	fs2 := newMemFS(map[string]string{
		"shared.cpp":                     "",
		"geobase/library/abi/shared.cpp": "",
	})
	ctx2 := &GenCtx{fs: fs2}
	got = resolveSourceVFS(ctx2, instance, "shared.cpp", srcDirs)
	if want := source("geobase/library/abi/shared.cpp"); got != want {
		t.Fatalf("ambiguous src: got %s, want %s", got.rel(), want.rel())
	}
}

func TestResolvePySrcRel_RootRelativeProto(t *testing.T) {
	fs := newMemFS(map[string]string{
		"market/idx/datacamp/proto/api/ExportMessage.proto":       "",
		"market/idx/datacamp/proto/external/ExportCategory.proto": "",
	})
	moduleDir := "market/idx/datacamp/proto/external"
	srcDirs := []VFS{dirKey(moduleDir)}

	// Root-relative proto SRCS resolves at the arcadia root.
	got := resolvePySrcRel(fs, srcDirs, moduleDir, "market/idx/datacamp/proto/api/ExportMessage.proto")
	if want := "market/idx/datacamp/proto/api/ExportMessage.proto"; got != want {
		t.Fatalf("root-relative proto: got %s, want %s", got, want)
	}

	// A proto that genuinely lives under the module dir resolves there.
	got = resolvePySrcRel(fs, srcDirs, moduleDir, "market/idx/datacamp/proto/external/ExportCategory.proto")
	if want := "market/idx/datacamp/proto/external/ExportCategory.proto"; got != want {
		t.Fatalf("root-relative proto under module: got %s, want %s", got, want)
	}
}

// A dirty (non-clean) srcRel must NOT be source-root bound. FS.isFile
// normalises `..`/`.` segments, so resolvePySrcRel(modulePath="pkg/sub",
// srcRel="../root.proto") would otherwise probe and match $(S)/root.proto and
// return the out-of-tree `../root.proto`. Upstream ymake reconstructs
// $S/../root.proto as out-of-tree and does not source-root bind it, so the
// arcadia-root fallback applies only to clean paths — matching the
// resolveSourceVFS guard. The dirty entry falls through to the module-relative
// join.
func TestResolvePySrcRel_DirtyPathNotRootBound(t *testing.T) {
	fs := newMemFS(map[string]string{
		"root.proto": "",
	})
	moduleDir := "pkg/sub"
	srcDirs := []VFS{dirKey(moduleDir)}

	got := resolvePySrcRel(fs, srcDirs, moduleDir, "../root.proto")
	if want := "pkg/sub/../root.proto"; got != want {
		t.Fatalf("dirty srcRel must not source-root bind: got %s, want %s", got, want)
	}
}

// Graph-level regression: a nested module (geobase/library/abi) lists a local
// source and an arcadia-root-relative source. The emitted CC nodes must carry
// the correct $(S) input for each — the local source module-relative, the
// root-relative source bound at the arcadia root, never the doubled
// $(S)/<moduledir>/<path>. Before the fix the root-relative source's CC node
// listed the doubled, nonexistent input.
func TestGen_RootRelativeSrc_CCInputsNotDoubled(t *testing.T) {
	fs := newMemFS(map[string]string{
		"geobase/library/abi/ya.make": "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(local.cpp geobase/library/asset.cpp)\nEND()\n",
		"geobase/library/abi/local.cpp": "int local(){return 0;}\n",
		"geobase/library/asset.cpp":     "int asset(){return 1;}\n",
	})

	g := testGen(fs, "geobase/library/abi")

	const localInput = "$(S)/geobase/library/abi/local.cpp"
	const rootInput = "$(S)/geobase/library/asset.cpp"
	const doubledInput = "$(S)/geobase/library/abi/geobase/library/asset.cpp"

	var sawLocal, sawRoot bool

	for _, n := range g.Graph {
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
