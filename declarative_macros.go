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
	case tokSkipTest:
		name, value, parameter = "SKIP_TEST_VALUE", strings.Join(anyStrs(v.Args), " "), "Reason"
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

	if parameter == "" && !strings.ContainsAny(value, "$ ") {
		return &SetStmt{Name: name, NameEnv: internEnv(name), Value: value, Line: v.Line}
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
