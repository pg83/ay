package main

import (
	"runtime"
	"strings"
)

// toolchainFlags returns the host/target config flags (build type, python mode,
// CLANG_VER, …). Tool *paths* are no longer here — the compiler/archiver/objcopy/
// linker/python come from the build/platform/* RESOURCES_LIBRARY peers via the
// module toolchain (resolveModuleToolchain / d.tc), not from ambient flags.
func toolchainFlags(fs FS) map[string]string {
	return prebuiltToolchainFlags()
}

func prebuiltToolchainFlags() map[string]string {
	return map[string]string{
		"CONSISTENT_DEBUG":   "yes",
		"NO_DEBUGINFO":       "yes",
		"OS_SDK":             "local",
		"TIDY":               "no",
		"USE_ARCADIA_PYTHON": "yes",
		"USE_PYTHON3":        "yes",
		// CLANG_VER is the clang major version (a scalar, not a tool path): it has no
		// external-resource counterpart, so it stays a config flag. Read into
		// Platform.ClangVer (--clang-ver) and COMPILER_VERSION.
		"CLANG_VER": "20",
	}
}

func readYaConfSection(fs FS, rel, wantSection string) map[string]string {
	raw := fs.Read(rel)
	out := map[string]string{}
	section := ""

	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))

			continue
		}

		if section != wantSection {
			continue
		}

		key, val, ok := strings.Cut(line, "=")

		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		val = strings.Trim(val, `"`)

		if key != "" {
			out[key] = val
		}
	}

	return out
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
