package main

// toolchain.go — platform descriptors for rule emission.
//
// PR-23 introduces the (Target, Host) pair so cross-platform recursion
// (D31) has a single source of truth for how to flip from a target
// instance to its host counterpart. The descriptor is still minimal —
// rule emitters only consume `ID` today, and the canonical bundles
// hardcode the rest of the toolchain (compiler path, sysroot,
// warning flags) in flags.go.
//
// `TargetCfg` survives as a back-compat alias so existing tests that
// use `EmitCC(TargetCfg, ...)` continue to compile until PR-25 sweeps
// every call site to pass a `ModuleInstance`. PR-25 retires
// `TargetCfg`; for PR-23 it is the M1-equivalent default.

// PlatformSpec captures the per-platform fields rule emitters
// occasionally branch on. PR-23 only consumes `ID` (it surfaces as
// `node.platform`); the remaining fields are forward-declared so M5's
// toolchain refactor can wire compiler invocation through them
// without rewriting this struct.
type PlatformSpec struct {
	ID     PlatformID
	Triple string
	March  string
	SDK    string
	PIC    bool
}

// PlatformConfig is the (Target, Host) pair the walker propagates
// through `genCtx.cfg`. `Target` seeds the initial Gen call; `Host`
// is what `(ModuleInstance).WithHost` flips to for host-tool
// recursion (D31).
type PlatformConfig struct {
	Target PlatformSpec
	Host   PlatformSpec
}

// DefaultLinuxConfig is the canonical M2 (target=aarch64, host=x86_64)
// configuration. Mirrors the platform pair in
// /home/pg/monorepo/yatool_orig/sg.json: 1,930 target nodes on
// `default-linux-aarch64`, ~1,800 host nodes on `default-linux-x86_64`,
// 27 cross-platform foreign-dep edges between them.
var DefaultLinuxConfig = PlatformConfig{
	Target: PlatformSpec{
		ID:     PlatformDefaultLinuxAArch64,
		Triple: "aarch64-linux-gnu",
		March:  "armv8-a",
		PIC:    false,
	},
	Host: PlatformSpec{
		ID:     PlatformDefaultLinuxX8664,
		Triple: "x86_64-linux-gnu",
		March:  "x86-64",
		PIC:    true,
	},
}

// TargetCfg is the back-compat alias kept so existing call sites
// (`EmitCC(TargetCfg, ...)`, `Gen(TargetCfg, ...)`) compile. It is
// equal to `DefaultLinuxConfig`; PR-25 will retire it once every
// rule emitter takes a `ModuleInstance` instead of a
// `PlatformConfig`.
var TargetCfg = DefaultLinuxConfig

// targetIsX8664 reports whether mi is built for the x86_64 platform.
// In M2/M3 this coincides with the host axis; naming avoids the
// host/target boolean framing per D41.
func targetIsX8664(mi ModuleInstance) bool {
	return mi.Platform.Target == PlatformDefaultLinuxX8664
}
