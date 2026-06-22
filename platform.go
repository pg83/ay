package main

import (
	"bytes"
	"strings"
)

var (
	// wrapccPython3STR / wrapccPyVFS / wrapccArgSrcFile / wrapccArgEnd back the wrapcc.py
	// compile-wrapper prefix (see wrapccPrefixFor). The python path is the constant
	// YMAKE_PYTHON3 resource binary (identical to moduleToolchain.Python3), since upstream
	// invokes the wrapper via the global $YMAKE_PYTHON3, not a per-module peer.
	wrapccPython3STR = internStr("$(B)/resources/" + resourcePatternYMakePython3 + "/bin/python3")
	// wrapccPyChunk is the wrapper-script input chunk shared by every wrapped CC
	// node (Node.Inputs is chunked; this one is appended by reference).
	wrapccPyChunk = []VFS{wrapccPyVFS}
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
	if !fs.isFile(srcRootVFS, "build/ymake_conf.py") {
		return false
	}

	return bytes.Contains(fs.read("build/ymake_conf.py"), []byte(gzZstdRule))
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

	PIC             bool
	BuildType       string
	BuildRelease    bool
	BuildSanitized bool
	// RagelOptimized is upstream's single `optimized` boolean
	// (Ragel.set_default_flags): release && !sanitized. It selects -G2/-T0 for the
	// R5 rlgen-cd mode and -CG2/-CT0 for the R6 ragel6 mode.
	RagelOptimized bool

	Triple string
	March  string

	// MultiarchLibPathSTR is the pre-interned DYLD_LIBRARY_PATH value (the
	// MultiarchLibPath() string), computed once per platform so ToolEnv needn't
	// rebuild and re-intern it on every call.
	MultiarchLibPathSTR STR

	// TargetArg is the pre-interned --target=<triple> cmd-arg token (STR), computed
	// once per platform so the per-CC-node compile line doesn't re-intern it.
	TargetArg STR

	// MarchArgs is the pre-interned -march=<March> arg vector (nil when March is
	// empty), computed once per platform so compileFlagBundleFor doesn't re-intern
	// it per compile node.
	MarchArgs []ARG

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

	// CompressDebugSections mirrors the gnu_compiler.conf condition that adds both
	// the compile `-gz=zstd` and the link `-Wl,--compress-debug-sections=zstd`
	// (ymake_conf.py: `not build.is_release and target.is_linux`, gated by the
	// repo conf carrying the rule — confCompressesDebug). The linker flag is spliced
	// in composeLDCmdLinkExe.
	CompressDebugSections bool

	SystemLibs       []STR
	LinkPreludeExtra []STR

	ClangVer string

	// ClangVerSTR / BuildTypeUpperSTR are the interned forms of the COMPILER_VERSION
	// and BUILD_TYPE env values, computed once per platform so buildIfEnv binds them
	// via SetStringID instead of re-interning the same constant on every module.
	ClangVerSTR       STR
	BuildTypeUpperSTR STR

	// WrapccHead / WrapccTail are the wrapcc.py compile-wrapper tokens that upstream's
	// gnu_compiler.conf (_C_CPP_WRAPPER) prepends before the compiler in the CC compile
	// line, computed once per platform. The per-source file slots between them, so the
	// emitted prefix is: WrapccHead ++ [<src>] ++ WrapccTail ++ [<compiler> …]. Both nil
	// when the wrapper is disabled (OPENSOURCE=yes — i.e. the sg2–5 opensource contour);
	// see wrapccPrefixFor.
	WrapccHead []STR
	WrapccTail []STR

	// CCUsesResources is the fetched-resource list every CC node on this platform
	// carries in Node.Resources: the version-specific CLANG, plus YMAKE_PYTHON3 when
	// the wrapcc.py wrapper is active (it runs under that python). Computed once per
	// platform and shared read-only across CC nodes. The sibling vectors cover the
	// other per-platform resource sets the emitters attach (python3+clang for
	// script-driven tool nodes, the link set, bare clang for AS).
	CCUsesResources   []STR
	UsesPython3Clang  []STR
	UsesLinkResources []STR
	UsesClangOnly     []STR

	// ToolEnvVars is the shared tool environment every CC/codegen node on this
	// platform carries (read-only; toolEnv() hands it out by reference).
	ToolEnvVars EnvVars

	// CCHead is the pre-built [--target=<triple>, -march…, sysroot args] span
	// opening every CC compile on this platform (after the compiler token) —
	// referenced as a chunk by the emitters, never copied.
	CCHead []STR

	// SysrootArgs is the SDK sysroot + tool-bin compile prefix that upstream's
	// GnuToolchain.C_FLAGS_PLATFORM contributes right after --target in every CC/AS
	// compile line: [--sysroot=<root>, -B<root>/usr/bin], where <root> is the
	// OS_SDK_ROOT resource ($(B)/resources/OS_SDK_ROOT) declared by build/platform/
	// linux_sdk (ymake_conf.py setup_sdk / setup_tools). MUSL pins --sysroot=/nowhere;
	// os_sdk=local and non-Linux targets fall back to the bare host -B/usr/bin.
	// Computed once per platform, shared read-only across CC/AS nodes.
	SysrootArgs []STR

	// UsesSDKRoot is true when SysrootArgs reference the fetched OS_SDK_ROOT resource
	// (Linux, non-opensource, os_sdk != local) — the gate for peering build/platform/
	// linux_sdk and declaring OS_SDK_ROOT on the nodes that embed the sysroot.
	UsesSDKRoot bool
}

