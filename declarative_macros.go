package main

import (
	"slices"
	"strings"
)

func declarativeMacroAssignment(v *UnknownStmt, packageModule bool, env Environment) *SetStmt {
	var name, value, parameter string

	switch v.Name {
	case tokValidateDataRestart, tokPack, tokSetCudaNodeRAMRequirement:
		if len(v.Args) != 1 {
			throwFmt("%s expects one argument", v.Name.string())
		}

		value = v.Args[0].string()
		parameter = "Ext"

		switch v.Name {
		case tokValidateDataRestart:
			name = "SBR_UID_EXT"
		case tokPack:
			if !packageModule {
				throwFmt("PACK is only allowed in PACKAGE modules")
			}

			name = "PACKED_PACKAGE_EXT"
		case tokSetCudaNodeRAMRequirement:
			name, parameter = "_CU_NODE_MAX_RAM_REQUIREMENT", "X"
		}
	case tokNoCow:
		name, value = "USE_COW", "no"
	case tokNoNeedCheck:
		name, value = "UNUSED_MACRO", "yes"
	case tokWindowsManifest, tokWindowsLongPathManifest:

		var args []string

		for _, arg := range v.Args {
			raw := arg.string()

			if strings.ContainsRune(raw, '$') {
				for _, word := range strings.Split(expandResourceArgument(raw, env), " ") {
					if word != "" {
						args = append(args, word)
					}
				}
			} else {
				args = append(args, raw)
			}
		}

		name = "WINDOWS_MANIFEST"

		if v.Name == tokWindowsManifest {
			if len(args) != 1 {
				throwFmt("WINDOWS_MANIFEST expects one argument")
			}

			value, parameter = args[0], "Manifest"
		} else {
			if len(args) != 0 {
				throwFmt("WINDOWS_LONG_PATH_MANIFEST expects no arguments")
			}

			value = "build/long-path.manifest"
		}
	default:
		return nil
	}

	locals := env.clone()

	locals.unbind(envBINDIR)
	locals.unbind(envCURDIR)

	if parameter != "" {
		locals.setString(internEnv(parameter), value)
		value = "$" + parameter

		if v.Name == tokSetCudaNodeRAMRequirement {
			value = "ram:" + value
		}
	}

	words := strings.Split(expandConfigMacroBody(name+" "+value, locals), " ")

	words = slices.DeleteFunc(words, func(word string) bool { return word == "" })
	value = strings.Join(words[1:], " ")

	return &SetStmt{Name: name, NameEnv: internEnv(name), Value: value, Line: v.Line}
}

func appendMacroEnv(env Environment, name, value string) {
	if previous, _ := env.lookup(name); previous != "" {
		value = previous + " " + value
	}

	env.setString(internEnv(name), value)
}

func applyPython2Addincl(d *ModuleData, env Environment, native bool) {
	d.python2Headers = true
	usePython2(env, d, unitTagPy2)
	d.usePython3 = false
	env.setString(envPythonVer, "py2")
	env.setString(envPythonTypeForCython, "PY2")

	tags := "PY2 PY2_NATIVE $_CPP_PROTO_COMPATIBLE_TAGS CPP_PROTO_FROM_SCHEMA CPP_FBS CPP_ROS $YQL_UDF_STATIC_PEERDIR_TAG $YQL_ABI_PEERDIR_TAG __EMPTY__ RESOURCE_LIB DLL_LIB"

	if native {
		env.setStringID(envMODULE_TAG, unitTagPy2Native)
		tags = "$_CPP_PROTO_COMPATIBLE_TAGS CPP_PROTO_FROM_SCHEMA CPP_FBS CPP_ROS PY2_NATIVE $YQL_UDF_STATIC_PEERDIR_TAG $YQL_ABI_PEERDIR_TAG __EMPTY__ RESOURCE_LIB DLL_LIB"
	}

	env.setString(envPeerdirTags, expandAssignedScalarVarRef(tags, env))
}

