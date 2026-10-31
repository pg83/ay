package main

import "strings"

func isRustModule(name TOK) bool {
	switch name {
	case tokCrate, tokCrateProcMacro, tokRustBuildScript, tokRustRuntime:
		return true
	}

	return false
}

func rustModuleUnit(name TOK, language Language) ModuleUnit {
	tag := "RUST"

	switch name {
	case tokCrate:
		if language == LangRustHost {
			tag = "RUST_HOST"
		}
	case tokCrateProcMacro:
		tag = "RUST_PROC_MACRO"
	case tokRustBuildScript:
		tag = "RUST_BUILD_SCRIPT"
	case tokRustRuntime:
		tag = "RUST_RUNTIME"
	}

	return ModuleUnit{Type: name, Tag: internStr(tag), ARPrefix: "lib", SbomLang: strRust, NoStartTarget: language == LangRustHost}
}

func envRustHost(env Environment) bool {
	return env.string(envRustMode) == "host"
}

func setupRustModule(name TOK, language Language, env Environment, d *ModuleData) {
	if !isRustModule(name) {
		return
	}

	d.flags.NoPlatform = true
	d.flags.NoUtil = true
	d.flags.NoRuntime = true
	d.peerdirs = append(d.peerdirs, internAny("build/platform/rust"), internAny("build/platform/clang"))
	env.setDefaultString(envRustRoot, "src/lib.rs")
	env.setDefaultString(envRustVersion, "0.0.0")
	env.setDefaultString(envRustEdition, "2021")
	env.setDefaultString(envRustManifestDir, env.string(envCURDIR))
	env.setString(envRustKind, "rlib")
	env.setString(envRustMode, "target")
	env.setString(envMODULE_TAG, d.unit.Tag.string())
	env.setString(envMODULE_TYPE, "LIBRARY")
	env.setString(envMODULE_PREFIX, "lib")
	env.setString(envMODULE_SUFFIX, ".a")

	if env.bool(envOS_WINDOWS) {
		env.setString(envMODULE_SUFFIX, ".lib")
	}

	switch name {
	case tokCrate:
		d.multimodule = name
		d.moduleVariants = 1 << LangCPP

		if language == LangRustHost {
			env.setString(envRustMode, "host")

			suffix := "-host.a"

			if env.bool(envHostOsWindows) {
				suffix = "-host.lib"
			}

			env.setString(envMODULE_SUFFIX, suffix)
		} else {
			d.peerdirs = append(d.peerdirs, internAny("build/rust/staticlib"))
		}
	case tokRustRuntime:
		env.setString(envRustKind, "runtime")
	case tokRustBuildScript:
		env.setString(envRustKind, "bin")
		env.setString(envRustMode, "host")
		env.setString(envMODULE_TYPE, "PROGRAM")
		env.setString(envMODULE_PREFIX, "")
		env.setString(envMODULE_SUFFIX, "")

		if env.bool(envHostOsWindows) {
			env.setString(envMODULE_SUFFIX, ".exe")
		}
	case tokCrateProcMacro:
		env.setString(envRustKind, "proc-macro")
		env.setString(envRustMode, "host")
		env.setString(envMODULE_TYPE, "PROGRAM")
		env.setString(envMODULE_SUFFIX, ".so")

		if env.bool(envHostOsDarwin) {
			env.setString(envMODULE_SUFFIX, ".dylib")
		} else if env.bool(envHostOsWindows) {
			env.setString(envMODULE_SUFFIX, ".dll")
		}
	}
}

func applyRustMacro(stmt UnknownStmt, modulePath string, env Environment, d *ModuleData) bool {
	var variable string

	appendValue := false

	switch stmt.Name {
	case tokCrateRoot:
		variable = "_RUST_ROOT"
	case tokCrateVersion:
		variable = "_RUST_VERSION"
	case tokCrateEdition:
		variable = "_RUST_EDITION"
	case tokCrateManifest:
		variable = "_RUST_MANIFEST"
	case tokCrateManifestDir:
		variable = "_RUST_MANIFEST_DIR"
	case tokCrateBuildScript:
		variable = "_RUST_BUILD_SCRIPT"
	case tokCrateSources:
		variable, appendValue = "_RUST_SOURCES", true
	case tokCrateInputs:
		variable, appendValue = "_RUST_INPUTS", true
	case tokCrateFeatures:
		variable, appendValue = "_RUST_FEATURES", true
	case tokCrateFlags:
		variable, appendValue = "_RUST_FLAGS", true
	case tokCrateEnv:
		variable, appendValue = "_RUST_ENV", true
	case tokCrateProcMacros:
		variable, appendValue = "_RUST_PROC_MACROS", true
	case tokCrateBuildTool, tokCrateDependencyAlias:
		if len(stmt.Args) != 2 {
			throwFmt("%s expects a name and a module directory", stmt.Name)
		}

		variable, appendValue = "_RUST_BUILD_TOOLS", true

		if stmt.Name == tokCrateDependencyAlias {
			variable = "_RUST_ALIASES"
		}
	case tokCrateBuildDeps:
		d.peerdirs = append(d.peerdirs, stmt.Args...)
	default:
		return false
	}

	value := strings.Join(anyStrs(stmt.Args), " ")

	switch stmt.Name {
	case tokCrateManifestDir:
		value = source(modulePath).string() + "/" + value
	case tokCrateBuildTool, tokCrateDependencyAlias:
		value = stmt.Args[0].string() + "=" + stmt.Args[1].string()
	}

	if appendValue {
		appendMacroEnv(env, variable, value)
	} else if variable != "" {
		if len(stmt.Args) != 1 {
			throwFmt("%s expects one argument", stmt.Name)
		}

		env.setString(internEnv(variable), value)
	}

	for _, arg := range stmt.Args {
		markUserArg(arg)
	}

	recordHandledMacro(stmt.Name, stmt.Args)

	return true
}

func rustTarget(p *Platform, host bool) string {
	arch := string(p.ISA)

	if p.ISA == ISAArm64 {
		arch = "aarch64"
	}

	switch p.OS {
	case OSLinux:
		libc := "gnu"

		if !host && p.ifEnv.string(envMUSL) == "yes" {
			libc = "musl"
		}

		return arch + "-unknown-linux-" + libc
	case OSDarwin:
		return arch + "-apple-darwin"
	case OSWindows:
		if p.ISA == ISAX8664 {
			return arch + "-pc-windows-msvc"
		}
	}

	return "unsupported"
}
