package main

import "strings"

// build/internal/conf/sandbox.conf gives SANDBOX_PY3_TASK two independent
// contexts. SB_TASK_BIN inherits PY3_PROGRAM_BIN; PY3 inherits PY3_LIBRARY.
// Both have MODULE_TAG=PY3, but ONLY_TAGS selects their configuration names.
func sandboxPythonTaskVariant(kind ModuleKind) string {
	if kind == KindBin {
		return "SB_TASK_BIN"
	}

	return "PY3"
}

func sandboxPythonTaskUnit(kind ModuleKind, language Language) ModuleUnit {
	if kind == KindBin {
		return resolveModuleUnitBare(tokPy3ProgramBin, kind, language)
	}

	return resolveModuleUnitBare(tokPy3Library, kind, language)
}

func setupSandboxPythonTask(name TOK, modulePath string, kind ModuleKind, env Environment, d *ModuleData) {
	if name != tokSandboxPy3Task {
		return
	}

	d.multimodule = name
	env.setString(internEnv("MODULE_KIND"), sandboxPythonTaskVariant(kind))

	if kind == KindBin {
		d.pyMain = ptr(internStr("sandbox.taskbox.binary:main").any())
		d.peerdirs = append(d.peerdirs, internAnys([]string{"sandbox/bin", "sandbox/sdk2", "sandbox/taskbox/worker"})...)
		// The inherited variable starts at None. _BASE_PY3_PROGRAM clears it
		// for sanitizers before sandbox.conf appends its four masks.
		key := internEnv("NO_CHECK_IMPORTS_FOR_VALUE")
		env.setDefaultString(key, "None")
		prefix := env.string(key)
		if env.bool(envSANITIZER_TYPE) && env.string(envSANITIZER_TYPE) != "no" {
			prefix = ""
		}
		if prefix != "" {
			d.noCheckImports = append(d.noCheckImports, internStr(prefix).any())
		}
		d.noCheckImports = append(d.noCheckImports, anysOf("api.*", "kernel.*", "library.*", "sky*")...)
	} else {
		d.peerdirs = append(d.peerdirs, internStr("sandbox/sdk2").any())
	}

	// REGISTER_SANDBOX_IMPORT(${MODDIR}) in sandbox_registry.py registers the
	// module directory as a dotted Python import through the RESOURCE macro.
	ensureResourcePeer(modulePath, d)
	module := strings.ReplaceAll(modulePath, "/", ".")
	d.resources = append(d.resources, ResourceEntry{
		DeclSeq: d.nextDeclSeq(), Path: "-", EndsBatch: true,
		Key: "SANDBOX_TASK_REGISTRY." + module + "=" + module,
	})
}
