package main

import "strings"

// platform.go — the `(host, target)` Platform pair threaded from CLI entry
// through every emitter.
//
// CLI constructs exactly two *Platform values (host + target). Pair is
// always propagated as (host, target); when the walker spawns a tool
// sub-graph it calls (host, host). Renderers never ask "am I host?" — they
// read instance.Platform.{Target, Flags, Tags, IsHost}. IsHost is set once
// by the CLI; recursion just plumbs the pointer.
//
// Flags is the canonical source of truth; PIC/LibcMusl/… boolean shadows
// are read-only caches filled at construction so hot paths avoid the map
// lookup. Treat the struct as immutable post-construction.
type Platform struct {
	OS     OS                // OS axis (linux / darwin / windows)
	ISA    ISA               // ISA axis (x86_64 / aarch64 / arm64)
	Target PlatformID        // = MakePlatformID(OS, ISA); surfaces as `node.platform`
	Flags  map[string]string // canonical per-platform toggles ("PIC"="yes", "MUSL"="yes", …)
	Tags   []string          // baseline tags every node on this platform carries (e.g. `["tool"]` on host)
	IsHost bool              // is this the host machine's platform? Set by CLI; surfaces as `node.host_platform`.

	// Tools is the absolute-path toolchain bound to this Platform. Populated
	// by NewPlatform from Flags entries written by mine.go::commonFlags.
	Tools Toolchain

	// Shadow accessors derived from Flags at construction. Read-only.
	// LibcMusl is the Platform-level libc selector (host and target halves
	// can independently be musl/glibc). It is distinct from the per-MODULE
	// marker ModuleInstance.Flags.LibcMusl ("module belongs to
	// contrib/libs/musl"); conflating the two broke 64 nodes during the
	// platform-pair refactor.
	PIC             bool
	LibcMusl        bool
	BuildType       string
	BuildRelease    bool
	BuildSanitized  bool
	Ragel6Optimized bool

	// Triple is the clang `--target=<triple>` arg (e.g. `aarch64-linux-gnu`),
	// derived from `<isa>-<os>-gnu`. March is `-march=<arch>`; empty for
	// ISAs that communicate baseline via other flags (x86_64 uses
	// `-m64`/`-msseN` instead). When March=="" the arg is omitted entirely.
	Triple string
	March  string

	// Raw compiler flags supplied by the caller (usually CFLAGS /
	// CXXFLAGS from the environment) and parsed once at construction.
	// They are target-platform inputs; host platforms normally pass
	// empty strings so build tools stay byte-exact.
	CFlags   []string
	CXXFlags []string

	ClangVer string
}

// Toolchain captures absolute paths embedded in cmd_args. Populated from
// a Flags map keyed on ymake's `<TOOL>_TOOL` / `BUILD_PYTHON_BIN`
// convention (mine.go::commonFlags). Empty fields are legal; consumers
// fail loudly rather than silently embedding a stale snapshot path.
type Toolchain struct {
	Python3 string // BUILD_PYTHON_BIN
	CC      string // CLANG_TOOL       — C compile driver
	CXX     string // CLANG_pl_pl_TOOL — C++ compile driver
	Objcopy string // OBJCOPY_TOOL     — debug-section strip / resource embed
	AR      string // AR_TOOL          — archiver
	Strip   string // STRIP_TOOL
	LLD     string // LLD_TOOL         — linker
}

// toolchainFromFlags reads tool paths out of a Flags map. Missing
// entries land as empty strings; the consumer that actually needs the
// tool is responsible for the error path.
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

