package main

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
//     `targetP.Target` (which toolchain to use), `targetP.Flags`
//     (per-platform toggles like PIC), `targetP.Tags` (e.g. `["tool"]`),
//     and `targetP.IsHost` (the boolean that lands in `node.host_platform`).
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
	Target PlatformID        // arch identity surfaced as `node.platform`
	Flags  map[string]string // canonical per-platform toggles ("PIC"="yes", "MUSL"="yes", …)
	Tags   []string          // baseline tags every node on this platform carries (e.g. `["tool"]` on host)
	IsHost bool              // is this the host machine's platform? Set by CLI; surfaces as `node.host_platform`.

	// Shadow accessors — derived from Flags at construction time. Kept as
	// fields rather than methods to avoid the map lookup in tight loops
	// (every CC compose dispatch checks PIC; every musl-aware emit checks
	// LibcMusl). Treat as read-only post-construction.
	PIC      bool
	LibcMusl bool
}

// NewPlatform constructs a `*Platform` from an explicit (target,
// flags, tags, isHost) tuple and pre-fills the boolean shadows from
// the flags map. The caller owns `flags` and `tags`; NewPlatform takes
// references, so the caller must not mutate them after construction.
//
// Flag value convention: `"yes"` is truthy; any other value (including
// empty, missing, `"no"`) is falsy. Matches the ymake `--define` /
// `cliDefines` convention.
func NewPlatform(target PlatformID, flags map[string]string, tags []string, isHost bool) *Platform {
	if flags == nil {
		flags = map[string]string{}
	}
	if tags == nil {
		tags = []string{}
	}

	return &Platform{
		Target:   target,
		Flags:    flags,
		Tags:     tags,
		IsHost:   isHost,
		PIC:      flags["PIC"] == "yes",
		LibcMusl: flags["MUSL"] == "yes",
	}
}

// platformFor returns the `*Platform` matching `instance.Target` from
// the (host, target) pair on `c`. Helper for the migration period: as
// each emitter is refactored to take `(hostP, targetP)` explicitly, its
// caller resolves the right `*Platform` via this method. Throws if
// `instance.Target` is neither host nor target; in M2/M3 this is
// unreachable (ModuleInstance.Target is always one of the two CLI-
// constructed platforms).
func (c *genCtx) platformFor(instance ModuleInstance) *Platform {
	switch instance.Target {
	case c.host.Target:
		return c.host
	case c.target.Target:
		return c.target
	}

	ThrowFmt(
		"genCtx.platformFor: instance.Target=%q does not match host=%q or target=%q",
		instance.Target, c.host.Target, c.target.Target,
	)
	return nil
}

// defaultLinuxPlatforms returns the canonical M2/M3 (host, target) pair
// the existing CLI implicitly used pre-refactor: host = x86_64 PIC + the
// `"tool"` tag baseline (so every node emitted under a host sub-graph
// inherits it via `targetP.Tags`); target = aarch64 non-PIC, no
// baseline tags. The MUSL flag is propagated to BOTH platforms from
// `cliDefines["MUSL"]` because musl-vs-glibc applies to BOTH axes (the
// host walk through ragel6/bin still hits the musl-flavoured cc.go
// composer for asmlib/musl-deep peers).
func defaultLinuxPlatforms(cliDefines map[string]string) (host, target *Platform) {
	muslOn := cliDefines["MUSL"] == "yes"
	muslVal := "no"
	if muslOn {
		muslVal = "yes"
	}

	host = NewPlatform(
		PlatformDefaultLinuxX8664,
		map[string]string{
			"PIC":  "yes",
			"MUSL": muslVal,
		},
		[]string{"tool"},
		true,
	)
	target = NewPlatform(
		PlatformDefaultLinuxAArch64,
		map[string]string{
			"PIC":  "no",
			"MUSL": muslVal,
		},
		nil,
		false,
	)

	return host, target
}
