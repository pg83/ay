package main

// r6.go — emitter for R6 (ragel6) generated-source nodes.
//
// PR-23 rewrites the PR-17 r6.go to take a real `ragel6LD NodeRef`
// (D31 — cross-platform recursion replaces stub-host UIDs). PR-28
// then re-routes the ragel6 LD edge from `ForeignDepRefs["tool"]` into
// `DepRefs` to match the empirical reference shape (F2 of the PR-28
// plan): the reference R6 node has `deps=[ragel6 host LD UID]` and
// `foreign_deps={tool:[<dangling internal placeholder>]}`. The
// dangling placeholder UID is unreachable in the reference graph
// itself, so we cannot reproduce it byte-exact and we omit
// `foreign_deps` entirely instead.
//
// Reference R6 node: `$(BUILD_ROOT)/util/_/datetime/parser.rl6.cpp`.
// 7 cmd_args, kv={p:R6, pc:yellow}, tags=[],
// requirements={cpu:1,network:restricted,ram:32},
// deps=[<ragel6 host LD UID>].

// EmitR6 emits an R6 node generating `<srcRel>.cpp` from
// `<instance.Path>/<srcRel>` using the host ragel6 binary referenced
// by `ragel6LD` and located at `ragel6BinaryPath`.
//
// `ragel6BinaryPath` is the absolute `$(BUILD_ROOT)/...` path of the
// ragel6 binary — i.e. the host LD's `outputs[0]`. Per PR-28-D01 the
// caller derives this from the host LD's emitted output rather than
// hardcoding it here, so the cmd_args invocation path always matches
// where our own walker emitted the host PROGRAM. When the host walk
// failed (parse gap; ragel6LD is the zero NodeRef), the caller may
// still pass a literal fallback string.
//
// Output path: `$(BUILD_ROOT)/<instance.Path>/_/<srcRel>.cpp`. Note
// the `_/` infix matches the AS convention (D29) — generated sources
// are nested-output regardless of srcRel depth.
//
// Returns (NodeRef, outputPath) so the caller can wire the R6 node as
// the input of a downstream EmitCC.
func EmitR6(instance ModuleInstance, srcRel string, ragel6LD NodeRef, ragel6BinaryPath string, emit Emitter) (NodeRef, string) {
	outputPath := "$(BUILD_ROOT)/" + instance.Path + "/_/" + srcRel + ".cpp"
	inputPath := "$(SOURCE_ROOT)/" + instance.Path + "/" + srcRel

	cmdArgs := []string{
		ragel6BinaryPath,
		"-CT0",
		"-L",
		"-I$(SOURCE_ROOT)",
		"-o",
		outputPath,
		inputPath,
	}

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)",
	}

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
			},
		},
		Env:     env,
		Inputs:  []string{inputPath},
		Outputs: []string{outputPath},
		KV: map[string]string{
			"p":  "R6",
			"pc": "yellow",
		},
		Tags: []string{},
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		Platform: string(instance.Target),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		DepRefs: []NodeRef{ragel6LD},
	}

	return emit.Emit(node), outputPath
}
