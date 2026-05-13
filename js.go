package main

// js.go — emitter for JS (JOIN_SRCS) nodes.
//
// EmitJS produces a single Node matching the shape ymake itself
// produces for a JOIN_SRCS macro invocation. The output is a single
// .cpp file (named allName, which already includes the .cpp suffix
// in all observed JOIN_SRCS calls) that #includes all listed
// sources.
//
// R13: sources are provided in DECLARATION ORDER — do NOT sort
// them. The reference graph confirms non-alphabetical orderings for
// several JS nodes; sorting would produce byte-mismatch at L3.
//
// PR-23 retrofitted the signature to take a `ModuleInstance`.

// EmitJS emits a JS node for JOIN_SRCS(allName srcs...).
//
// Output: $(B)/<instance.Path>/<allName>. allName already
// carries the .cpp suffix in all known JOIN_SRCS call sites.
//
// Sources are bare module-relative names (e.g. "recode_result.cpp" or
// "generated/unidata.cpp"). PR-28-D11: EmitJS composes them against
// instance.Path so cmd_args carries "<instance.Path>/<src>" and
// inputs carries "$(S)/<instance.Path>/<src>". When the
// caller threads a SRCDIR-rebased instance, the resulting paths
// reflect the rebased directory.
//
// Inputs: sources in DECLARATION ORDER (R13 — NOT sorted). The
// inputs list is: gen_join_srcs.py, process_command_files.py, then
// the sources expanded to their $(S)/... form, then the
// caller-supplied `closure` (PR-35d) — the union of per-source
// #include closures across the joined sources. L2 compares Inputs as
// a multiset (PR-31 D14), so closure order matters only for the
// byte-exact JS test pin in js_test.go.
//
// `platform` overrides the JS node's `Platform` field. PR-35s: in the
// reference graph, JS (JOIN_SRCS) nodes are anchored to the outer-
// target platform axis (`default-linux-aarch64`) even when the
// surrounding module is reached through a host-PROGRAM walk
// (`contrib/tools/ragel6/bin`). Threading platform explicitly keeps
// the JS Platform decoupled from the host-walked instance's Target
// (which flips to `default-linux-x86_64` via `WithHost`); the
// downstream JS-derived CC node continues to use `instance.Platform.Target` so
// only the JS axis is detached, not the per-source compile axis.
//
// Returns the JS NodeRef and the output path so the caller (PR-25's
// gen.go) can thread the output into a downstream EmitCC.
func EmitJS(instance ModuleInstance, allName string, sources []string, closure []VFS, platform PlatformID, emit Emitter) (NodeRef, string) {
	const (
		python3Path  = "/ix/realm/pg/bin/python3"
		joinSrcsPath = "$(S)/build/scripts/gen_join_srcs.py"
		procCmdFiles = "$(S)/build/scripts/process_command_files.py"
	)

	outputPath := "$(B)/" + instance.Path + "/" + allName

	// cmd_args: python3, script, output, --ya-start-command-file,
	// <instance.Path>/<src>..., --ya-end-command-file.
	cmdArgs := make([]string, 0, 4+len(sources))
	cmdArgs = append(cmdArgs,
		python3Path,
		joinSrcsPath,
		outputPath,
		"--ya-start-command-file",
	)

	for _, s := range sources {
		cmdArgs = append(cmdArgs, instance.Path+"/"+s)
	}

	cmdArgs = append(cmdArgs, "--ya-end-command-file")

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
	}

	// inputs: scripts first, then source files expanded to
	// $(S)/<instance.Path>/<src>, then the caller-supplied
	// per-source include closure (PR-35d).
	inputs := make([]VFS, 0, 2+len(sources)+len(closure))
	inputs = append(inputs, Source("build/scripts/gen_join_srcs.py"))
	inputs = append(inputs, Source("build/scripts/process_command_files.py"))

	for _, s := range sources {
		inputs = append(inputs, Source(instance.Path+"/"+s))
	}

	inputs = append(inputs, closure...)

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
			},
		},
		Env:    env,
		Inputs: inputs,
		KV: map[string]string{
			"p":  "JS",
			"pc": "magenta",
		},
		Outputs:  []VFS{Build(instance.Path + "/" + allName)},
		Platform: string(platform),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		Tags: []string{},
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
	}

	return emit.Emit(node), outputPath
}
