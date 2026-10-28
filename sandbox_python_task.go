package main

import (
	"path"
	"slices"
	"strings"
)

const sandboxPythonBinaryVariant = uint16(1) << LangNone

func isSandboxPythonTask(name TOK) bool {
	return name == tokSandboxPy3Task || hasSandboxPython2Library(name)
}

func isSandboxPython2Task(name TOK) bool {
	return name == tokSandboxTask || name == tokSandboxPy23Task
}

func hasSandboxPython2Library(name TOK) bool {
	return isSandboxPython2Task(name) || name == tokSandboxPy233Task
}

func sandboxPythonTaskVariant(name TOK, kind ModuleKind, language Language) string {
	if kind == KindBin {
		return "SB_TASK_BIN"
	}

	if name == tokSandboxTask || hasSandboxPython2Library(name) && language == LangPy2 {
		return "PY2"
	}

	return "PY3"
}

func sandboxPythonTaskFirstInstance(instance ModuleInstance, name TOK) ModuleInstance {
	instance.Kind, instance.Language = KindLib, LangCPP

	if hasSandboxPython2Library(name) {
		instance.Language = LangPy2
	}

	return instance
}

func sandboxPythonTaskInstances(instance ModuleInstance, name TOK) []ModuleInstance {
	instance = sandboxPythonTaskFirstInstance(instance, name)

	instances := []ModuleInstance{instance}

	if hasSandboxPython2Library(name) && name != tokSandboxTask {
		instance.Language = LangCPP
		instances = append(instances, instance)
	}

	instance.Kind, instance.Language = KindBin, LangCPP
	instances = append(instances, instance)

	return instances
}

func sandboxPythonTaskVariantBit(instance ModuleInstance) uint16 {
	if instance.Kind == KindBin {
		return sandboxPythonBinaryVariant
	}

	if instance.Language == LangPy2 {
		return uint16(1) << LangPy2
	}

	return uint16(1) << LangCPP
}

func selectedSandboxPythonTaskVariants(instance ModuleInstance, name TOK, d *ModuleData) uint16 {
	excluded := strings.Fields(instance.Platform.ifEnv.string(envExcludeSubmodules))

	var variants uint16

	for _, sub := range sandboxPythonTaskInstances(instance, name) {
		tag := sandboxPythonTaskVariant(name, sub.Kind, sub.Language)

		if !moduleExcludesTag(d, tag) && !slices.Contains(excluded, tag) {
			variants |= sandboxPythonTaskVariantBit(sub)
		}
	}

	return variants
}

func (ctx *GenCtx) sandboxPythonTaskVariants(instance ModuleInstance, name TOK) uint16 {
	first := sandboxPythonTaskFirstInstance(instance, name)

	return genSiblingModule(ctx, first).ModuleVariants
}

func sandboxPythonTaskUnit(name TOK, kind ModuleKind, language Language) ModuleUnit {
	if name == tokSandboxTask {
		if kind == KindBin {
			return resolveModuleUnitBare(tokPy2Program, kind, language)
		}

		return resolveModuleUnitBare(tokPy2Library, kind, language)
	}

	if name == tokSandboxPy23Task || name == tokSandboxPy233Task {
		if kind == KindBin {
			program := tokPy2Program

			if !isSandboxPython2Task(name) {
				program = tokPy3ProgramBin
			}

			return resolveModuleUnitBare(program, kind, language)
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
	d.unit.HashTag = internStr(sandboxPythonTaskVariant(name, kind, language))

	if name == tokSandboxPy233Task && kind == KindLib && language != LangPy2 {
		prefix := "libpy3"

		if env.string(envMsvc) == "yes" || env.string(envCygwin) == "yes" {
			prefix = "py3"
		}

		d.unit.ARPrefix = prefix
		env.setString(envMODULE_PREFIX, prefix)
	}

	python2 := d.unit.Tag == unitTagPy2

	env.setBool(envPYTHON2, python2)
	env.setBool(envPYTHON3, !python2)

	version := "py3"

	if python2 {
		version = "py2"
	}

	env.setString(envPythonVer, version)
	env.setString(envPythonTypeForCython, strings.ToUpper(version))

	if kind == KindBin {
		d.pyMain = ptr(strSandboxTaskboxBinaryMain.any())
		d.peerdirs = append(d.peerdirs, internAnys([]string{"sandbox/bin", "sandbox/sdk2", "sandbox/taskbox/worker"})...)

		for _, mask := range []string{"api.*", "kernel.*", "library.*", "sky*"} {
			value := appendPythonImportMask(mask, env, d)

			env.setString(internEnv(importMaskVariable), value)
		}
	} else {
		d.peerdirs = append(d.peerdirs, strSandboxSdk2.any())
	}

	registerSandboxImport(modulePath, modulePath, d)
}

func registerSandboxImport(modulePath, importPath string, d *ModuleData) {
	ensureResourcePeer(modulePath, d)

	parts := strings.Split(path.Clean(importPath), "/")
	left, right := 0, len(parts)

	if parts[right-1] == "__init__.py" {
		right--
	}

	if parts[0] == "" {
		left++
	}

	module := strings.Join(parts[left:right], ".")

	d.resources = append(d.resources, ResourceEntry{
		DeclSeq: d.nextDeclSeq(), Path: "-", EndsBatch: true,
		Key: "SANDBOX_TASK_REGISTRY." + module + "=" + module,
	})
}
