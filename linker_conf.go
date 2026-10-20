package main

import "strings"

var linkerConfNames = []string{
	"_START_GROUP", "_END_GROUP", "BEFORE_PEERS", "AFTER_PEERS",
	"_LD_SRCS_GLOBALS", "_ROOT_FLAGS", "_SONAME_OPTION", "_SONAME_FLAG", "_SHARED_FLAG",
	"LD_STRIP_FLAG", "_LD_DCE_FLAG_GC_SECTIONS", "_LD_DCE_FLAG_PRINT_SECTIONS", "LD_DCE_FLAG",
	"_LD_ICF_FLAG", "_LD_ICF_FLAG_PRINT_SECTIONS", "LD_ICF_FLAG", "_LD_NO_PIE_FLAG",
	"_LD_USE_STDLIB", "_C_SYSTEM_LIBRARIES", "C_SYSTEM_LIBRARIES", "C_SYSTEM_LIBRARIES_INTERCEPT",
	"LD_EXPORT_ALL_DYNAMIC_SYMBOLS_FLAG", "EXPORTS_VALUE",
}

type LinkerFlags struct {
	StartGroup, EndGroup, BeforePeers, AfterPeers []ANY
	Globals, Roots, Shared, Soname, Exports       []ANY
	SystemLibs, Intercept, Strip, DCE, ICF, NoPie []ANY
	ArgCount                                      int
}

func readLinkerConf(fs FS, cache *SharedParseCache) *ConfAssignments {
	if !fs.isFile(srcRootRel, ldConfRel) {
		throwFmt("linker conf: %s is missing", ldConfRel)
	}

	return cache.confFile(fs, ldConfRel)
}

func (c *ConfAssignments) flags(p *Platform) LinkerFlags {
	env := p.ifEnv.clone()

	for _, name := range linkerConfNames {
		env.setString(internEnv(name), "")
	}

	if !env.hasBindingID(envLinkerId) {
		env.setString(envLinkerId, env.string(envDefaultLinkerId))
	}

	env.setString(envExportsFile, "${input:EXPORTS_FILE}")
	env.setString(envSoname, "$_SONAME")

	c.apply(env, "")

	for _, name := range linkerConfNames {
		key := internEnv(name)

		if value, ok := p.Flags[key]; ok {
			env.setStringID(key, value)
		}
	}

	var result LinkerFlags

	read := func(name string) []ANY {
		value := expandStmtToken(env.string(internEnv(name)), env)

		value = strings.ReplaceAll(value, "$(BUILD_ROOT)", "$(B)")
		value = strings.ReplaceAll(value, "$(SOURCE_ROOT)", "$(S)")

		args := internAnys(splitShellWords(value))

		result.ArgCount += len(args)

		return args
	}

	result.StartGroup = read("_START_GROUP")
	result.EndGroup = read("_END_GROUP")
	result.BeforePeers = read("BEFORE_PEERS")
	result.AfterPeers = read("AFTER_PEERS")
	result.Globals = read("_LD_SRCS_GLOBALS")
	result.Roots = read("_ROOT_FLAGS")
	result.Shared = read("_SHARED_FLAG")
	result.Soname = read("_SONAME_FLAG")
	result.Exports = read("EXPORTS_VALUE")
	result.SystemLibs = read("C_SYSTEM_LIBRARIES")
	result.Intercept = read("C_SYSTEM_LIBRARIES_INTERCEPT")
	result.Strip = read("LD_STRIP_FLAG")
	result.NoPie = read("_LD_NO_PIE_FLAG")

	if !env.bool(envNoLinkerDce) {
		result.DCE = read("LD_DCE_FLAG")
	}

	if env.bool(envBtMinsizerel) || env.bool(envLinkerIcf) {
		result.ICF = read("LD_ICF_FLAG")
	}

	return result
}

func appendLinkerGlobals(args []ANY, flags LinkerFlags, archives, objects []VFS) []ANY {
	for _, arg := range flags.Globals {
		var paths []VFS

		switch arg.string() {
		case "${rootrel;ext=.a:SRCS_GLOBAL}":
			paths = archives
		case "${rootrel;ext=.o:SRCS_GLOBAL}":
			paths = objects
		case "${rootrel;ext=.supp:SRCS_GLOBAL}":
		default:
			args = append(args, arg)
		}

		for _, path := range paths {
			args = append(args, path.rel().any())
		}
	}

	return args
}

func appendLinkerTemplate(args, template []ANY, variable, value string) []ANY {
	for _, arg := range template {
		if strings.Contains(arg.string(), variable) {
			if value != "" {
				args = append(args, internAny(strings.ReplaceAll(arg.string(), variable, value)))
			}
		} else {
			args = append(args, arg)
		}
	}

	return args
}

func (f LinkerFlags) registerOwned() {
	for _, args := range [][]ANY{f.StartGroup, f.EndGroup, f.BeforePeers, f.AfterPeers, f.Globals, f.Roots, f.Shared, f.Soname, f.Exports, f.SystemLibs, f.Intercept, f.Strip, f.DCE, f.ICF, f.NoPie} {
		registerOwnedSlice(args)
	}
}
