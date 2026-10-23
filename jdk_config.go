package main

import "strings"

func readJdkConfig(pm *IncludeParserManager) *ConfFile {
	const rel = "build/conf/java.conf"

	if !pm.fs.isFile(srcRootRel, rel) {
		return nil
	}

	return pm.confFile(rel)
}

func (p *Platform) updateJdkConfig(env Environment, input ENV, originals map[STR]STR) {
	if p.jdkConfig == nil {
		return
	}

	var field ENV

	switch input {
	case envJdkVersion, envMapsmobiBuildTarget, envOS_ANDROID:
		field = envJdkRealVersion
	case envJdkRealVersion, envUSE_SYSTEM_JDK:
		field = envJdkResourcePeerdir
	default:
		return
	}

	initial := env.s == p.ifEnv.s

	if !initial {
		env.unbind(field)

		if original, ok := originals[internStr(field.string())]; ok {
			value := original.string()

			if p.ifEnv.hasBindingID(field) {
				value = "$" + field.string() + " " + value
			}

			env.setString(field, value)
		} else if inherited, ok := p.ifEnv.lookup(field.string()); ok {
			env.setString(field, inherited)
		}
	}

	for _, stmt := range p.jdkConfig.stmts {
		if stmt.name == field.string() && (initial || stmt.branch != 0) && p.jdkConfig.holds(stmt.branch, env) {
			applyConfStmt(env, stmt, stmt.branch != 0)
		}
	}
}

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
