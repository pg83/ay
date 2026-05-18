package main

import (
	"regexp"
	"sort"
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
// Returns (CF NodeRef, outputPath).
func EmitCF(
	instance ModuleInstance,
	srcRel string,
	in ModuleCCInputs,
	emit Emitter,
) (NodeRef, VFS) {
	srcVFS := Source(instance.Path + "/" + srcRel)
	outVFS := Build(instance.Path + "/" + strings.TrimSuffix(srcRel, ".in"))

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
	}

	cfgVars := buildCFGVars(in.FS, instance.Path+"/"+srcRel, in.DefaultVars, in.DefaultVarOrder)

	cmdArgs := []string{
		instance.Platform.Tools.Python3,
		configureFilePyPath,
		srcVFS.String(),
		outVFS.String(),
	}
	cmdArgs = append(cmdArgs, cfgVars...)

	inputs := make([]VFS, 0, 2+len(in.IncludeInputs))
	inputs = append(inputs, configureFilePyVFS, srcVFS)
	inputs = append(inputs, in.IncludeInputs...)

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
			"p":  "CF",
			"pc": "yellow",
		},
		Outputs: []VFS{outVFS},
		Tags:    []string{},
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		Platform:     string(instance.Platform.Target),
		HostPlatform: instance.Platform.IsHost,
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		DepRefs: []NodeRef{},
	}

	return emit.Emit(node), outVFS
}

// cfgVarRefRe matches @VAR_NAME@ substitution markers in .in template files.
var cfgVarRefRe = regexp.MustCompile(`@([A-Z_][A-Z0-9_]*)@`)

// buildCFGVars filters the module's DEFAULT declarations to vars actually
// @VAR@-referenced in the .in source, sorted alphabetically (ymake's order).
// @BUILD_TYPE@ without a DEFAULT falls back to DEBUG.
func buildCFGVars(fs *FS, rel string, defaultVars map[string]string, defaultVarOrder []string) []string {
	referenced := map[string]bool{}

	if data, err := fs.Read(rel); err == nil {
		for _, m := range cfgVarRefRe.FindAllSubmatch(data, -1) {
			referenced[string(m[1])] = true
		}
	}

	var vars []string
	declaredSet := map[string]bool{}

	for _, name := range defaultVarOrder {
		if !referenced[name] {
			continue
		}

		val, ok := defaultVars[name]
		if !ok {
			continue
		}

		vars = append(vars, name+"="+val)
		declaredSet[name] = true
	}

	if referenced["BUILD_TYPE"] && !declaredSet["BUILD_TYPE"] {
		vars = append(vars, buildTypeDebug)
	}

	sort.Strings(vars)

	return vars
}
