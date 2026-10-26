package main

import "strings"

func jdkVersionMacroAssignment(v *UnknownStmt, env Environment) *SetStmt {
	if v.Name != tokJdkVersion && v.Name != tokDefaultJdkVersion {
		return nil
	}

	args := expandStmtTokens(v.Args, env)

	if len(args) != 1 {
		throwFmt("%s expects one argument", v.Raw.string())
	}

	value := args[0].string()

	if v.Name == tokDefaultJdkVersion {
		values := strings.Fields(expandConfigMacroBody("$JDK_VERSION "+value, env))

		value = ""

		if len(values) != 0 {
			value = values[0]
		}

		env.setString(envDefaultJdkSelection, value)
	}

	value = expandConfigMacroBody(value, env)

	switch value {
	case "11", "17", "21", "22", "23", "24", "25", "26":
	default:
		throwFmt("Invalid jdk version: %s", value)
	}

	return &SetStmt{Name: "JDK_REAL_VERSION", NameEnv: envJdkRealVersion, Value: value, Line: v.Line}
}
