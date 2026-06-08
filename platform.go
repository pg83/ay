package main

import (
	"bytes"
	"strings"
)

// gzZstdRule is the verbatim line in a repo's build/ymake_conf.py that appends the
// -gz=zstd debug-section-compression flag for non-release Linux targets. ydb's conf
// carries it; yatool's does not — the sole config-level reason the flag appears in
// some builds and not others. confCompressesDebug detects its presence.
const gzZstdRule = "debug_info_flags.append('-gz=zstd')"

// confCompressesDebug reports whether the source repo's build/ymake_conf.py adds
// -gz=zstd to the debug-info flags. The conf is an optional file at the source-root
// boundary (a minimal repo or test tree may lack it), so absence means "no rule".
func confCompressesDebug(fs FS) bool {
	if !fs.IsFile(srcRootVFS, "build/ymake_conf.py") {
		return false
	}

	return bytes.Contains(fs.Read("build/ymake_conf.py"), []byte(gzZstdRule))
}

type Platform struct {
	OS     OS
	ISA    ISA
	Target PlatformID
	// Flags is the interned IF/config flag set: keys are ENV, values STR. The raw
	// CLI/conf string map is interned once here (internFlags) at platform
	// construction — the single string boundary — so nothing downstream keys env
	// by string. Toolchain/build-config derivations inside NewPlatform read the
	// raw input map; everything past the platform uses ENV/STR.
	Flags map[ENV]STR
	Tags  []string

	Tools Toolchain

	PIC             bool
	BuildType       string
	BuildRelease    bool
	BuildSanitized  bool
	Ragel6Optimized bool

	Triple string
	March  string

	// Pre-interned cmd-arg tokens (STR), computed once per platform so the
	// per-CC-node compile line doesn't re-intern the (constant) compiler path
	// and --target.
	CCArg     STR
	CXXArg    STR
	TargetArg STR

	CFlags   []ARG
	CXXFlags []ARG

	// DebugInfoFlags is the platform's debug-info flag group (-g, optional
	// -gz=zstd, -fdebug-default-version=4, -ggnu-pubnames), derived once at
	// construction from the build type and the source repo's ymake_conf.py.
	// CompileCFlags is the full compile C flag vector with that group spliced
	// into its natural slot, composed once so the per-source line needn't rebuild
	// it. See buildDebugInfoFlags / composeCompileCFlags.
	DebugInfoFlags []ARG
	CompileCFlags  []ARG

	SystemLibs       []string
	LinkPreludeExtra []string

	ClangVer string

	// ClangVerSTR / BuildTypeUpperSTR are the interned forms of the COMPILER_VERSION
	// and BUILD_TYPE env values, computed once per platform so buildIfEnv binds them
	// via SetStringID instead of re-interning the same constant on every module.
	ClangVerSTR       STR
	BuildTypeUpperSTR STR
}

type Toolchain struct {
	Python3 string
	CC      string
	CXX     string
	Objcopy string
	AR      string
	Strip   string
	LLD     string
}

// internFlags interns the raw CLI/conf flag map into the ENV/STR form stored on
// Platform.Flags. The one place env-flag strings are interned.
func internFlags(flags map[string]string) map[ENV]STR {
	out := make(map[ENV]STR, len(flags))

	for k, v := range flags {
		out[internEnv(k)] = internStr(v)
	}

	return out
}

func toolchainFromFlags(flags map[string]string) Toolchain {
	return Toolchain{
		Python3: flags["BUILD_PYTHON_BIN"],
		CC:      flags["CLANG_TOOL"],
		CXX:     flags["CLANG_pl_pl_TOOL"],
		Objcopy: flags["OBJCOPY_TOOL"],
		AR:      flags["AR_TOOL"],
		Strip:   flags["STRIP_TOOL"],
		LLD:     flags["LLD_TOOL"],
	}
}

func NewPlatform(fs FS, os OS, isa ISA, flags map[string]string, tags []string, cflagsEnv, cxxflagsEnv string) *Platform {
	if flags == nil {
		flags = map[string]string{}
	}

	if tags == nil {
		tags = []string{}
	}

	buildType := platformBuildType(flags)
	buildSanitized := platformBuildSanitized(flags)
	buildRelease := isReleaseBuildType(buildType)

	var systemLibs, linkPreludeExtra []string

	if flags["MUSL"] == "yes" {
		systemLibs = []string{"-nostdlib", "-lm"}
	} else {
		linkPreludeExtra = []string{"-ldl", "-lrt"}
		systemLibs = []string{"-nodefaultlibs", "-lpthread", "-lc", "-lm"}
	}

	p := &Platform{
		OS:                os,
		ISA:               isa,
		Target:            MakePlatformID(os, isa),
		Flags:             internFlags(flags),
		Tags:              tags,
		Tools:             toolchainFromFlags(flags),
		PIC:               flags["PIC"] == "yes",
		BuildType:         buildType,
		BuildRelease:      buildRelease,
		BuildSanitized:    buildSanitized,
		Ragel6Optimized:   buildRelease && !buildSanitized,
		Triple:            string(isa) + "-" + string(os) + "-gnu",
		March:             marchFor(isa),
		CFlags:            internArgs(parseCompilerFlags(cflagsEnv)),
		CXXFlags:          internArgs(parseCompilerFlags(cxxflagsEnv)),
		SystemLibs:        systemLibs,
		LinkPreludeExtra:  linkPreludeExtra,
		ClangVer:          platformClangVersion(flags),
		ClangVerSTR:       internStr(platformClangVersion(flags)),
		BuildTypeUpperSTR: internStr(strings.ToUpper(buildType)),
	}

	p.CCArg = internStr(p.Tools.CC)
	p.CXXArg = internStr(p.Tools.CXX)
	p.TargetArg = internStr("--target=" + p.Triple)

	p.DebugInfoFlags = buildDebugInfoFlags(os, buildRelease, confCompressesDebug(fs))
	p.CompileCFlags = composeCompileCFlags(isa, buildRelease, p.DebugInfoFlags)

	return p
}

