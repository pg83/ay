package main

import "strings"

// EmitAS emits an AS node for assembling `srcRel` (relative to
// instance.Path) into an object file.
//
// `in` carries per-module compile knobs (own/peer ADDINCL, own CFLAGS,
// auto peer CFLAGS, transitive header closure); synthetic tests can
// pass ModuleCCInputs{} for "no per-module flags".
//
// `yasmLD` is the host yasm linker NodeRef (real for asmlib .pic.o, nil
// elsewhere); when non-nil, wired into BOTH ForeignDepRefs["tool"] and
// DepRefs (foreign-deps-only diverged on L0 fingerprint).
//
// cmd_args branches on two orthogonal flags:
//   - instance.Flags.PIC selects host (x86_64; --target=x86_64-linux-gnu,
//     no -march, hostCFlags/hostDefines/ndebugPicBlock×2 with
//     hostSseFeatures between) vs target (aarch64;
//     --target=aarch64-linux-gnu -march=armv8-a, commonCFlags/Defines/
//     noLibcUndebugBlock×2).
//   - instance.Flags.NoStdInc injects muslExtraDefines (incl. -D_musl_=1)
//     between defines and suppression blocks.
//
// Returns (NodeRef, outputPath).
func EmitAS(instance ModuleInstance, srcRel string, in ModuleCCInputs, yasmLD *NodeRef, hostP *Platform, emit Emitter) (NodeRef, VFS) {
	if instance.Platform.ISA == ISAX8664 && strings.HasSuffix(srcRel, ".asm") {
		return emitASYasm(instance, srcRel, in, yasmLD, emit)
	}

	outVFS, inVFS := composeASPaths(instance, srcRel, in)
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
		Env:          env,
		Inputs:       allInputs,
		Outputs:      []VFS{outVFS},
		HostPlatform: instance.Platform.IsHost,
		KV: map[string]string{
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

	return emit.Emit(node), outVFS
}
