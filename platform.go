package main

import (
	"slices"
	"strings"
	"sync"
)

var (
	wrapccPython3STR = internV("$(B)/resources/", resourcePatternYMakePython3, "/bin/python3")
	wrapccPyChunk    = []VFS{wrapccPyVFS}
)

type PlatformConf struct {
	jdkConfig           *ConfFile
	GoStdVersion        string
	Linker              *LinkerConf
	Parsers             *IncludeParserManager
	YqlStableABI        bool
	ProtoHeaderEpilogue bool
}

func readPlatformConf(fs FS, parsers *IncludeParserManager) PlatformConf {
	return PlatformConf{jdkConfig: readJdkConfig(parsers), GoStdVersion: confGoStdVersion(fs), Linker: readLinkerConf(parsers), Parsers: parsers, YqlStableABI: confYqlStableABI(fs), ProtoHeaderEpilogue: confProtoHeaderEpilogue(fs)}
}

func confProtoHeaderEpilogue(fs FS) bool {
	const conf = "build/conf/proto.conf"

	return fs.isFile(srcRootRel, conf) && strings.Contains(string(concatChunks(fs.read(conf))), "SET(PROTOC_TRANSITIVE_HEADERS $_CPP_PROTO_HEADERS_TRANSITIVE)")
}

type Platform struct {
	jdkConfig             *ConfFile
	YqlStableABI          bool
	ProtoHeaderEpilogue   bool
	picVariant            *Platform
	picOnce               sync.Once
	picMake               func() *Platform
	OS                    OS
	ISA                   ISA
	Target                PlatformID
	Flags                 map[ENV]STR
	PIC                   bool
	BuildType             string
	BuildRelease          bool
	BuildSanitized        bool
	RlgenFlags            []ANY
	Ragel6Flags           []ANY
	Triple                string
	March                 string
	MultiarchLibPathSTR   STR
	TargetArg             STR
	MarchArgs             []ANY
	CFlags                []ANY
	CXXFlags              []ANY
	DebugInfoFlags        []ANY
	CompileCFlags         []ANY
	CompressDebugSections bool
	BuildInfoKV           KV
	Linker                LinkerFlags
	Sysincl               []string
	confMacros            map[STR]*ConfMacro
	peerPolicies          *PeerPolicies
	LinkPreludeExtra      []ANY
	ClangVer              string
	ClangVerSTR           STR
	BuildTypeUpperSTR     STR
	WrapccHead            []ANY
	WrapUnshare           bool
	WrapccTail            []ANY
	CCUsesResources       []STR
	UsesPython3Clang      []STR
	UsesLinkResources     []STR
	UsesClangOnly         []STR
	ToolEnvVars           EnvVars
	CCHead                []ANY
	SysrootArgs           []ANY
	UsesSDKRoot           bool
	ifEnv                 Environment
	Defines               []ANY
	NoLibcBlock           []ANY
	Go                    *GoStd
}

func platformUsesSDKRoot(os OS, flags map[string]string) bool {
	return os == OSLinux && flags["OS_SDK"] != "local" && flags["OPENSOURCE"] != "yes"
}

func sysrootArgsFor(os OS, flags map[string]string) []ANY {
	if !platformUsesSDKRoot(os, flags) {
		return []ANY{argDashBBin.any()}
	}

	sdkRoot := "$(B)/resources/" + resourcePatternOSSDKRoot
	sysroot := "--sysroot=" + sdkRoot

	if flags["MUSL"] == "yes" {
		sysroot = "--sysroot=/nowhere"
	}

	return []ANY{internStr(sysroot).any(), internV("-B", sdkRoot, "/usr/bin").any()}
}

func wrapccPrefixFor(flags map[string]string) (head, tail []ANY) {
	if flags["OPENSOURCE"] == "yes" || flags["RAW_COMPILE_CPP_CMD"] == "yes" || flags["CPP_ANALYSIS_ARGS"] != "" {
		return nil, nil
	}

	head = []ANY{wrapccPython3STR.any(), wrapccPyVFS.any(), wrapccArgSrcFile.any()}
	tail = []ANY{argSourceRoot.any(), strS.any(), argBuildRoot.any(), strB.any(), wrapccArgEnd.any()}

	return head, tail
}

