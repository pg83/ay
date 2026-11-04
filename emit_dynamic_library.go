package main

type DllModuleArgs struct {
	name              ANY
	prefix            *ANY
	version           string
	exports           *ANY
	hasName           bool
	deprecatedExports bool
}

func parseDllModuleArgs(args []ANY) DllModuleArgs {
	var out DllModuleArgs

	for i := 0; i < len(args); i++ {
		switch args[i].string() {
		case "PREFIX", "EXPORTS":
			keyword := args[i].string()

			if keyword == "EXPORTS" {
				out.deprecatedExports = true
			}

			if i+1 < len(args) {
				i++

				if keyword == "PREFIX" {
					out.prefix = &args[i]
				} else {
					out.exports = &args[i]
				}
			}
		default:
			if !out.hasName {
				out.name = args[i]
				out.hasName = true
			} else if out.version == "" {
				out.version = "." + args[i].string()
			}
		}
	}

	return out
}

func bindDllModulePrefix(stmt *ModuleStmt, env Environment) {
	args := parseDllModuleArgs(stmt.Args)

	if args.prefix != nil {
		bound := append([]ANY(nil), stmt.Args...)

		for i := range stmt.Args {
			if args.prefix == &stmt.Args[i] {
				bound[i] = expandStmtTokenAny(*args.prefix, env)
			}
		}

		stmt.Args = bound
	}
}

func dllOutputName(instance ModuleInstance, stmt *ModuleStmt) string {
	args := parseDllModuleArgs(stmt.Args)
	prefix := "lib"
	suffix := ".so"
	env := instance.Platform.ifEnv

	if env.bool(envOS_WINDOWS) {
		prefix, suffix = "", ".dll"
	} else if env.bool(envOS_DARWIN) || env.bool(envOsIos) {
		suffix = ".dylib"
	}

	if isPythonDllModule(stmt.Name) {
		prefix, suffix = "", ".so"

		if env.bool(envOS_WINDOWS) {
			suffix = ".pyd"
		}
	}

	if args.prefix != nil {
		prefix = args.prefix.string()
	}

	name := javaRealPrjName(instance.Path.relString(), javaNameTwoDirs)

	if args.hasName {
		name = args.name.string()
	}

	return prefix + name + suffix
}

func dllModuleVersion(stmt *ModuleStmt) string {
	return parseDllModuleArgs(stmt.Args).version
}

func dllExportsArg(stmt *ModuleStmt) *ANY {
	return parseDllModuleArgs(stmt.Args).exports
}
