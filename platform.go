package main

import (
	"bytes"
	"strings"
)

var (
	wrapccPython3STR = internV("$(B)/resources/", resourcePatternYMakePython3, "/bin/python3")
	wrapccPyChunk    = []VFS{wrapccPyVFS}
)

const gzZstdRule = "debug_info_flags.append('-gz=zstd')"

func confCompressesDebug(fs FS) bool {
	if !fs.isFile(srcRootVFS, "build/ymake_conf.py") {
		return false
	}

	return bytes.Contains(fs.read("build/ymake_conf.py"), []byte(gzZstdRule))
}

type Platform struct {
	OS                    OS
	ISA                   ISA
	Target                PlatformID
	Flags                 map[ENV]STR
	PIC                   bool
	BuildType             string
	BuildRelease          bool
	BuildSanitized        bool
	RagelOptimized        bool
	Triple                string
	March                 string
	MultiarchLibPathSTR   STR
	TargetArg             STR
	MarchArgs             []ARG
	CFlags                []ARG
	CXXFlags              []ARG
	DebugInfoFlags        []ARG
	CompileCFlags         []ARG
	CompressDebugSections bool
	SystemLibs            []STR
	LinkPreludeExtra      []STR
	ClangVer              string
	ClangVerSTR           STR
	BuildTypeUpperSTR     STR
	WrapccHead            []STR
	WrapccTail            []STR
	CCUsesResources       []STR
	UsesPython3Clang      []STR
	UsesLinkResources     []STR
	UsesClangOnly         []STR
	ToolEnvVars           EnvVars
	CCHead                []STR
	SysrootArgs           []STR
	UsesSDKRoot           bool
	ifEnv                 Environment
	CompileCFlagsStr      []STR
	DefinesStr            []STR
	NoLibcBlockStr        []STR
}

func platformUsesSDKRoot(os OS, flags map[string]string) bool {
	return os == OSLinux && flags["OS_SDK"] != "local" && flags["OPENSOURCE"] != "yes"
}

func sysrootArgsFor(os OS, flags map[string]string) []STR {
	if !platformUsesSDKRoot(os, flags) {
		return []STR{argDashBBin}
	}

	sdkRoot := "$(B)/resources/" + resourcePatternOSSDKRoot
	sysroot := "--sysroot=" + sdkRoot

	if flags["MUSL"] == "yes" {
		sysroot = "--sysroot=/nowhere"
	}

	return []STR{internStr(sysroot), internV("-B", sdkRoot, "/usr/bin")}
}

func wrapccPrefixFor(flags map[string]string) (head, tail []STR) {
	if flags["OPENSOURCE"] == "yes" {
		return nil, nil
	}

	head = []STR{wrapccPython3STR, wrapccPyVFS.str(), wrapccArgSrcFile.str()}
	tail = []STR{argSourceRoot.str(), strS, argBuildRoot.str(), strB, wrapccArgEnd.str()}

	return head, tail
}

func internFlags(flags map[string]string) map[ENV]STR {
	out := make(map[ENV]STR, len(flags))

	for k, v := range flags {
		out[internEnv(k)] = internStr(v)
	}

	return out
}

