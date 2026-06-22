package main

import (
	"bytes"
	"strings"
)

var (
	// wrapccPython3STR / wrapccPyVFS / wrapccArgSrcFile / wrapccArgEnd back the
	// compile-wrapper prefix (see wrapccPrefixFor). The python path is the constant
	// python resource binary, since the wrapper runs under the global python, not a
	// per-module peer.
	wrapccPython3STR = internStr("$(B)/resources/" + resourcePatternYMakePython3 + "/bin/python3")
	// wrapccPyChunk is the wrapper-script input chunk shared by every wrapped CC
	// node, appended to Node.Inputs by reference.
	wrapccPyChunk = []VFS{wrapccPyVFS}
)

// gzZstdRule is the verbatim conf line that appends the -gz=zstd debug-section-
// compression flag for non-release Linux targets — the sole config-level reason
// the flag appears in some repos and not others. confCompressesDebug detects it.
const gzZstdRule = "debug_info_flags.append('-gz=zstd')"

// confCompressesDebug reports whether the source repo's conf adds -gz=zstd to the
// debug-info flags. The conf is optional (a minimal or test tree may lack it), so
// absence means "no rule".
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
	// Flags is the interned config flag set: keys ENV, values STR. The raw CLI/conf
	// map is interned once here (internFlags) at construction — the single string
	// boundary — so nothing downstream keys env by string. Derivations inside
	// NewPlatform read the raw input map; everything past the platform uses ENV/STR.
	Flags map[ENV]STR

	PIC            bool
	BuildType      string
	BuildRelease   bool
	BuildSanitized bool
	// RagelOptimized is the `optimized` boolean (release && !sanitized): selects
	// -G2/-T0 for the R5 rlgen-cd mode and -CG2/-CT0 for the R6 ragel6 mode.
	RagelOptimized bool

	Triple string
	March  string

	// MultiarchLibPathSTR is the pre-interned DYLD_LIBRARY_PATH value, computed once
	// per platform so ToolEnv needn't rebuild and re-intern it per call.
	MultiarchLibPathSTR STR

	// TargetArg is the pre-interned --target=<triple> token, computed once per
	// platform so the per-CC-node compile line doesn't re-intern it.
	TargetArg STR

	// MarchArgs is the pre-interned -march=<March> arg vector (nil when March is
	// empty), computed once so compileFlagBundleFor doesn't re-intern it per node.
	MarchArgs []ARG

	CFlags   []ARG
	CXXFlags []ARG

	// DebugInfoFlags is the debug-info flag group (-g, optional -gz=zstd,
	// -fdebug-default-version=4, -ggnu-pubnames), derived once from the build type
	// and the source repo's conf. CompileCFlags is the full compile C flag vector
	// with that group spliced into its slot, composed once so the per-source line
	// needn't rebuild it. See buildDebugInfoFlags / composeCompileCFlags.
	DebugInfoFlags []ARG
	CompileCFlags  []ARG

	// CompressDebugSections adds both the compile `-gz=zstd` and the link
	// `-Wl,--compress-debug-sections=zstd`: not-release && linux, gated by the repo
	// conf carrying the rule (confCompressesDebug). The linker flag is spliced in
	// composeLDCmdLinkExe.
	CompressDebugSections bool

	SystemLibs       []STR
	LinkPreludeExtra []STR

	ClangVer string

	// ClangVerSTR / BuildTypeUpperSTR are the interned COMPILER_VERSION and
	// BUILD_TYPE env values, computed once so buildIfEnv binds them via SetStringID
	// instead of re-interning the same constant per module.
	ClangVerSTR       STR
	BuildTypeUpperSTR STR

	// WrapccHead / WrapccTail are the compile-wrapper tokens prepended before the
	// compiler in the CC compile line, computed once. The per-source file slots
	// between them: WrapccHead ++ [<src>] ++ WrapccTail ++ [<compiler> …]. Both nil
	// when the wrapper is disabled (OPENSOURCE=yes); see wrapccPrefixFor.
	WrapccHead []STR
	WrapccTail []STR

	// CCUsesResources is the fetched-resource list every CC node carries in
	// Node.Resources: the version-specific compiler, plus the python resource when
	// the wrapper is active. Computed once, shared read-only across CC nodes. The
	// sibling vectors cover the other per-platform resource sets the emitters attach
	// (python+compiler for script-driven tool nodes, the link set, bare compiler for AS).
	CCUsesResources   []STR
	UsesPython3Clang  []STR
	UsesLinkResources []STR
	UsesClangOnly     []STR

	// ToolEnvVars is the shared tool environment every CC/codegen node carries
	// (read-only; toolEnv() hands it out by reference).
	ToolEnvVars EnvVars

	// CCHead is the pre-built [--target=<triple>, -march…, sysroot args] span
	// opening every CC compile (after the compiler token) — referenced as a chunk,
	// never copied.
	CCHead []STR

	// SysrootArgs is the SDK sysroot + tool-bin compile prefix contributed right
	// after --target in every CC/AS compile line: [--sysroot=<root>, -B<root>/usr/bin],
	// where <root> is the fetched SDK-root resource. MUSL pins --sysroot=/nowhere;
	// os_sdk=local and non-Linux targets fall back to the bare host -B/usr/bin.
	// Computed once, shared read-only across CC/AS nodes.
	SysrootArgs []STR

	// UsesSDKRoot is true when SysrootArgs reference the fetched SDK-root resource
	// (Linux, non-opensource, os_sdk != local) — the gate for declaring that
	// resource on the nodes that embed the sysroot.
	UsesSDKRoot bool
}

