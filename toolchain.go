package main

// toolchain.go — minimal platform descriptor used by rule emitters.
//
// M1 only ships a single target platform (default-linux-aarch64); no host
// platform yet (M4 introduces host-side tool nodes). The struct is a
// placeholder: it carries just the platform name, which rule emitters embed
// into the emitted Node's `platform` field. M5 will fill out the real
// toolchain descriptor (compiler binary path, sysroot, target triple,
// arch flags, etc.) so that flag composition can stop hardcoding strings
// from the reference graph.

// PlatformConfig describes a target/host platform for rule emission.
//
// Currently only the platform name is captured; future PRs will add the
// fields the flag composers actually parameterize on (compiler path, target
// triple, march flag, debug-prefix-map roots, etc.). Until then, the
// hardcoded bundles in flags.go cover the M1 leaf module.
type PlatformConfig struct {
	Name string // e.g. "default-linux-aarch64"
}

// TargetCfg is the target-platform config for M1. Hardcoded to match the
// reference graph (`/home/pg/monorepo/yatool_orig/g.json`) for the
// `build/cow/on` leaf module. M4 will introduce a separate HostCfg for
// build-time tool nodes.
var TargetCfg = PlatformConfig{Name: "default-linux-aarch64"}
