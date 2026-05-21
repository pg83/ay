package main

import (
	"strings"
)

// configureFilePy is the source-relative path to the configure_file.py
// script used in all CF nodes.
var configureFilePyVFS = Source("build/scripts/configure_file.py")
var configureFilePyPath = configureFilePyVFS.String()

// buildTypeDebug is injected when @BUILD_TYPE@ is referenced but not
// DEFAULT-declared. Hardcoded to DEBUG.
const buildTypeDebug = "BUILD_TYPE=DEBUG"

// EmitCF emits a CF node expanding a .cpp.in / .c.in template via
// configure_file.py. Output strips the .in suffix.
//
// cmd_args: [python3, configure_file.py, $(S)/<modulePath>/<srcRel>,
// $(B)/<modulePath>/<srcRel without .in>, <cfgVars...>].
// cfgVars derive from DEFAULT(name value) declarations filtered to
// vars actually @VAR@-referenced in the .in; BUILD_TYPE=DEBUG is
// injected when referenced but not DEFAULT-declared.
//
// Returns (CF NodeRef, outputPath). cfgVars is the pre-filtered
// `NAME=VALUE` cmd-arg fragment list (walker computes via
// buildCFGVars); includeInputs is the source-closure already walked
// from the .in template. moduleDir is the target_properties module_dir:
// the declaring module for compiled-in-place templates (.cpp.in/.c.in,
// explicit CONFIGURE_FILE) but the consuming module for a generated
// header realized by a peer (.h.in) — ymake attributes a generated
// header to the module that #includes it.
func EmitCF(
	instance ModuleInstance,
	srcRel string,
	cfgVars []string,
	includeInputs []VFS,
	moduleDir string,
	emit Emitter,
) (NodeRef, VFS) {
	srcVFS := Source(instance.Path + "/" + srcRel)
	outVFS := Build(instance.Path + "/" + strings.TrimSuffix(srcRel, ".in"))

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
	}

	cmdArgs := []string{
		instance.Platform.Tools.Python3,
		configureFilePyPath,
		srcVFS.String(),
		outVFS.String(),
	}
	cmdArgs = append(cmdArgs, cfgVars...)

	inputs := make([]VFS, 0, 2+len(includeInputs))
	inputs = append(inputs, configureFilePyVFS, srcVFS)
	inputs = append(inputs, includeInputs...)

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
			},
		},
		Env:    env,
		Inputs: inputs,
		KV: map[string]interface{}{
			"p":  "CF",
			"pc": "yellow",
		},
		Outputs: []VFS{outVFS},
		Tags:    []string{},
		TargetProperties: map[string]string{
			"module_dir": moduleDir,
		},
		Platform: string(instance.Platform.Target),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		DepRefs: []NodeRef{},
	}

	return emit.Emit(node), outVFS
}