func newPlatform(fs FS, os OS, isa ISA, flags map[string]string, cflagsEnv, cxxflagsEnv string) *Platform {
	if flags == nil {
		flags = map[string]string{}
	}

	buildType := platformBuildType(flags)
	buildSanitized := platformBuildSanitized(flags)
	buildRelease := isReleaseBuildType(buildType)

	var systemLibs, linkPreludeExtra []string

	if flags["MUSL"] == "yes" {
		systemLibs = []string{"-nostdlib"}
	} else {
		linkPreludeExtra = []string{"-ldl", "-lrt"}
		systemLibs = []string{"-nodefaultlibs", "-lpthread", "-lc"}
	}

	p := &Platform{
		OS:                os,
		ISA:               isa,
		Target:            makePlatformID(os, isa),
		Flags:             internFlags(flags),
		PIC:               flags["PIC"] == "yes",
		BuildType:         buildType,
		BuildRelease:      buildRelease,
		BuildSanitized:    buildSanitized,
		RagelOptimized:    buildRelease && !buildSanitized,
		Triple:            string(isa) + "-" + string(os) + "-gnu",
		March:             marchFor(isa),
		CFlags:            internArgs(parseCompilerFlags(cflagsEnv)),
		CXXFlags:          internArgs(parseCompilerFlags(cxxflagsEnv)),
		SystemLibs:        internStrs(systemLibs),
		LinkPreludeExtra:  internStrs(linkPreludeExtra),
		ClangVer:          platformClangVersion(flags),
		ClangVerSTR:       internStr(platformClangVersion(flags)),
		BuildTypeUpperSTR: internStr(strings.ToUpper(buildType)),
	}

	p.TargetArg = internV("--target=", p.Triple)
	p.MultiarchLibPathSTR = internStr(p.multiarchLibPath(flags["OPENSOURCE"] == "yes"))
	p.WrapccHead, p.WrapccTail = wrapccPrefixFor(flags)

	clangRes := internV(resourcePatternClangTool, p.ClangVer)
	p.CCUsesResources = []STR{clangRes}

	if len(p.WrapccHead) > 0 {
		p.CCUsesResources = append(p.CCUsesResources, strYMakePython3Name)
	}

	p.UsesPython3Clang = []STR{strYMakePython3Name, clangRes}
	p.UsesLinkResources = []STR{clangRes, strLLDRootName, strYMakePython3Name}
	p.UsesClangOnly = []STR{clangRes}

	p.SysrootArgs = sysrootArgsFor(os, flags)
	p.UsesSDKRoot = platformUsesSDKRoot(os, flags)

	if p.UsesSDKRoot {
		osSDKRoot := internStr(resourcePatternOSSDKRoot)
		p.CCUsesResources = append(p.CCUsesResources, osSDKRoot)
		p.UsesClangOnly = append(p.UsesClangOnly, osSDKRoot)
		p.UsesLinkResources = append(p.UsesLinkResources, osSDKRoot)
	}

	if p.March != "" {
		p.MarchArgs = []ARG{internArg("-march=" + p.March)}
	}

	p.CCHead = append(appendArgStr([]STR{p.TargetArg}, p.MarchArgs), p.SysrootArgs...)

	p.ToolEnvVars = EnvVars{
		{Name: envARCADIA_ROOT_DISTBUILD, Value: strS},
		{Name: envDYLD_LIBRARY_PATH, Value: p.MultiarchLibPathSTR},
		{Name: envCPATH, Value: strEmpty},
		{Name: envLIBRARY_PATH, Value: strEmpty},
		{Name: envSDKROOT, Value: strEmpty},
	}

	compress := confCompressesDebug(fs)
	p.CompressDebugSections = compress && !buildRelease && os == OSLinux
	p.DebugInfoFlags = buildDebugInfoFlags(os, buildRelease, compress)
	p.CompileCFlags = composeCompileCFlags(isa, buildRelease, p.DebugInfoFlags)
	p.ifEnv = buildPlatformIfEnv(p)

	bundle := compileFlagBundleFor(p)
	p.CompileCFlagsStr = argSTRs(bundle.CFlags)
	p.DefinesStr = argSTRs(bundle.Defines)
	p.NoLibcBlockStr = argSTRs(bundle.NoLibcBlock)

	return p
}