func platformClangVersion(flags map[string]string) string {
	if v := flags["CLANG_VER"]; v != "" {
		return v
	}

	return "20"
}

func platformBuildType(flags map[string]string) string {
	if v := flags["GG_BUILD_TYPE"]; v != "" {
		return strings.ToLower(v)
	}

	if v := flags["BUILD_TYPE"]; v != "" {
		return strings.ToLower(v)
	}

	return "debug"
}

func isReleaseBuildType(buildType string) bool {
	switch buildType {
	case "release", "relwithdebinfo", "minsizerel", "profile", "gprof":
		return true
	}

	return strings.HasSuffix(buildType, "-release")
}

func platformBuildSanitized(flags map[string]string) bool {
	sanitizer := strings.ToLower(flags["SANITIZER_TYPE"])

	return sanitizer != "" && sanitizer != "no" && sanitizer != "false" && sanitizer != "0"
}

func parseCompilerFlags(s string) []string {
	if s == "" {
		return nil
	}

	var out []string
	var b strings.Builder
	var quote byte
	escaped := false

	flush := func() {
		if b.Len() == 0 {
			return
		}

		out = append(out, b.String())
		b.Reset()
	}

	for i := 0; i < len(s); i++ {
		ch := s[i]

		if escaped {
			b.WriteByte(ch)
			escaped = false

			continue
		}

		if ch == '\\' {
			escaped = true

			continue
		}

		if quote != 0 {
			if ch == quote {
				quote = 0
			} else {
				b.WriteByte(ch)
			}

			continue
		}

		switch ch {
		case '\t', '\n', '\r', ' ':
			flush()
		case '\'', '"':
			quote = ch
		default:
			b.WriteByte(ch)
		}
	}

	if escaped {
		b.WriteByte('\\')
	}

	flush()

	return out
}

func marchFor(isa ISA) string {
	switch isa {
	case ISAAArch64:
		return "armv8-a"
	default:
		return ""
	}
}

func (p *Platform) MultiarchLibPath() string {
	path := "$OS_SDK_ROOT_RESOURCE_GLOBAL/usr/lib/" + p.Triple

	if p.UsesResourceClang() {
		return p.resourceClangRoot() + "/lib:" + path
	}

	return path
}

func (p *Platform) ToolEnv() EnvVars {
	env := EnvVars{
		{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"},
		{Name: "DYLD_LIBRARY_PATH", Value: p.MultiarchLibPath()},
	}

	if p.UsesResourceClang() {
		env = append(env,
			EnvVar{Name: "CPATH"},
			EnvVar{Name: "LIBRARY_PATH"},
			EnvVar{Name: "SDKROOT"},
		)
	}

	return env
}

func (p *Platform) LinkerSelectionGDBIndexFlags() []string {
	if !p.UsesResourceLLD() {
		return nil
	}

	return []string{"-Wl,--gdb-index"}
}

func (p *Platform) LinkerSelectionTailFlags() []string {
	if !p.UsesResourceLLD() {
		return nil
	}

	flags := []string{
		"-fuse-ld=lld",
		"--ld-path=" + p.Tools.LLD,
		"-Wl,--no-rosegment",
		"-Wl,--build-id=sha1",
	}

	return flags
}

func (p *Platform) LinkerSelectionNoPieFlags() []string {
	if !p.UsesResourceLLD() || p.PIC {
		return nil
	}

	return []string{"-Wl,-no-pie"}
}

func (p *Platform) ObjectSuffix() string {
	if p.PIC {
		return ".pic.o"
	}

	return ".o"
}

func (p *Platform) ArchiverArgs() (string, string, string) {
	if p.UsesResourceClang() {
		return p.Tools.AR, "LLVM_AR", "gnu"
	}

	return "ar", "GNU_AR", "None"
}

func (p *Platform) UsesResourceClang() bool {
	return strings.HasPrefix(p.Tools.CC, "$(") || strings.HasPrefix(p.Tools.CXX, "$(") || strings.HasPrefix(p.Tools.AR, "$(")
}

func (p *Platform) UsesResourceLLD() bool {
	return strings.HasPrefix(p.Tools.LLD, "$(")
}

func (p *Platform) resourceClangRoot() string {
	for _, tool := range []struct {
		path   string
		suffix string
	}{
		{path: p.Tools.CC, suffix: "/bin/clang"},
		{path: p.Tools.CXX, suffix: "/bin/clang++"},
		{path: p.Tools.AR, suffix: "/bin/llvm-ar"},
	} {
		if strings.HasSuffix(tool.path, tool.suffix) {
			return strings.TrimSuffix(tool.path, tool.suffix)
		}
	}

	return resourcePatternRef(resourcePatternClangTool)
}

func ParsePlatformID(s string) (OS, ISA) {
	if !strings.HasPrefix(s, "default-") {
		ThrowFmt("ParsePlatformID: %q does not start with \"default-\"", s)
	}

	rest := s[len("default-"):]
	dash := strings.IndexByte(rest, '-')

	if dash < 0 {
		ThrowFmt("ParsePlatformID: %q lacks the <os>-<isa> separator", s)
	}

	return OS(rest[:dash]), ISA(rest[dash+1:])
}