// sysrootArgsFor builds Platform.SysrootArgs (see the field doc). Modelled for the
// Linux gnu contour these graphs build; non-Linux / os_sdk=local keep the bare host
// -B/usr/bin that needs no fetched SDK.
// platformUsesSDKRoot reports whether this platform compiles against the fetched
// OS_SDK_ROOT sysroot. The opensource contour (sg2-5 references) and os_sdk=local fall
// back to the bare host prefix, and only Linux has an SDK — those carry no OS_SDK_ROOT.
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

	return []STR{internStr(sysroot), internStr("-B" + sdkRoot + "/usr/bin")}
}

// wrapccPrefixFor returns the wrapcc.py compile-wrapper head/tail token slices that
// upstream's build/conf/compilers/gnu_compiler.conf prepends before the compiler in
// _CPP_ARGS_NEW/_C_ARGS_NEW. Head = [python3, $(S)/build/scripts/wrapcc.py,
// --source-file]; the per-source file is spliced between head and tail; tail =
// [--source-root, $(S), --build-root, $(B), --wrapcc-end].
//
// The wrapper is disabled (returns nil, nil) for opensource builds (OPENSOURCE=yes) —
// matching the conf's `when ($CPP_ANALYSIS_ARGS || $OPENSOURCE == "yes" ||
// $RAW_COMPILE_CPP_CMD == "yes") { _C_CPP_WRAPPER= }`. CPP_ANALYSIS_ARGS and
// RAW_COMPILE_CPP_CMD are not modelled (unset in every build here).
func wrapccPrefixFor(flags map[string]string) (head, tail []STR) {
	if flags["OPENSOURCE"] == "yes" {
		return nil, nil
	}

	head = []STR{wrapccPython3STR, wrapccPyVFS.str(), wrapccArgSrcFile.str()}
	tail = []STR{argSourceRoot.str(), strS, argBuildRoot.str(), strB, wrapccArgEnd.str()}

	return head, tail
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

func newPlatform(fs FS, os OS, isa ISA, flags map[string]string, cflagsEnv, cxxflagsEnv string) *Platform {
	if flags == nil {
		flags = map[string]string{}
	}

	buildType := platformBuildType(flags)
	buildSanitized := platformBuildSanitized(flags)
	buildRelease := isReleaseBuildType(buildType)

	var systemLibs, linkPreludeExtra []string

	// Base _C_SYSTEM_LIBRARIES (build/conf/linkers/ld.conf:106-128). The trailing
	// `-lm` is NOT part of the base set — it is the COMMON_LINK_SETTINGS append in
	// the USE_ARCADIA_LIBM == "no" arm (ymake.core.conf:941-942), emitted per link
	// module in composeProgramLinkTrailer.
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

	p.TargetArg = internStr("--target=" + p.Triple)
	p.MultiarchLibPathSTR = internStr(p.multiarchLibPath(flags["OPENSOURCE"] == "yes"))
	p.WrapccHead, p.WrapccTail = wrapccPrefixFor(flags)

	clangRes := internStr(resourcePatternClangTool + p.ClangVer)
	p.CCUsesResources = []STR{clangRes}

	if len(p.WrapccHead) > 0 {
		p.CCUsesResources = append(p.CCUsesResources, strYMakePython3Name)
	}

	p.UsesPython3Clang = []STR{strYMakePython3Name, clangRes}
	p.UsesLinkResources = []STR{clangRes, strLLDRootName, strYMakePython3Name}
	p.UsesClangOnly = []STR{clangRes}

	p.SysrootArgs = sysrootArgsFor(os, flags)
	p.UsesSDKRoot = platformUsesSDKRoot(os, flags)

	// A node that puts the SDK sysroot (--sysroot/-B …/resources/OS_SDK_ROOT) on its
	// command line must also depend on that FETCH, so the resource is materialized
	// before it runs. The sysroot rides on CCHead (CC) and SysrootArgs (AS/LD/DLL);
	// those node kinds draw their Resources from exactly these three lists, so adding
	// OS_SDK_ROOT here — under the same gate as the sysroot resource — wires the
	// dependency precisely where the command needs it (and nowhere else).
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

// The toolchain is always the resource clang/lld (build/platform/clang, build/
// platform/lld), reached via PEERDIR; there is no non-resource compiler path. The
// clang root in these executor-env / linker-selection helpers is the bare $(CLANG)
// resource pattern (the same form the executor mounts).
func (p *Platform) multiarchLibPath(opensource bool) string {
	// The SDK lib dir form is contour-dependent: the opensource reference
	// graphs (sg2-5) carry the raw $OS_SDK_ROOT_RESOURCE_GLOBAL macro verbatim,
	// while the internal contour (sg6) resolves it to the OS_SDK_ROOT resource
	// ($(B)/resources/OS_SDK_ROOT, normalizing to $(OS_SDK_ROOT)) — upstream's
	// TOOLCHAIN_ENV resolves the global only where the resource is declared.
	sdkLib := "$OS_SDK_ROOT_RESOURCE_GLOBAL/usr/lib/" + p.Triple

	if !opensource {
		sdkLib = "$(B)/resources/" + resourcePatternOSSDKRoot + "/usr/lib/" + p.Triple
	}

	return "$(B)/resources/" + resourcePatternClangTool + p.ClangVer + "/lib:" + sdkLib
}

// toolEnv returns the per-platform tool environment — precomputed once
// (ToolEnvVars): emitters attach it to every CC/codegen node, and the content
// never varies within a platform. Nodes never mutate their Env.
func (p *Platform) toolEnv() EnvVars {
	return p.ToolEnvVars
}

func (p *Platform) linkerSelectionGDBIndexFlags() []string {
	return []string{"-Wl,--gdb-index"}
}

func (p *Platform) linkerSelectionTailFlags() []string {
	// The lld linker flags (-fuse-ld=lld, --ld-path, -Wl,--no-rosegment,
	// -Wl,--build-id=sha1) now come from build/platform/lld's propagated
	// LDFLAGS_GLOBAL via the implicit toolchain peer, not from the Platform.
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
