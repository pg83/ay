package main

import "strings"

var perlConfigOutputs = []ENV{
	envPerlVersion, envPerlRoot, envPerlLibArch, envPerl,
	envPerlPrivlib, envPerlArchlib, envPerlLibs,
	envPerlLdLibraryPath, envPerlInclude,
}

const (
	perlVersionField uint16 = 1 << iota
	perlRootField
	perlLibArchField
	perlCommandField
	perlPrivlibField
	perlArchlibField
	perlLibsField
	perlLDPathField
	perlIncludeField
	perlAllFields = (1 << 9) - 1
)

func perlInputMask(key ENV) uint16 {
	switch key {
	case envPerlSdk:
		return perlVersionField | perlRootField | perlLibArchField
	case envUsePerlSystem:
		return perlAllFields
	case envUseLocalTools:
		return perlVersionField | perlCommandField | perlPrivlibField | perlArchlibField | perlLibsField
	case envLocalPerlLibs:
		return perlLibsField
	}

	return 0
}

func initializePerlEnv(env Environment) {
	env.setDefaultString(envUsePerlSystem, "yes")
	env.setDefaultString(envPerlLdLibraryPath, "")
	env.setDefaultString(envPerlInclude, "")
	applyPerlFields(env, perlConfigOutputs)
}

func recalcPerlEnv(env Environment, platform *Platform, sets map[STR]STR, mask uint16) {
	var selected [9]ENV

	fields := selected[:0]

	for i, field := range perlConfigOutputs {
		if mask&(1<<i) == 0 {
			continue
		}

		fields = append(fields, field)

		name := field.string()

		if value, ok := sets[internStr(name)]; ok {
			env.setStringID(field, value)
		} else if value, ok := platform.ifEnv.lookup(name); ok {
			env.setString(field, value)
		} else {
			env.unbind(field)
		}
	}

	applyPerlFields(env, fields)
}

func perlSDKLayout(sdk string) (version, arch, prefix string) {
	switch sdk {
	case "ubuntu-12":
		version = "5.14"
	case "ubuntu-14":
		version = "5.18"
	case "ubuntu-16":
		version, arch = "5.22", "/x86_64-linux-gnu"
	case "ubuntu-18":
		version, arch = "5.26", "/x86_64-linux-gnu"
	case "ubuntu-20":
		version, arch, prefix = "5.30", "/x86_64-linux-gnu", "/usr"
	case "ubuntu-24":
		version, arch, prefix = "5.38", "/x86_64-linux-gnu", "/usr"
	}

	if version != "" && prefix == "" {
		prefix = "/perl${PERL_VERSION}"
	}

	return
}

func applyPerlFields(env Environment, fields []ENV) {
	system := expandConfScalar(env.string(envUsePerlSystem), env) == "yes"
	local := expandConfScalar(env.string(envUseLocalTools), env) == "yes"
	version, arch, prefix := "", "", ""

	if system && fields[0] == envPerlVersion {
		version, arch, prefix = perlSDKLayout(expandConfScalar(env.string(envPerlSdk), env))
	}

	for _, field := range fields {
		name := field.string()

		switch field {
		case envPerlVersion, envPerl, envPerlPrivlib, envPerlArchlib:
			value := "NO-PERL-SELECTED"

			if name != "PERL" {
				value += "-" + strings.TrimPrefix(name, "PERL_")
			}

			if local {
				value = "$LOCAL_" + name
			}

			env.setString(field, value)
		case envPerlLibs:
			if local && stringIsTruthy(expandConfScalar(env.string(envLocalPerlLibs), env)) {
				env.setString(field, "$LOCAL_PERL_LIBS")
			}
		}

		if !system {
			continue
		}

		switch field {
		case envPerlVersion:
			if version != "" {
				env.setString(field, version)
			}
		case envPerlRoot:
			if version != "" {
				env.setString(field, "${SYSTEM_PERL_"+strings.ReplaceAll(version, ".", "")+"_RESOURCE_GLOBAL}"+prefix)
			}
		case envPerlLibArch:
			if version != "" {
				env.setString(field, arch)
			}
		case envPerl:
			env.setString(field, "${PERL_ROOT}/bin/perl")
		case envPerlArchlib:
			env.setString(field, "${PERL_ROOT}/lib${PERL_LIB_ARCH}/perl/${PERL_VERSION}")
		case envPerlPrivlib:
			env.setString(field, "${PERL_ROOT}/share/perl/${PERL_VERSION}")
		case envPerlLibs:
			env.setString(field, "${PERL_ROOT}/lib${PERL_LIB_ARCH}")
		case envPerlLdLibraryPath:
			env.setString(field, "LD_LIBRARY_PATH=${PERL_ROOT}/lib")
		case envPerlInclude:
			env.setString(field, "-I${PERL_ARCHLIB} -I${PERL_PRIVLIB}")
		}
	}
}