func buildPlatformIfEnv(p *Platform) Environment {
	env := DefaultIfEnv.clone()

	for k, v := range p.Flags {
		env.setFromStringID(k, v)
	}

	if env.bool(envOPENSOURCE) || env.string(envOPENSOURCE_PROJECT) == "ymake" || env.string(envOPENSOURCE_PROJECT) == "ya" {
		env.setBool(envYA_OPENSOURCE, true)
	}

	if env.bool(envOPENSOURCE) {
		env.setBool(envCATBOOST_OPENSOURCE, true)

		env.setString(env_USE_AIO, "dynamic")
		env.setString(env_USE_ICONV, "dynamic")
		env.setString(env_USE_IDN, "dynamic")
	}

	switch p.ISA {
	case ISAX8664:
		env.setBool(envARCH_X86_64, true)
		env.setBool(envARCH_TYPE_64, true)
	case ISAAArch64:
		env.setBool(envARCH_AARCH64, true)
		env.setBool(envARCH_ARM64, true)
		env.setBool(envARCH_TYPE_64, true)
	}

	if !env.hasBindingID(envHAVE_MKL) {
		haveMkl := env.bool(envOS_LINUX) && env.bool(envARCH_X86_64) &&
			env.string(envSANITIZER_TYPE) == ""
		env.setBool(envHAVE_MKL, haveMkl)
	}

	if env.bool(envOPENSOURCE) {
		env.setBool(envHAVE_MKL, false)
	}

	if !env.hasBindingID(envHAVE_CUDA) {
		haveCuda := !env.bool(envMUSL) && env.bool(envOS_LINUX) && env.bool(envARCH_X86_64) &&
			env.string(envSANITIZER_TYPE) == ""
		env.setBool(envHAVE_CUDA, haveCuda)
	}

	if !env.hasBindingID(envCUDA_VERSION) {
		env.setString(envCUDA_VERSION, "12.9")
	}

	if !env.hasBindingID(envCUDA_ROOT) {
		env.setString(envCUDA_ROOT, "$(B)/resources/CUDA")
	}

	if !env.hasBindingID(envCUDA_ARCHITECTURES) {
		env.setString(envCUDA_ARCHITECTURES, cudaArchitectures129)
	}

	if !env.hasBindingID(envOBJCOPY_TOOL) {
		env.setString(envOBJCOPY_TOOL, "$(B)/resources/CLANG"+p.ClangVer+"/bin/llvm-objcopy")
	}

	env.setString(envARCADIA_ROOT, "$(S)")
	env.setString(envARCADIA_BUILD_ROOT, "$(B)")

	useRuntime := p.Flags[envUSE_ARCADIA_COMPILER_RUNTIME]
	env.setBool(envUSE_ARCADIA_COMPILER_RUNTIME, useRuntime != strNo)
	env.setStringID(envCOMPILER_VERSION, p.ClangVerSTR)
	env.setStringID(envBUILD_TYPE, p.BuildTypeUpperSTR)

	if (p.ISA == ISAX8664 || env.bool(envARCH_I386)) &&
		!env.bool(envDISABLE_INSTRUCTION_SETS) {
		env.setStringID(envSSE41_CFLAGS, strSSE41CFlags)
		env.setStringID(envSSE42_CFLAGS, strSSE42CFlags)
		env.setStringID(envPOPCNT_CFLAGS, strPopcntCFlags)
		env.setStringID(envCX16_FLAGS, strCX16CFlags)
		env.setStringID(envAVX_CFLAGS, strAVXCFlags)
		env.setStringID(envAVX2_CFLAGS, strAVX2CFlags)
		env.setStringID(envAVX512_CFLAGS, strAVX512CFlags)
		env.setStringID(envSSE_CFLAGS, strSSECFlags)
		env.setStringID(envSSE4_CFLAGS, strSSE4CFlags)
		env.setStringID(envAMX_CFLAGS, strAMXCFlags)
	}

	return env
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

func (p *Platform) multiarchLibPath(opensource bool) string {
	sdkLib := "$OS_SDK_ROOT_RESOURCE_GLOBAL/usr/lib/" + p.Triple

	if !opensource {
		sdkLib = "$(B)/resources/" + resourcePatternOSSDKRoot + "/usr/lib/" + p.Triple
	}

	return "$(B)/resources/" + resourcePatternClangTool + p.ClangVer + "/lib:" + sdkLib
}

func (p *Platform) toolEnv() EnvVars {
	return p.ToolEnvVars
}

func (p *Platform) linkerSelectionGDBIndexFlags() []string {
	return []string{"-Wl,--gdb-index"}
}

func (p *Platform) linkerSelectionTailFlags() []string {
	return nil
}

func (p *Platform) linkerSelectionNoPieFlags() []string {
	if p.PIC {
		return nil
	}

	return []string{"-Wl,-no-pie"}
}

func (p *Platform) objectSuffix() string {
	if p.PIC {
		return ".pic.o"
	}

	return ".o"
}

func parsePlatformID(s string) (OS, ISA) {
	if !strings.HasPrefix(s, "default-") {
		throwFmt("ParsePlatformID: %q does not start with \"default-\"", s)
	}

	rest := s[len("default-"):]
	dash := strings.IndexByte(rest, '-')

	if dash < 0 {
		throwFmt("ParsePlatformID: %q lacks the <os>-<isa> separator", s)
	}

	return OS(rest[:dash]), ISA(rest[dash+1:])
}
