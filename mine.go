package main

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// mine.go — toolchain discovery and flag mining.
//
// Ported from the gg/ya.go MVP path: `findTools` resolves a fixed set
// of programs via $PATH (clang, clang++, ar, objcopy, strip, python3,
// lld) and `commonFlags` projects those into the ymake-style flag
// namespace (BUILD_PYTHON_BIN, CLANG_TOOL, CLANG_pl_pl_TOOL, etc.)
// expected by downstream consumers in cmd_args composition.
//
// Both subcommands (`gen` and `make`) consult these to populate
// cliDefines so the resulting graph is dynamically pinned to the
// current host's toolchain layout rather than hard-coded paths from a
// reference snapshot.

// mineTools resolves each tool's absolute path via exec.LookPath.
// Missing tools throw with a clear message; per MVP scope we require
// the full set, no partial discovery.
func mineTools() map[string]string {
	names := []string{
		"clang",
		"clang++",
		"ar",
		"objcopy",
		"strip",
		"python3",
		"lld",
	}

	out := make(map[string]string, len(names))

	for _, n := range names {
		bin := n

		// The ymake naming convention talks about "llvm-strip" etc.;
		// resolve those by their actual command names while keeping
		// the short logical key in the map.
		switch n {
		case "ar", "objcopy", "strip":
			bin = "llvm-" + n
		}

		path, err := exec.LookPath(bin)

		if err != nil {
			ThrowFmt("mineTools: %q not found in PATH: %v", bin, err)
		}

		out[n] = path
	}

	return out
}

// commonFlags builds the ymake-style flag map from a mined tools map.
// The shape mirrors the old gg/ya.go commonFlags() function: each tool
// surfaces as `<UPPER>_TOOL` and `<UPPER>_TOOL_VENDOR` keys (with `+`
// transliterated to `_pl` for ymake's `clang++` → `CLANG_pl_pl_TOOL`),
// plus baseline scalars used by the conf templates.
func commonFlags(tools map[string]string) map[string]string {
	res := map[string]string{
		"CONSISTENT_DEBUG":         "yes",
		"NO_DEBUGINFO":             "yes",
		"OS_SDK":                   "local",
		"TIDY":                     "no",
		"USE_ARCADIA_PYTHON":       "yes",
		"USE_PREBUILT_TOOLS":       "no",
		"USE_PYTHON3":              "yes",
		"BUILD_PYTHON_BIN":         tools["python3"],
		"BUILD_PYTHON3_BIN":        tools["python3"],
		"LLD_ROOT_RESOURCE_GLOBAL": filepath.Dir(filepath.Dir(tools["lld"])),
	}

	for k, v := range tools {
		key := strings.ToUpper(strings.ReplaceAll(k, "+", "_pl"))
		res[key+"_TOOL"] = v
		res[key+"_TOOL_VENDOR"] = v
	}

	return res
}

// hostOS / hostISA / hostPlatformID surface the running process's
// typed axes for the CLI fallback (when `--host-platform` is unset).
// hostOS reads Go's runtime.GOOS verbatim; hostISA normalises Go's
// runtime.GOARCH into the conventional ymake triple component
// (amd64 → x86_64; arm64 → aarch64 on Linux, arm64 elsewhere).
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

// hostPlatformID returns the canonical `default-<os>-<isa>` triple
// for the running process. Used by `yatool make` to seed
// `GG_TARGET_PLATFORM` when the CLI flag is not supplied.
func hostPlatformID() string {
	return string(MakePlatformID(hostOS(), hostISA()))
}

// resolvePlatform parses the canonical `default-<os>-<isa>` form;
// when the string is empty, mines OS/ISA from the running process
// (hostOS / hostISA). The CLI caller threads the target's empty-
// string fallback through this same function by passing the
// already-resolved host's PlatformID string instead of "".
func resolvePlatform(s string) (OS, ISA) {
	if s == "" {
		return hostOS(), hostISA()
	}
	return ParsePlatformID(s)
}

// mergeFlags returns a fresh map containing every entry from `base`
// overlaid with `over` (over wins on conflict). Useful for merging
// mined defaults with user-supplied -D flags.
func mergeFlags(base, over map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(over))

	for k, v := range base {
		out[k] = v
	}

	for k, v := range over {
		out[k] = v
	}

	return out
}
