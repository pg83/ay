package main

import "strings"

func isSandboxPythonTask(name TOK) bool {
	return name == tokSandboxPy3Task || name == tokSandboxPy23Task
}

// Configuration names are distinct from the inherited Python unit classification.
func sandboxPythonTaskVariant(name TOK, kind ModuleKind, language Language) string {
	if kind == KindBin {
		return "SB_TASK_BIN"
	}

	if name == tokSandboxPy23Task && language == LangPy2 {
		return "PY2"
	}

	return "PY3"
}

// sandbox.conf orders the binary first, followed by its library contexts.
func sandboxPythonTaskInstances(instance ModuleInstance, name TOK) []ModuleInstance {
	instance.Kind, instance.Language = KindBin, LangCPP
	instances := []ModuleInstance{instance}
	instance.Kind = KindLib
	if name == tokSandboxPy23Task {
		instance.Language = LangPy2
		instances = append(instances, instance)
		instance.Language = LangCPP
	}
	return append(instances, instance)
}

func sandboxPythonTaskExcluded(ctx *GenCtx, instance ModuleInstance, tag string) bool {
	instance.Kind, instance.Language, instance.Demand = KindBin, LangCPP, demandNone
	first, _ := ctx.scannerFor(instance).codegen.sourceModule(instance)
	return first.ExcludedVariants[internStr(tag)]
}

func sandboxPythonTaskUnit(name TOK, kind ModuleKind, language Language) ModuleUnit {
	if name == tokSandboxPy23Task {
		if kind == KindBin {
			return resolveModuleUnitBare(tokPy2Program, kind, language)
		}
		unit := resolveModuleUnitBare(tokPy23Library, kind, language)
		if language != LangPy2 {
			unit.Type = tokPy3Library
		}
		return unit
	}

	if kind == KindBin {
		return resolveModuleUnitBare(tokPy3ProgramBin, kind, language)
	}

	return resolveModuleUnitBare(tokPy3Library, kind, language)
}

func setupSandboxPythonTask(name TOK, modulePath string, kind ModuleKind, language Language, env Environment, d *ModuleData) {
	if !isSandboxPythonTask(name) {
		return
	}

	d.multimodule = name
	// confreader inserts the submodule's MODULE_TAG after inherited macro calls.
	// InitModule derives MODULE_KIND from the outer name in the mangled identity.
	d.unit.HashTag = internStr(sandboxPythonTaskVariant(name, kind, language))
	env.setStringID(internEnv("MODULE_KIND"), name.str())
	python2 := d.unit.Tag == unitTagPy2
	env.setBool(envPYTHON2, python2)
	env.setBool(envPYTHON3, !python2)
	version := "py3"
	if python2 {
		version = "py2"
	}
	env.setString(internEnv("_PYTHON_VER"), version)
	env.setString(internEnv("PYTHON_TYPE_FOR_CYTHON"), strings.ToUpper(version))

	if kind == KindBin {
		d.pyMain = ptr(internStr("sandbox.taskbox.binary:main").any())
		d.peerdirs = append(d.peerdirs, internAnys([]string{"sandbox/bin", "sandbox/sdk2", "sandbox/taskbox/worker"})...)
		// The inherited variable starts at None. Both Python program bases clear it
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
		env.setString(key, strings.Join(anyStrs(d.noCheckImports), " "))
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
