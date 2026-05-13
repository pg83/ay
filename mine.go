package main

import (
	"fmt"
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

// hostPlatformID returns the canonical `default-<os>-<arch>` triple
// for the running process. Used as the default for both
// `--target-platform` and `--host-platform` when neither flag is set.
func hostPlatformID() string {
	return fmt.Sprintf("default-%s-%s", runtime.GOOS, hostArch())
}

func hostArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "arm64":
		if runtime.GOOS == "darwin" || runtime.GOOS == "ios" {
			return "arm64"
		}

		return "aarch64"
	default:
		return runtime.GOARCH
	}
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