// NewPlatform constructs *Platform from an explicit (os, isa, flags, tags,
// isHost) tuple and pre-fills boolean shadows. cflagsEnv / cxxflagsEnv
// are shell-like flag strings parsed once into Platform.CFlags /
// Platform.CXXFlags. Caller must not mutate flags or tags after
// construction (NewPlatform retains references). Flag convention:
// "yes" is truthy; anything else (empty/missing/"no") is falsy.
func NewPlatform(os OS, isa ISA, flags map[string]string, tags []string, isHost bool, cflagsEnv, cxxflagsEnv string) *Platform {
	if flags == nil {
		flags = map[string]string{}
	}
	if tags == nil {
		tags = []string{}
	}
	buildType := platformBuildType(flags)
	buildSanitized := platformBuildSanitized(flags)
	buildRelease := isReleaseBuildType(buildType)

	return &Platform{
		OS:              os,
		ISA:             isa,
		Target:          MakePlatformID(os, isa),
		Flags:           flags,
		Tags:            tags,
		IsHost:          isHost,
		Tools:           toolchainFromFlags(flags),
		PIC:             flags["PIC"] == "yes",
		LibcMusl:        flags["MUSL"] == "yes",
		BuildType:       buildType,
		BuildRelease:    buildRelease,
		BuildSanitized:  buildSanitized,
		Ragel6Optimized: buildRelease && !buildSanitized,
		Triple:          string(isa) + "-" + string(os) + "-gnu",
		March:           marchFor(isa),
		CFlags:          parseCompilerFlags(cflagsEnv),
		CXXFlags:        parseCompilerFlags(cxxflagsEnv),
		ClangVer:        platformClangVersion(flags),
	}
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

// parseCompilerFlags splits a CFLAGS/CXXFLAGS-style string into argv
// tokens. It supports whitespace separation, single/double quotes, and
// backslash escaping. Quotes are not retained; backslash preserves the
// following byte verbatim.
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

// marchFor returns the `-march=<arg>` value for an ISA. Empty when the
// ISA's architectural baseline is communicated by other flags
// (x86_64 → `-m64` / `-msseN` bundles, not `-march=`).
func marchFor(isa ISA) string {
	switch isa {
	case ISAAArch64:
		return "armv8-a"
	default:
		return ""
	}
}

// MultiarchLibPath returns the `$OS_SDK_ROOT_RESOURCE_GLOBAL/usr/lib/<triple>`
// path the reference graph embeds in DYLD_LIBRARY_PATH on every CC /
// AR / LD / AS node. The path is the BUILD HOST's multiarch dir — the
// machine running the build — so emitters call this on the host
// Platform regardless of which axis the node belongs to.
func (p *Platform) MultiarchLibPath() string {
	path := "$OS_SDK_ROOT_RESOURCE_GLOBAL/usr/lib/" + p.Triple

	if p.UsesResourceClang() {
		return "$(CLANG)/lib:" + path
	}

	return path
}

func (p *Platform) ToolEnv() map[string]string {
	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
		"DYLD_LIBRARY_PATH":      p.MultiarchLibPath(),
	}

	if p.UsesResourceClang() {
		env["CPATH"] = ""
		env["LIBRARY_PATH"] = ""
		env["SDKROOT"] = ""
	}

	return env
}

func (p *Platform) WithLinkerSelectionFlags(trailer []string) []string {
	if !p.UsesResourceLLD() {
		return trailer
	}

	flags := []string{
		"-Wl,--gdb-index",
		"-fuse-ld=lld",
		"--ld-path=$(LLD_ROOT)/bin/ld.lld",
		"-Wl,--no-rosegment",
		"-Wl,--build-id=sha1",
	}

	if len(trailer) > 3 && trailer[2] == "-fPIC" && trailer[3] == "-fPIC" {
		out := make([]string, 0, len(trailer)+len(flags))
		out = append(out, trailer[:3]...)
		out = append(out, flags[0])
		out = append(out, trailer[3])
		out = append(out, flags[1:]...)
		out = append(out, trailer[4:]...)

		return out
	}

	if len(trailer) < 2 {
		return append(flags, trailer...)
	}

	out := make([]string, 0, len(trailer)+len(flags))
	out = append(out, trailer[:2]...)
	out = append(out, flags...)
	out = append(out, trailer[2:]...)

	if !p.PIC {
		out = append(out, "-Wl,-no-pie")
	}

	return out
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

// ParsePlatformID splits a "default-<os>-<isa>" string into its OS
// and ISA components. Throws on a malformed input; returns the
// recognised typed pair on success.
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