func internFlags(flags map[string]string) map[ENV]STR {
	out := make(map[ENV]STR, len(flags))

	for k, v := range flags {
		out[internEnv(k)] = internStr(v)
	}

	return out
}

func newPlatform(os OS, isa ISA, buildType string, tc *Toolchain, flags map[string]string, root *ConfFile, pc PlatformConf, cflagsEnv, cxxflagsEnv string) *Platform {
	rebaseToolchainRoot(flags, tc)

	clangVer := flags["COMPILER_VERSION"]

	if clangVer == "" {
		throwFmt("ymake conf: %s printed no COMPILER_VERSION", ymakeConfScript)
	}

	var linkPreludeExtra []string

	if flags["MUSL"] != "yes" {
		linkPreludeExtra = []string{"-ldl", "-lrt"}
	}

	p := &Platform{
		jdkConfig:             pc.jdkConfig,
		YqlStableABI:          pc.YqlStableABI,
		ProtoHeaderEpilogue:   pc.ProtoHeaderEpilogue,
		OS:                    os,
		ISA:                   isa,
		Target:                makePlatformID(os, isa),
		Flags:                 internFlags(flags),
		PIC:                   flags["PIC"] == "yes",
		BuildType:             strings.ToLower(buildType),
		BuildRelease:          flags["_BUILD_RELEASE"] == "yes",
		BuildSanitized:        flags["_BUILD_SANITIZED"] == "yes",
		RlgenFlags:            internAnys(splitShellWords(flags["RLGEN_FLAGS"])),
		Ragel6Flags:           internAnys(splitShellWords(flags["RAGEL6_FLAGS"])),
		Triple:                string(isa) + "-" + string(os) + "-gnu",
		March:                 marchFor(isa),
		CFlags:                internAnys(splitShellWords(cflagsEnv)),
		CXXFlags:              internAnys(splitShellWords(cxxflagsEnv)),
		LinkPreludeExtra:      internAnys(linkPreludeExtra),
		ClangVer:              clangVer,
		ClangVerSTR:           internStr(clangVer),
		CompressDebugSections: slices.Contains(splitShellWords(flags["_LD_FLAGS"]), argWlCompressDebugSectionsZstd.string()),
		BuildInfoKV: KV{
			P:            pkBI,
			PC:           pcYellow,
			ShowOut:      true,
			DisableCache: strings.Contains(flags["SVN_DEPENDS_CACHE__NO_UID__"], `kv:"disable_cache"`),
		},
	}

	p.TargetArg = internV("--target=", p.Triple)
	p.MultiarchLibPathSTR = internStr(p.multiarchLibPath(flags["OPENSOURCE"] == "yes"))
	p.WrapccHead, p.WrapccTail = wrapccPrefixFor(flags)
	p.WrapUnshare = len(p.WrapccHead) > 0 && (flags["ENABLE_UNSHARE_WRAPPER"] != "" && flags["ENABLE_UNSHARE_WRAPPER"] != "no" || flags["SDC_PLATFORM"] != "" && flags["SDC_PLATFORM"] != "no")

	clangRes := internV(resourcePatternClangTool, p.ClangVer)

	p.CCUsesResources = []STR{clangRes}

	if len(p.WrapccHead) > 0 {
		p.CCUsesResources = append(p.CCUsesResources, strYMakePython3Name)
	}

	if p.WrapUnshare {
		p.CCUsesResources = append(p.CCUsesResources, strUnshareWrapper)
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
		p.MarchArgs = []ANY{internArg("-march=" + p.March).any()}
	}

	p.CCHead = append(appendAnyLists([]ANY{p.TargetArg.any()}, p.MarchArgs), p.SysrootArgs...)

	p.ToolEnvVars = EnvVars{
		{Name: envARCADIA_ROOT_DISTBUILD, Value: strS.any()},
		{Name: envDYLD_LIBRARY_PATH, Value: p.MultiarchLibPathSTR.any()},
		{Name: envCPATH, Value: strEmpty.any()},
		{Name: envLIBRARY_PATH, Value: strEmpty.any()},
		{Name: envSDKROOT, Value: strEmpty.any()},
	}

	goStdVersion, ok := flags["GOSTD_VERSION"]

	if !ok {
		goStdVersion = pc.GoStdVersion
	}

	p.Go = newGoStd(goStdVersion)
	p.ifEnv = buildPlatformIfEnv(p)
	p.updateJdkConfig(p.ifEnv, envJdkVersion, nil)
	p.updateJdkConfig(p.ifEnv, envJdkRealVersion, nil)
	p.Linker = pc.Linker.flags(p)
	p.Sysincl, p.confMacros = sysinclFiles(pc.Parsers, root, p.ifEnv)
	p.peerPolicies = pc.Parsers.peerPolicies(strings.Fields(expandConfScalar(p.ifEnv.string(envPeerdirsRulesPath), p.ifEnv)))
	p.BuildTypeUpperSTR = internStr(p.ifEnv.string(envBUILD_TYPE))
	p.DebugInfoFlags = debugInfoFlags(p.ifEnv, flags)
	p.CompileCFlags = composeCompileCFlags(p.ifEnv, flags, p.DebugInfoFlags)
	p.NoLibcBlock = noLibcBlock(p)

	bundle := compileFlagBundleFor(p)

	p.Defines = bundle.Defines

	if ownershipOn {
		registerPlatformOwned(p)
	}

	return p
}

