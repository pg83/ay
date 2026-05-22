package main

import "strings"

// EmitAS emits an AS node for assembling `srcRel` (relative to
// instance.Path) into an object file. Synthetic tests can pass
// ModuleCCInputs{} for "no per-module flags".
//
// `yasmLD` is the host yasm linker NodeRef (real for asmlib .pic.o, nil
// elsewhere); when non-nil, wired into BOTH ForeignDepRefs["tool"] and
// DepRefs — foreign-deps-only diverges on the L0 fingerprint.
//
// Returns (NodeRef, outputPath).
func EmitAS(instance ModuleInstance, srcRel string, srcVFS VFS, in ModuleCCInputs, yasmLD *NodeRef, hostP *Platform, emit Emitter) (NodeRef, VFS) {
	if instance.Platform.ISA == ISAX8664 && strings.HasSuffix(srcRel, ".asm") {
		return emitASYasm(instance, srcRel, srcVFS, in, yasmLD, emit)
	}

	outVFS, inVFS := composeASPaths(instance, srcRel, srcVFS, in)
	outputPath := outVFS.String()
	inputPath := inVFS.String()

	cmdArgs := composeASCmdArgs(instance, outputPath, inputPath, in)
	env := hostP.ToolEnv()

	allInputs := make([]VFS, 0, 1+len(in.IncludeInputs))
	allInputs = append(allInputs, inVFS)
	allInputs = append(allInputs, in.IncludeInputs...)

	tags := instance.Platform.Tags

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Cwd:     "$(B)",
				Env:     env,
			},
		},
		Env:     env,
		Inputs:  allInputs,
		Outputs: []VFS{outVFS},
		KV: map[string]interface{}{
			"p":  "AS",
			"pc": "light-green",
		},
		Tags: tags,
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		Platform: string(instance.Platform.Target),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
	}

	if yasmLD != nil {
		node.ForeignDepRefs = map[string][]NodeRef{
			"tool": {*yasmLD},
		}
		node.DepRefs = []NodeRef{*yasmLD}
	}

	return emit.Emit(bindNodePlatform(node, instance.Platform)), outVFS
}