func applyPython3Addincl(d *ModuleData, env Environment) {
	d.python3NativeIncl = true
	d.usePython2, d.usePython3 = false, true
	env.setBool(envPYTHON3, true)
	env.setBool(envPYTHON2, false)
	env.setString(envPythonVer, "py3")
	env.setString(envPythonTypeForCython, "PY3")

	env.setString(envMODULE_TAG, "PY3_NATIVE")
	env.setString(envPeerdirTags, expandAssignedScalarVarRef("$_CPP_PROTO_COMPATIBLE_TAGS CPP_PROTO_FROM_SCHEMA CPP_FBS CPP_ROS PY3_NATIVE $YQL_UDF_STATIC_PEERDIR_TAG $YQL_ABI_PEERDIR_TAG __EMPTY__ RESOURCE_LIB DLL_LIB", env))
}

func finishPythonHeaderConfig(d *ModuleData, env Environment) {
	if d.python2Headers {
		if env.string(envUSE_ARCADIA_PYTHON) == "yes" {
			flag := internArg("-DARCADIA_PYTHON_UNICODE_SIZE=" + env.string(envArcadiaPythonUnicodeSize)).any()

			appendMacroEnv(env, "CFLAGS", flag.string())
			appendMacroEnv(env, "ADDINCL", "GLOBAL contrib/libs/python/Include")
			d.moduleScopeCFlags = append(d.moduleScopeCFlags, flag)
			d.addIncl = append(d.addIncl, pythonIncludeDir)
			d.addInclGlobal = append(d.addInclGlobal, pythonIncludeDir)
			d.addInclUserGlobal = append(d.addInclUserGlobal, pythonIncludeDir)
		} else if env.string(envUseSystemPython) != "" {
			appendMacroEnv(env, "PEERDIR", "build/platform/python")
			d.peerdirs = append(d.peerdirs, strBuildPlatformPython.any())
		} else {
			flags := expandStmtToken(env.string(envPythonInclude), env)

			appendMacroEnv(env, "CFLAGS", flags)
			d.moduleScopeCFlags = append(d.moduleScopeCFlags, internAnys(splitShellWords(flags))...)
		}
	}

	if d.python3NativeIncl {
		if env.string(envUSE_ARCADIA_PYTHON) == "yes" {
			flags := []ANY{argDusePython3.any()}
			include, _ := env.lookup("PY3_BASE_INCLUDE_DIR")

			if include == "" {
				include = "contrib/libs/python/Include"

				if env.string(envUSE_PYTHON3_PREV) == "yes" {
					include = "contrib/libs/python/Include_prev"
				}
			}

			if env.string(envUSE_PYTHON3_PREV) == "yes" {
				flags = append(flags, strDusePython3Prev.any())
			}

			appendMacroEnv(env, "CFLAGS", strings.Join(anyStrs(flags), " "))
			appendMacroEnv(env, "ADDINCL", "GLOBAL "+include)
			d.moduleScopeCFlags = append(d.moduleScopeCFlags, flags...)

			paths := expandConfigVFSPaths(internAnys(strings.Fields(include)), env)

			d.addIncl = append(d.addIncl, paths...)
			d.addInclGlobal = append(d.addInclGlobal, paths...)
			d.addInclUserGlobal = append(d.addInclUserGlobal, paths...)
		} else if env.string(envUseSystemPython) != "" {
			appendMacroEnv(env, "PEERDIR", "build/platform/python")
			d.peerdirs = append(d.peerdirs, strBuildPlatformPython.any())
		} else {
			flags := expandStmtToken(env.string(envPythonInclude), env)

			appendMacroEnv(env, "CFLAGS", flags)
			d.moduleScopeCFlags = append(d.moduleScopeCFlags, internAnys(splitShellWords(flags))...)
		}
	}
}

func (e *EmitContext) cowPeerdirs() []string {
	switch (Environment{s: &e.frame.ifEnv}).string(envUseCow) {
	case "yes":
		return []string{"build/cow/on"}
	case "no":
		return []string{"build/cow/off"}
	}

	return nil
}
