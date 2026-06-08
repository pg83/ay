package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type graphConfResource struct {
	Name      string                 `json:"name,omitempty"`
	Pattern   string                 `json:"pattern"`
	Resource  string                 `json:"resource,omitempty"`
	Resources []graphConfResourceURI `json:"resources,omitempty"`
}

type graphConfResourceURI struct {
	Platform string `json:"platform"`
	Resource string `json:"resource"`
}

type graphConf struct {
	Resources []graphConfResource `json:"resources,omitempty"`
}

type toolOverride struct {
	Key string
	Val string
}

func toolchainFlags(fs FS, overrides []toolOverride) (map[string]string, *graphConf) {
	flags := prebuiltToolchainFlags()

	for _, o := range envToolOverrides() {
		if o.Val != "" {
			flags[o.Key] = o.Val
		}
	}

	for _, o := range overrides {
		if o.Val != "" {
			flags[o.Key] = o.Val
		}
	}

	applyExternalClangVersion(flags)

	return flags, graphConfForToolchainFlags(fs, flags)
}

func applyExternalClangVersion(flags map[string]string) {
	clang := flags["CLANG_TOOL"]

	if clang == "" || strings.HasPrefix(clang, "$(") {
		return
	}

	flags["CLANG_VER"] = mineClangMajor(clang)
}

func prebuiltToolchainFlags() map[string]string {
	const (
		clangRoot  = "$(" + resourcePatternClangTool + ")"
		lldRoot    = "$(" + resourcePatternLLDRoot + ")"
		pythonRoot = "$(" + resourcePatternYMakePython3 + ")"
	)

	flags := map[string]string{
		"CONSISTENT_DEBUG":         "yes",
		"NO_DEBUGINFO":             "yes",
		"OS_SDK":                   "local",
		"TIDY":                     "no",
		"USE_ARCADIA_PYTHON":       "yes",
		"USE_PYTHON3":              "yes",
		"BUILD_PYTHON_BIN":         pythonRoot + "/bin/python3",
		"BUILD_PYTHON3_BIN":        pythonRoot + "/bin/python3",
		"CLANG_VER":                "20",
		"CLANG_TOOL":               clangRoot + "/bin/clang",
		"CLANG_TOOL_VENDOR":        clangRoot + "/bin/clang",
		"CLANG_pl_pl_TOOL":         clangRoot + "/bin/clang++",
		"CLANG_pl_pl_TOOL_VENDOR":  clangRoot + "/bin/clang++",
		"AR_TOOL":                  clangRoot + "/bin/llvm-ar",
		"AR_TOOL_VENDOR":           clangRoot + "/bin/llvm-ar",
		"OBJCOPY_TOOL":             clangRoot + "/bin/llvm-objcopy",
		"OBJCOPY_TOOL_VENDOR":      clangRoot + "/bin/llvm-objcopy",
		"STRIP_TOOL":               clangRoot + "/bin/llvm-strip",
		"STRIP_TOOL_VENDOR":        clangRoot + "/bin/llvm-strip",
		"LLD_TOOL":                 lldRoot + "/bin/ld.lld",
		"LLD_TOOL_VENDOR":          lldRoot + "/bin/ld.lld",
		// <NAME>_RESOURCE_GLOBAL vars are bound from the build/platform/* DECLARE_*
		// statements (bindResourceGlobalVars) — no longer hardcoded here.
	}

	return flags
}

// graphConfForToolchainFlags now yields only the VCS stub. Toolchain resources
// (CLANG*, LLD_ROOT, YMAKE_PYTHON3, …) are declared by the build/platform/*
// RESOURCES_LIBRARY modules and fetched via emitResourceFetch; their executor
// mount is mechanical ($(NAME) -> <bld>/resources/NAME, see mountString). VCS is
// an inline stub no module declares.
func graphConfForToolchainFlags(_ FS, _ map[string]string) *graphConf {
	return &graphConf{Resources: []graphConfResource{{
		Name:     "vcs",
		Pattern:  "VCS",
		Resource: "base64:vcs.json:e30=",
	}}}
}

func envToolOverrides() []toolOverride {
	return []toolOverride{
		{Key: "BUILD_PYTHON_BIN", Val: envToolPath("PYTHON")},
		{Key: "BUILD_PYTHON3_BIN", Val: envToolPath("PYTHON")},
		{Key: "CLANG_TOOL", Val: firstEnvToolPath("CC", "C_COMPILER")},
		{Key: "CLANG_pl_pl_TOOL", Val: firstEnvToolPath("CXX", "CXX_COMPILER")},
		{Key: "OBJCOPY_TOOL", Val: envToolPath("OBJCOPY")},
		{Key: "AR_TOOL", Val: envToolPath("AR")},
		{Key: "STRIP_TOOL", Val: envToolPath("STRIP")},
		{Key: "LLD_TOOL", Val: firstEnvToolPath("LLD", "LD")},
	}
}

func firstEnvToolPath(names ...string) string {
	for _, name := range names {
		if v := envToolPath(name); v != "" {
			return v
		}
	}

	return ""
}

func envToolPath(name string) string {
	v := os.Getenv(name)

	if v == "" {
		return ""
	}

	if strings.HasPrefix(v, "$(") || filepath.IsAbs(v) || strings.ContainsRune(v, os.PathSeparator) {
		return v
	}

	return ""
}

func mineClangMajor(clang string) string {
	out := Throw2(exec.Command(clang, "--version").Output())
	fields := strings.Fields(string(out))

	for i, f := range fields {
		if f != "version" || i+1 >= len(fields) {
			continue
		}

		major := fields[i+1]

		if dot := strings.IndexByte(major, '.'); dot >= 0 {
			major = major[:dot]
		}

		if _, err := strconv.Atoi(major); err == nil {
			return major
		}
	}

	ThrowFmt("mineClangMajor: cannot parse clang version from %q", strings.TrimSpace(string(out)))

	return ""
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
