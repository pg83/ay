package main

import "strings"

// EmitAR emits an AR node that archives the .o files (passed via objRefs)
// into $(BUILD_ROOT)/<moduleDir>/lib<dashed-modulePath>.a for the given
// module context.
//
// objRefs are the NodeRef values for the upstream CC nodes (used as
// DepRefs). objPaths are the corresponding .o file paths (used in inputs
// and as the trailing cmd_args elements). The caller is responsible for
// keeping the two slices in step.
//
// Returns the NodeRef for the emitted AR node.
//
// TODO(multi-source): for modules with more than one source file, all
// .o paths are appended after the archive path in cmd_args. The current
// implementation handles any number of .o paths correctly — just pass
// them all in objPaths and matching NodeRefs in objRefs.
func EmitAR(platform string, moduleDir string, objRefs []NodeRef, objPaths []string, emit Emitter) NodeRef {
	if len(objRefs) != len(objPaths) {
		ThrowFmt("EmitAR: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths))
	}

	// Archive-name convention used here ("lib" + ReplaceAll(moduleDir, "/", "-") + ".a")
	// is correct for the M1 leaf module `build/cow/on` only. Real ymake applies
	// `contrib/...` and `library/...` prefix-truncation rules and special cases
	// (e.g. `util` → `libyutil.a`) that are NOT implemented here.
	//
	// TODO(M2): replace with the real naming function once a multi-leaf rule emitter lands.
	archivePath := "$(BUILD_ROOT)/" + moduleDir + "/lib" + strings.ReplaceAll(moduleDir, "/", "-") + ".a"

	scriptPath := "$(SOURCE_ROOT)/build/scripts/link_lib.py"

	cmdEnv := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)",
		"DYLD_LIBRARY_PATH":      "$OS_SDK_ROOT_RESOURCE_GLOBAL/usr/lib/x86_64-linux-gnu",
	}

	// Build the cmd_args list: fixed prefix of 9 elements, then the
	// archive output path, then all .o input paths. For the M1 single-
	// source case this yields 11 elements total.
	cmdArgs := []string{
		// TODO(portability): python3 path is captured from the reference build;
		// future work must template this from TargetCfg or detect it from $PATH.
		"/ix/realm/pg/bin/python3",
		scriptPath,
		"ar",
		"GNU_AR",
		"None",
		"$(BUILD_ROOT)",
		"None",
		"--",
		"--",
		archivePath,
	}

	cmdArgs = append(cmdArgs, objPaths...)

	// inputs: .o paths first, then the script path. Order matches the
	// reference g.json for build/cow/on.
	inputs := make([]string, 0, len(objPaths)+1)
	inputs = append(inputs, objPaths...)
	inputs = append(inputs, scriptPath)

	topEnv := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)",
		"DYLD_LIBRARY_PATH":      "$OS_SDK_ROOT_RESOURCE_GLOBAL/usr/lib/x86_64-linux-gnu",
	}

	n := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     cmdEnv,
			},
		},
		Env:    topEnv,
		Inputs: inputs,
		KV: map[string]string{
			"p":        "AR",
			"pc":       "light-red",
			"show_out": "yes",
		},
		Outputs:  []string{archivePath},
		Platform: platform,
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		Tags: []string{},
		TargetProperties: map[string]string{
			"module_dir":  moduleDir,
			"module_lang": "cpp",
			"module_type": "lib",
		},
		DepRefs: objRefs,
	}

	return emit.Emit(n)
}
