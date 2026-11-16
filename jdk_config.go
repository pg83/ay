package main

import (
	"slices"
	"strings"
)

var jdkVersions = []string{"11", "17", "21", "22", "23", "24", "25", "26"}

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

	if msg := jdkVersionError(value); msg != "" {
		throwFmt("%s", msg)
	}

	return newSetStmt("JDK_REAL_VERSION", envJdkRealVersion, value, v.Line)
}

func jdkVersionError(value string) string {
	if slices.Contains(jdkVersions, value) {
		return ""
	}

	return "Invalid jdk version: " + value + ". ('" + strings.Join(jdkVersions, "', '") + "') are available"
}
