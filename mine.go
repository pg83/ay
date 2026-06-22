package main

import (
	"runtime"
	"strconv"

	"github.com/BurntSushi/toml"
)

// toolchainFlags returns host/target config flags. Tool paths are not here —
// they come from the module toolchain's resource peers.
func toolchainFlags(fs FS) map[string]string {
	return prebuiltToolchainFlags()
}

// linuxSDKDefault is the OS_SDK used for a Linux target when no preset is set.
const linuxSDKDefault = "ubuntu-16"

func prebuiltToolchainFlags() map[string]string {
	return map[string]string{
		"CONSISTENT_DEBUG":   "yes",
		"NO_DEBUGINFO":       "yes",
		"OS_SDK":             linuxSDKDefault,
		"TIDY":               "no",
		"USE_ARCADIA_PYTHON": "yes",
		"USE_PYTHON3":        "yes",
		// CLANG_VER is a scalar (clang major version), not a tool path, so it
		// stays a config flag.
		"CLANG_VER": "20",
	}
}

// readYaConfSection decodes ya.conf (TOML) and returns the named top-level
// table as a flat string map; non-scalar entries are skipped.
func readYaConfSection(fs FS, rel, wantSection string) map[string]string {
	var root map[string]any

	if _, err := toml.Decode(string(fs.read(rel)), &root); err != nil {
		throwFmt("ya.conf %s: %v", rel, err)
	}

	return yaConfStringTable(root[wantSection])
}

// yaConfStringTable flattens a decoded TOML table into a string map; composite
// values are dropped.
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

// yaConfScalar renders a TOML scalar as a literal token; composite values
// return ok=false.
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

	return parsePlatformID(s)
}
