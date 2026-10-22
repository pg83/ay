package main

import (
	"slices"
	"strings"
)

var (
	envSbrUIDExt              = internEnv("SBR_UID_EXT")
	envPackedPackageExt       = internEnv("PACKED_PACKAGE_EXT")
	envCudaNodeRAMRequirement = internEnv("_CU_NODE_MAX_RAM_REQUIREMENT")
	envUseCow                 = internEnv("USE_COW")
)

// These upstream macros contain only SET/ENABLE. Lower them during collection,
// so the lexical IR can be reused with each module's own environment.
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
		// VarStatement dereferences arguments before signature binding, then
		// PrepareMacroCall deeply substitutes the configuration SET body.
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

	// PrepareMacroBody binds arguments in a local scope. PrepareMacroCall then
	// substitutes the entire SET body once, splits it, and passes it to EvalExpr.
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

// python.conf: PYTHON3_ADDINCL -> _PYTHON3_ADDINCL. This selects headers
// without adding USE_PYTHON3's runtime peers.
func applyPython3Addincl(d *ModuleData, env Environment) {
	d.python3NativeIncl = true
	d.usePython2, d.usePython3 = false, true
	env.setBool(envPYTHON3, true)
	env.setBool(envPYTHON2, false)
	env.setString(internEnv("_PYTHON_VER"), "py3")
	env.setString(internEnv("PYTHON_TYPE_FOR_CYTHON"), "PY3")

	if env.string(envUSE_ARCADIA_PYTHON) == "yes" {
		flags := []ANY{argDusePython3.any()}
		include, _ := env.lookup("PY3_BASE_INCLUDE_DIR")

		if include == "" {
			include = "contrib/libs/python/Include"

			if env.string(internEnv("USE_PYTHON3_PREV")) == "yes" {
				include = "contrib/libs/python/Include_prev"
			}
		}

		if env.string(internEnv("USE_PYTHON3_PREV")) == "yes" {
			flags = append(flags, internStr("-DUSE_PYTHON3_PREV").any())
		}

		appendMacroEnv(env, "CFLAGS", strings.Join(anyStrs(flags), " "))
		appendMacroEnv(env, "ADDINCL", "GLOBAL "+include)
		d.moduleScopeCFlags = append(d.moduleScopeCFlags, flags...)
		paths := expandConfigVFSPaths(internAnys(strings.Fields(include)), env)
		d.addIncl = append(d.addIncl, paths...)
		d.addInclGlobal = append(d.addInclGlobal, paths...)
		d.addInclUserGlobal = append(d.addInclUserGlobal, paths...)
	} else if env.bool(internEnv("USE_SYSTEM_PYTHON")) {
		appendMacroEnv(env, "PEERDIR", "build/platform/python")
		d.peerdirs = append(d.peerdirs, internStr("build/platform/python").any())
	} else {
		flags := expandStmtToken(env.string(internEnv("PYTHON_INCLUDE")), env)

		appendMacroEnv(env, "CFLAGS", flags)
		d.moduleScopeCFlags = append(d.moduleScopeCFlags, internAnys(splitShellWords(flags))...)
	}

	env.setString(envMODULE_TAG, "PY3_NATIVE")
	env.setString(internEnv("PEERDIR_TAGS"), expandAssignedScalarVarRef("$_CPP_PROTO_COMPATIBLE_TAGS CPP_PROTO_FROM_SCHEMA CPP_FBS CPP_ROS PY3_NATIVE $YQL_UDF_STATIC_PEERDIR_TAG $YQL_ABI_PEERDIR_TAG __EMPTY__ RESOURCE_LIB DLL_LIB", env))
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
