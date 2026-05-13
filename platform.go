package main

import "strings"

// platform.go — the `(host, target)` Platform pair the generator threads
// from CLI entry through every emitter.
//
// Architectural contract (PR-M3-platform-pair):
//
//   - The CLI entry point constructs exactly two `*Platform` values: one
//     for the build's host machine, one for the eventual target platform.
//   - The pair is propagated as `(host, target)` everywhere — never as a
//     single "current platform" with a `if isHost { ... }` branch.
//   - When the walker spawns a tool sub-graph it calls `(host, host)` —
//     the second slot becomes host, so the recursive emission renders
//     for the host's arch.
//   - Renderers (emit*.go) **never** ask "am I a host build?". They read
//     `instance.Platform.Target` (which toolchain to use), `instance.Platform.Flags`
//     (per-platform toggles like PIC), `instance.Platform.Tags` (e.g. `["tool"]`),
//     and `instance.Platform.IsHost` (the boolean that lands in `node.host_platform`).
//   - The renderer-side knowledge that something is "host" is restricted
//     to one place: `IsHost` is set when the top-level CLI constructs the
//     host Platform; every recursion just plumbs the pointer.
//
// `Flags` is the canonical authoritative source (string `A=B` pairs);
// the boolean shadows (`PIC`, `LibcMusl`, …) are read-only caches the
// CLI fills at construction time so hot-path dispatch (cc.go composer
// pick, ld.go trailer pick) avoids the map lookup. Any mutation must
// rewrite the map first and re-derive shadows; the struct is treated
// as immutable post-construction.
type Platform struct {
	OS     OS                // OS axis (linux / darwin / windows)
	ISA    ISA               // ISA axis (x86_64 / aarch64 / arm64)
	Target PlatformID        // = MakePlatformID(OS, ISA); surfaces as `node.platform`
	Flags  map[string]string // canonical per-platform toggles ("PIC"="yes", "MUSL"="yes", …)
	Tags   []string          // baseline tags every node on this platform carries (e.g. `["tool"]` on host)
	IsHost bool              // is this the host machine's platform? Set by CLI; surfaces as `node.host_platform`.

	// Tools is the absolute-path toolchain bound to this Platform.
	// Each emitter that materialises a tool invocation (python3 driver,
	// clang(++) compile, llvm-objcopy strip-debug, …) reads the path
	// off this struct rather than hard-coding it. Populated by
	// NewPlatform from Flags entries written by mine.go::commonFlags;
	// missing entries fall back to the reference-snapshot paths so
	// tests that construct platforms with nil/empty flags continue to
	// emit byte-exact cmd_args.
	Tools Toolchain

	// Shadow accessors — derived from Flags at construction time. Kept as
	// fields rather than methods to avoid the map lookup in tight loops
	// (every CC compose dispatch checks PIC; every libc-aware emit may
	// check LibcMusl). Treat as read-only post-construction.
	//
	// `LibcMusl` is the Platform-level libc selector: host and target
	// halves of the build can independently be musl or glibc, and a
	// Platform represents one of them. It is distinct from the
	// per-MODULE marker `ModuleInstance.Flags.LibcMusl`, which says
	// "this module is part of the contrib/libs/musl subtree". Emitters
	// must pick the one that matches the semantic they actually need
	// (per-platform toolchain selection vs per-module subtree membership)
	// — conflating them broke 64 nodes on M3 during this refactor.
	PIC      bool
	LibcMusl bool
}

// Toolchain captures the absolute paths the rule emitters need to
// embed in cmd_args. Populated from a Flags map keyed on ymake's
// `<TOOL>_TOOL` / `BUILD_PYTHON_BIN` convention (see mine.go::commonFlags).
// Empty fields are legal at construction time — emitters that consume
// a tool fail loudly if it was not mined, rather than silently embedding
// a stale snapshot path.
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

// NewPlatform constructs a `*Platform` from an explicit
// (os, isa, flags, tags, isHost) tuple and pre-fills the boolean
// shadows from the flags map. The caller owns `flags` and `tags`;
// NewPlatform takes references, so the caller must not mutate them
// after construction.
//
// Flag value convention: `"yes"` is truthy; any other value (including
// empty, missing, `"no"`) is falsy. Matches the ymake `--define` /
// `cliDefines` convention.
func NewPlatform(os OS, isa ISA, flags map[string]string, tags []string, isHost bool) *Platform {
	if flags == nil {
		flags = map[string]string{}
	}
	if tags == nil {
		tags = []string{}
	}

	return &Platform{
		OS:       os,
		ISA:      isa,
		Target:   MakePlatformID(os, isa),
		Flags:    flags,
		Tags:     tags,
		IsHost:   isHost,
		Tools:    toolchainFromFlags(flags),
		PIC:      flags["PIC"] == "yes",
		LibcMusl: flags["MUSL"] == "yes",
	}
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