// sysrootArgsFor builds Platform.SysrootArgs (see the field doc). Non-Linux /
// os_sdk=local keep the bare host -B/usr/bin that needs no fetched SDK.
// platformUsesSDKRoot reports whether this platform compiles against the fetched
// SDK sysroot. Opensource and os_sdk=local fall back to the bare host prefix, and
// only Linux has an SDK — those carry no SDK-root resource.
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

// wrapccPrefixFor returns the compile-wrapper head/tail token slices prepended
// before the compiler. Head = [python, wrapper-script, --source-file]; the
// per-source file is spliced between head and tail; tail = [--source-root, $(S),
// --build-root, $(B), --wrapcc-end].
//
// Disabled (returns nil, nil) for opensource builds (OPENSOURCE=yes). The other
// upstream disable conditions are not modelled (unset in every build here).
func wrapccPrefixFor(flags map[string]string) (head, tail []STR) {
	if flags["OPENSOURCE"] == "yes" {
		return nil, nil
	}

	head = []STR{wrapccPython3STR, wrapccPyVFS.str(), wrapccArgSrcFile.str()}
	tail = []STR{argSourceRoot.str(), strS, argBuildRoot.str(), strB, wrapccArgEnd.str()}

	return head, tail
}

// internFlags interns the raw CLI/conf flag map into the ENV/STR form stored on
// Platform.Flags — the one place env-flag strings are interned.
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

	// Base _C_SYSTEM_LIBRARIES. The trailing `-lm` is NOT part of the base set — it
	// is appended per link module in composeProgramLinkTrailer (the libm-not-arcadia
	// arm).
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

	// A node that puts the SDK sysroot on its command line must also depend on that
	// FETCH, so the resource is materialized before it runs. The sysroot rides on
	// CCHead (CC) and SysrootArgs (AS/LD/DLL), whose kinds draw Resources from these
	// three lists — so adding the SDK-root resource here, under the same gate as the
	// sysroot, wires the dependency exactly where the command needs it.
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

// The toolchain is always the resource compiler/linker, reached via PEERDIR; there
// is no non-resource compiler path. The compiler root in these executor-env /
// linker-selection helpers is the bare $(CLANG) resource pattern (the same form the
// executor mounts).
func (p *Platform) multiarchLibPath(opensource bool) string {
	// The SDK lib dir form is contour-dependent: opensource carries the raw
	// $OS_SDK_ROOT_RESOURCE_GLOBAL macro verbatim, while the internal contour
	// resolves it to the fetched SDK-root resource — the global is resolved only
	// where the resource is declared.
	sdkLib := "$OS_SDK_ROOT_RESOURCE_GLOBAL/usr/lib/" + p.Triple

	if !opensource {
		sdkLib = "$(B)/resources/" + resourcePatternOSSDKRoot + "/usr/lib/" + p.Triple
	}

	return "$(B)/resources/" + resourcePatternClangTool + p.ClangVer + "/lib:" + sdkLib
}

// toolEnv returns the per-platform tool environment — precomputed once
// (ToolEnvVars), attached to every CC/codegen node. Nodes never mutate their Env.
func (p *Platform) toolEnv() EnvVars {
	return p.ToolEnvVars
}

func (p *Platform) linkerSelectionGDBIndexFlags() []string {
	return []string{"-Wl,--gdb-index"}
}

func (p *Platform) linkerSelectionTailFlags() []string {
	// The lld linker flags (-fuse-ld=lld, --ld-path, -Wl,--no-rosegment,
	// -Wl,--build-id=sha1) now come from the implicit toolchain peer's propagated
	// LDFLAGS_GLOBAL, not from the Platform.
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
