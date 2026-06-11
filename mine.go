package main

import (
	"runtime"
	"strconv"

	"github.com/BurntSushi/toml"
)

// toolchainFlags returns the host/target config flags (build type, python mode,
// CLANG_VER, …). Tool *paths* are no longer here — the compiler/archiver/objcopy/
// linker/python come from the build/platform/* RESOURCES_LIBRARY peers via the
// module toolchain (resolveModuleToolchain / d.tc), not from ambient flags.
func toolchainFlags(fs FS) map[string]string {
	return prebuiltToolchainFlags()
}

// linuxSDKDefault mirrors ymake_conf.py's LINUX_SDK_DEFAULT — the OS_SDK used for a
// Linux target when ya.conf sets no preset (GnuToolchainOptions: os_sdk = preset or
// default). It selects the OS_SDK_ROOT sandbox resource (ubuntu-16 → sbr:243881345)
// that build/platform/linux_sdk declares and the compile sysroot/-B point at.
const linuxSDKDefault = "ubuntu-16"

func prebuiltToolchainFlags() map[string]string {
	return map[string]string{
		"CONSISTENT_DEBUG":   "yes",
		"NO_DEBUGINFO":       "yes",
		"OS_SDK":             linuxSDKDefault,
		"TIDY":               "no",
		"USE_ARCADIA_PYTHON": "yes",
		"USE_PYTHON3":        "yes",
		// CLANG_VER is the clang major version (a scalar, not a tool path): it has no
		// external-resource counterpart, so it stays a config flag. Read into
		// Platform.ClangVer (--clang-ver) and COMPILER_VERSION.
		"CLANG_VER": "20",
	}
}

// readYaConfSection decodes ya.conf (TOML) and returns the named top-level
// table — e.g. "flags" or "host_platform_flags" — as a flat string map. Flag
// values are scalars; non-scalar entries (arrays, sub-tables) are not flags and
// are skipped. Because nested tables such as [alias.flags] decode under their
// parent key (alias), only the genuine top-level section is returned, matching
// the previous line-parser's exact-section semantics.
func readYaConfSection(fs FS, rel, wantSection string) map[string]string {
	var root map[string]any

	if _, err := toml.Decode(string(fs.read(rel)), &root); err != nil {
		ThrowFmt("ya.conf %s: %v", rel, err)
	}

	return yaConfStringTable(root[wantSection])
}

// yaConfStringTable flattens a decoded TOML table into a string map, stringifying
// scalar values and dropping composite ones (arrays / sub-tables are never flags).
func yaConfStringTable(v any) map[string]string {
	tbl, ok := v.(map[string]any)

	if !ok {
		return map[string]string{}
	}

	out := make(map[string]string, len(tbl))

	for k, val := range tbl {
		if s, ok := yaConfScalar(val); ok {
			out[k] = s
		}
	}

	return out
}

// yaConfScalar renders a TOML scalar as the literal token the old line parser
// would have produced (bool -> "true"/"false", ints/floats decimal); composite
// values return ok=false.
func yaConfScalar(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case bool:
		return strconv.FormatBool(x), true
	case int64:
		return strconv.FormatInt(x, 10), true
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64), true
	}

	return "", false
}

func hostOS() OS {
	return OS(runtime.GOOS)
}

func hostISA() ISA {
	switch runtime.GOARCH {
	case "amd64":
		return ISAX8664
	case "arm64":
		if runtime.GOOS == "darwin" || runtime.GOOS == "ios" {
			return ISAArm64
		}

		return ISAAArch64
	default:
		return ISA(runtime.GOARCH)
	}
}

func resolvePlatform(s string) (OS, ISA) {
	if s == "" {
		return hostOS(), hostISA()
	}

	return ParsePlatformID(s)
}
