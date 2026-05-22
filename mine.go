package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// mine.go — toolchain discovery and flag mining.
//
// Resolves a fixed set of programs via $PATH and projects them into the
// ymake-style flag namespace (BUILD_PYTHON_BIN, CLANG_TOOL,
// CLANG_pl_pl_TOOL, etc.).

// mineTools resolves each tool's absolute path via exec.LookPath.
// Missing tools throw; the full set is required, no partial discovery.
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
// Each tool surfaces as `<UPPER>_TOOL` and `<UPPER>_TOOL_VENDOR` keys
// (with `+` transliterated to `_pl` for ymake's `clang++` →
// `CLANG_pl_pl_TOOL`), plus baseline scalars used by the conf templates.
func commonFlags(tools map[string]string) map[string]string {
	res := map[string]string{
		"CONSISTENT_DEBUG":         "yes",
		"NO_DEBUGINFO":             "yes",
		"OS_SDK":                   "local",
		"TIDY":                     "no",
		"USE_ARCADIA_PYTHON":       "yes",
		"USE_PREBUILT_TOOLS":       "no",
		"USE_PYTHON3":              "yes",
		"BUILD_PYTHON_BIN":         canonicalizeResourcePatternRefs(tools["python3"]),
		"BUILD_PYTHON3_BIN":        canonicalizeResourcePatternRefs(tools["python3"]),
		"CLANG_VER":                mineClangMajor(tools["clang"]),
		"CLANG16_RESOURCE_GLOBAL":  resourceGlobalRef("CLANG16_RESOURCE_GLOBAL", resourcePatternClang16),
		"LLD_ROOT_RESOURCE_GLOBAL": resourceGlobalRef("LLD_ROOT_RESOURCE_GLOBAL", resourcePatternLLDRoot),
	}

	for k, v := range tools {
		// ymake's convention is mixed-case for the `+` transliteration
		// (`clang++` → `CLANG_pl_pl_TOOL`, NOT `CLANG_PL_PL_TOOL`); the
		// `_pl` is a marker token, not a token to upper-case. Uppercase
		// the alpha component first, then transliterate `+`.
		key := strings.ReplaceAll(strings.ToUpper(k), "+", "_pl")
		res[key+"_TOOL"] = canonicalizeResourcePatternRefs(v)
		res[key+"_TOOL_VENDOR"] = canonicalizeResourcePatternRefs(v)
	}

	return res
}

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

func toolchainFlags(fs *FS, overrides []toolOverride) (map[string]string, *graphConf) {
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
		"CLANG16_RESOURCE_GLOBAL":  resourceGlobalRef("CLANG16_RESOURCE_GLOBAL", resourcePatternClang16),
		"LLD_ROOT_RESOURCE_GLOBAL": resourceGlobalRef("LLD_ROOT_RESOURCE_GLOBAL", resourcePatternLLDRoot),
	}

	return flags
}

func graphConfForToolchainFlags(fs *FS, flags map[string]string) *graphConf {
	resources := make([]graphConfResource, 0, 5)

	if flagsUsePattern(flags, resourcePatternYMakePython3) {
		resources = append(resources, readHostResourcesBundle(fs, resourcePatternYMakePython3, "build/platform/python/ymake_python3/resources.json", true))
	}

	if flagsUsePattern(flags, resourcePatternClang16) {
		resources = append(resources, readHostResourcesBundle(fs, resourcePatternClang16, "build/platform/clang/clang16.json", true))
	}

	if flagsUsePattern(flags, resourcePatternLLDRoot) {
		resources = append(resources, readHostResourcesBundle(fs, resourcePatternLLDRoot, "build/platform/lld/lld20.json", true))
	}

	if flagsUsePattern(flags, resourcePatternClangTool) {
		resources = append(resources, readHostResourcesBundle(fs, resourcePatternClangTool, "build/platform/clang/clang20.json", false))
	}

	resources = append(resources, readHostResourcesBundle(fs, resourcePatternJDK17, "build/platform/java/jdk/jdk17/jdk.json", true))
	resources = append(resources, graphConfResource{
		Name:     "vcs",
		Pattern:  "VCS",
		Resource: "base64:vcs.json:e30=",
	})

	return &graphConf{Resources: resources}
}

func flagsUsePattern(flags map[string]string, pattern string) bool {
	ref := resourcePatternRef(pattern)
	for _, v := range flags {
		if strings.Contains(v, ref) {
			return true
		}
	}

	return false
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
		return canonicalizeResourcePatternRefs(v)
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

func readYaConfSection(fs *FS, rel, wantSection string) map[string]string {
	raw := Throw2(fs.Read(rel))
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

type hostResourcesJSON struct {
	ByPlatform map[string]struct {
		URI string `json:"uri"`
	} `json:"by_platform"`
}

func readHostResourcesBundle(fs *FS, pattern, rel string, upperPlatform bool) graphConfResource {
	var data hostResourcesJSON
	raw := Throw2(fs.Read(rel))
	Throw(json.Unmarshal(raw, &data))

	order := []string{
		"darwin-x86_64",
		"darwin-arm64",
		"linux-x86_64",
		"linux-aarch64",
		"win32-x86_64",
	}

	res := graphConfResource{Pattern: pattern}

	for _, key := range order {
		item, ok := data.ByPlatform[key]
		if !ok {
			continue
		}

		res.Resources = append(res.Resources, graphConfResourceURI{
			Platform: resourcePlatformName(key, upperPlatform),
			Resource: item.URI,
		})
	}

	return res
}

func resourcePlatformName(key string, upper bool) string {
	name := strings.TrimSuffix(key, "-x86_64")

	if !upper {
		return name
	}

	return strings.ToUpper(name)
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
// for the running process. Used by `ay make` to seed
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