func rebaseToolchainRoot(flags map[string]string, tc *Toolchain) {
	if tc.ToolVar == "" {
		return
	}

	marker := "$(" + tc.ToolVar + ")"
	mount := "$(B)/resources/" + tc.MatchRoot + flags["COMPILER_VERSION"]

	for k, v := range flags {
		if strings.Contains(v, marker) {
			flags[k] = strings.ReplaceAll(v, marker, mount)
		}
	}
}

func buildPlatformIfEnv(p *Platform) Environment {
	env := DefaultIfEnv.clone()

	for k, v := range p.Flags {
		env.setFromStringID(k, v)
	}

	initializePerlEnv(env)

	if !env.hasBindingID(envJdkRealVersion) {
		env.setString(envJdkRealVersion, "17")
	}

	if !env.bool(envArcadiaPythonUnicodeSize) {
		width := "4"

		if env.bool(envOS_WINDOWS) {
			width = "2"
		}

		env.setString(envArcadiaPythonUnicodeSize, width)
	}

	for name, value := range javaConfGlobals {
		if !env.hasBindingID(internEnv(name)) {
			env.setString(internEnv(name), value)
		}
	}

	for _, compiler := range []struct{ variable, binary string }{{"C_COMPILER", "clang"}, {"CXX_COMPILER", "clang++"}} {
		variable := internEnv(compiler.variable)

		if !env.hasBindingID(variable) {
			env.setVFS(variable, build("resources/", resourcePatternClangTool, p.ClangVer, "/bin/", compiler.binary))
		}
	}

	if !env.hasBindingID(envPicNoPie) {
		env.setBool(envPicNoPie, env.bool(envAutocheck))
	}

	if !env.hasBindingID(envGOSTD_VERSION) {
		env.setString(envGOSTD_VERSION, p.Go.Version)
	}

	env.setString(envGOSTD, p.Go.Prefix)

	if !env.hasBindingID(envCGO_ENABLED) {
		env.setBool(envCGO_ENABLED, true)
	}

	if sanitizer := env.string(envSANITIZER_TYPE); env.bool(envOS_WINDOWS) || env.bool(envOS_EMSCRIPTEN) ||
		sanitizer != "" && sanitizer != "no" && sanitizer != "address" && sanitizer != "memory" {
		env.setBool(envCGO_ENABLED, false)
	}

	if env.bool(envOPENSOURCE) || env.string(envOPENSOURCE_PROJECT) == "ymake" || env.string(envOPENSOURCE_PROJECT) == "ya" {
		env.setBool(envYA_OPENSOURCE, true)
	}

	if env.bool(envOPENSOURCE) {
		env.setBool(envCATBOOST_OPENSOURCE, true)
		env.setBool(envValidateData, false)

		env.setString(env_USE_AIO, "dynamic")
		env.setString(env_USE_ICONV, "dynamic")
		env.setString(env_USE_IDN, "dynamic")
	}

	if env.string(envHAVE_MKL) == "" {
		env.setBool(envHAVE_MKL, env.bool(envOS_LINUX) && env.bool(envARCH_X86_64) && env.string(envSANITIZER_TYPE) == "")
	}

	if env.bool(envOPENSOURCE) {
		env.setBool(envHAVE_MKL, false)
	}

	if !env.hasBindingID(envOBJCOPY_TOOL) {
		if vendor := env.string(envOBJCOPY_TOOL_VENDOR); vendor != "" {
			env.setString(envOBJCOPY_TOOL, vendor)
		} else {
			env.setString(envOBJCOPY_TOOL, "${LLVM_TOOLS_ROOT}/bin/llvm-objcopy")
		}
	}

	env.setString(envARCADIA_ROOT, "$(S)")
	env.setString(envARCADIA_BUILD_ROOT, "$(B)")

	if !env.hasBindingID(envUSE_ARCADIA_COMPILER_RUNTIME) {
		env.setBool(envUSE_ARCADIA_COMPILER_RUNTIME, !(env.bool(envLOCAL) && env.bool(envXCODE)))
	}

	if (env.bool(envARCH_X86_64) || env.bool(envARCH_I386)) && !env.bool(envDISABLE_INSTRUCTION_SETS) &&
		(env.bool(envCLANG) || env.bool(envCLANG_CL) || env.bool(envGCC)) {
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

	if (!env.bool(envARCH_X86_64) && !env.bool(envARCH_I386)) || env.string(envDISABLE_INSTRUCTION_SETS) == "yes" {
		env.setStringID(envUSE_SSE4, strNo)
		env.setStringID(envCpuCheck, strNo)
	}

	if env.bool(envARCH_I386) && env.bool(envOS_ANDROID) || env.string(envNosse) == "yes" {
		env.setStringID(envUSE_SSE4, strNo)
	}

	return env
}

func debugInfoFlags(env Environment, flags map[string]string) []ANY {
	switch {
	case env.string(envNO_DEBUGINFO) == "yes":
		return nil
	case env.bool(envCLANG) && env.string(envDEBUGINFO_LINES_ONLY) == "yes":
		return []ANY{argGlineTablesOnly.any()}
	}

	return internAnys(splitShellWords(flags["_DEBUG_INFO_FLAGS"]))
}

func pythonNoStrip(env Environment) bool {
	buildType := env.string(envBUILD_TYPE)
	sanitizer := env.string(envSANITIZER_TYPE)

	return buildType == "DEBUG" || buildType == "FASTDEBUG" || sanitizer != "" && sanitizer != "no"
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

func (p *Platform) pic() *Platform {
	if p.PIC {
		return p
	}

	p.picOnce.Do(func() {
		p.picVariant = p.picMake()
	})

	return p.picVariant
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

func registerPlatformOwned(p *Platform) {
	registerOwnedSlice(p.RlgenFlags)
	registerOwnedSlice(p.Ragel6Flags)
	registerOwnedSlice(p.MarchArgs)
	registerOwnedSlice(p.CFlags)
	registerOwnedSlice(p.CXXFlags)
	registerOwnedSlice(p.DebugInfoFlags)
	registerOwnedSlice(p.CompileCFlags)
	p.Linker.registerOwned()
	registerOwnedSlice(p.LinkPreludeExtra)
	registerOwnedSlice(p.WrapccHead)
	registerOwnedSlice(p.WrapccTail)
	registerOwnedSlice(p.CCUsesResources)
	registerOwnedSlice(p.UsesPython3Clang)
	registerOwnedSlice(p.UsesLinkResources)
	registerOwnedSlice(p.UsesClangOnly)
	registerOwnedSlice(p.ToolEnvVars)
	registerOwnedSlice(p.CCHead)
	registerOwnedSlice(p.SysrootArgs)
	registerOwnedSlice(p.Defines)
	registerOwnedSlice(p.NoLibcBlock)
}
